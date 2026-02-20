package engine

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
	"xionghan/internal/xionghan"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	NumSpatialFeatures = 25
	NumGlobalFeatures  = 19
	BoardSize          = 13
	PolicySize         = BoardSize*BoardSize + 1
	MaxBatchSize       = 64
	BatchTimeout       = 1 * time.Millisecond
	// Align with C++ nnPolicyTemperature (policy logits are scaled by 1/temp before softmax).
	NNPolicyTemperature = 1.0
)

const (
	resultWinnerUnknown = iota
	resultWinnerDraw
	resultWinnerNext
	resultWinnerOpp
)

type resultsBeforeNN struct {
	winnerClass int
	myOnlyLoc   int
	myOnlyPass  bool
}

type evalRequest struct {
	pos          *xionghan.Position
	stage        int
	chosenSquare int
	result       chan evalResponse
}

type evalResponse struct {
	res *NNResult
	err error
}

type NNResult struct {
	WinProb  float32
	LossProb float32
	Score    float32
	Policy   []float32
}

type NNEvaluator struct {
	session *ort.AdvancedSession
	queue   chan evalRequest

	// Buffers
	binInput    []float32
	globalInput []float32
	policy      []float32
	value       []float32

	// Persistent Tensors
	inputs  []ort.Value
	outputs []ort.Value

	// Stats
	totalItems   int64
	totalBatches int64
}

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	procSetEnv  = modkernel32.NewProc("SetEnvironmentVariableW")
)

const ansiReset = "\033[0m"

func init() {
	// 极致尽早重定向日志，防止 PowerShell 将 stderr 误认为错误而变红
	log.SetOutput(os.Stdout)
}

func setWinEnv(key, value string) {
	k, _ := syscall.UTF16PtrFromString(key)
	v, _ := syscall.UTF16PtrFromString(value)
	_, _, _ = procSetEnv.Call(uintptr(unsafe.Pointer(k)), uintptr(unsafe.Pointer(v)))
}

func setNativeEnv(key, value string) {
	os.Setenv(key, value)
	if runtime.GOOS == "windows" {
		setWinEnv(key, value)
	}
}

func NewNNEvaluator(modelPath string, libPath string) (*NNEvaluator, error) {
	// 1. Prepare Environment
	absCachePath, _ := filepath.Abs("trt_cache")
	os.MkdirAll(absCachePath, 0755)

	// Sync env vars for TensorRT Cache and Logging
	setNativeEnv("ORT_TENSORRT_ENGINE_CACHE_ENABLE", "1")
	setNativeEnv("ORT_TENSORRT_ENGINE_CACHE_PATH", absCachePath)
	setNativeEnv("ORT_TENSORRT_CACHE_ENABLE", "1")
	setNativeEnv("ORT_TENSORRT_CACHE_PATH", absCachePath)
	setNativeEnv("ORT_TRT_ENGINE_CACHE_ENABLE", "1")
	setNativeEnv("ORT_TRT_CACHE_PATH", absCachePath)
	setNativeEnv("ORT_TENSORRT_TIMING_CACHE_ENABLE", "1")
	setNativeEnv("ORT_TENSORRT_TIMING_CACHE_PATH", absCachePath)
	setNativeEnv("ORT_TENSORRT_FP16_ENABLE", "1")
	setNativeEnv("ORT_TENSORRT_MAX_WORKSPACE_SIZE", "2147483648")

	// Set logging level to Error (3) to suppress fallback warnings
	setNativeEnv("ORT_LOGGING_LEVEL", "3")

	if !ort.IsInitialized() {
		absLibPath, _ := filepath.Abs(libPath)
		// Ensure lib dir is in PATH for dependencies
		libDir := filepath.Dir(absLibPath)
		pathEnv := os.Getenv("PATH")
		setNativeEnv("PATH", libDir+string(os.PathListSeparator)+pathEnv)

		ort.SetSharedLibraryPath(absLibPath)
		ort.InitializeEnvironment()
		fmt.Print(ansiReset) // 强行重置可能由 ORT 产生的颜色码
	}

	binInput := make([]float32, MaxBatchSize*NumSpatialFeatures*BoardSize*BoardSize)
	globalInput := make([]float32, MaxBatchSize*NumGlobalFeatures)
	policy := make([]float32, MaxBatchSize*PolicySize)
	value := make([]float32, MaxBatchSize*3)

	binShape := ort.NewShape(MaxBatchSize, int64(NumSpatialFeatures), int64(BoardSize), int64(BoardSize))
	globalShape := ort.NewShape(MaxBatchSize, int64(NumGlobalFeatures))
	policyShape := ort.NewShape(MaxBatchSize, int64(PolicySize))
	valueShape := ort.NewShape(MaxBatchSize, 3)

	inputTensor1, _ := ort.NewTensor(binShape, binInput)
	inputTensor2, _ := ort.NewTensor(globalShape, globalInput)
	outputTensor1, _ := ort.NewTensor(policyShape, policy)
	outputTensor2, _ := ort.NewTensor(valueShape, value)

	inputNames := []string{"bin_inputs", "global_inputs"}
	outputNames := []string{"policy", "value"}
	inputs := []ort.Value{inputTensor1, inputTensor2}
	outputs := []ort.Value{outputTensor1, outputTensor2}

	var session *ort.AdvancedSession

	providers := []struct {
		name  string
		setup func(*ort.SessionOptions) error
	}{
		{"TensorRT", func(so *ort.SessionOptions) error {
			trtOpts, e := ort.NewTensorRTProviderOptions()
			if e != nil {
				return e
			}
			defer trtOpts.Destroy()
			trtOpts.Update(map[string]string{
				"device_id":               "0",
				"trt_engine_cache_enable": "1",
				"trt_engine_cache_path":   absCachePath,
				"trt_fp16_enable":         "1",
				"trt_max_workspace_size":  "2147483648",
				"trt_timing_cache_enable": "1",
				"trt_timing_cache_path":   absCachePath,
			})
			return so.AppendExecutionProviderTensorRT(trtOpts)
		}},
		{"CUDA", func(so *ort.SessionOptions) error {
			cudaOpts, e := ort.NewCUDAProviderOptions()
			if e != nil {
				return e
			}
			defer cudaOpts.Destroy()
			return so.AppendExecutionProviderCUDA(cudaOpts)
		}},
		{"DirectML", func(so *ort.SessionOptions) error {
			return so.AppendExecutionProviderDirectML(0)
		}},
		{"CPU", func(so *ort.SessionOptions) error { return nil }},
	}

	var success bool
	for _, p := range providers {
		log.Printf("NN: Attempting to initialize with %s...%s", p.name, ansiReset)
		so, _ := ort.NewSessionOptions()
		_ = so.SetLogSeverityLevel(3)

		if err := p.setup(so); err != nil {
			log.Printf("NN: %s setup failed: %v%s", p.name, err, ansiReset)
			so.Destroy()
			continue
		}

		s, errS := ort.NewAdvancedSession(modelPath, inputNames, outputNames, inputs, outputs, so)
		if errS != nil {
			log.Printf("NN: %s session creation failed: %v%s", p.name, errS, ansiReset)
			so.Destroy()
			continue
		}

		// Warmup
		log.Printf("NN: Warming up %s...%s", p.name, ansiReset)
		if errRun := s.Run(); errRun != nil {
			log.Printf("NN: %s warmup failed: %v%s", p.name, errRun, ansiReset)
			s.Destroy()
			so.Destroy()
			continue
		}

		log.Printf("NN: Successfully initialized with %s.%s", p.name, ansiReset)
		session = s
		success = true
		so.Destroy()
		break
	}

	if !success {
		return nil, fmt.Errorf("failed to initialize NN with any provider")
	}

	n := &NNEvaluator{
		session:     session,
		queue:       make(chan evalRequest, MaxBatchSize*10),
		binInput:    binInput,
		globalInput: globalInput,
		policy:      policy,
		value:       value,
		inputs:      inputs,
		outputs:     outputs,
	}

	go n.batchLoop()

	return n, nil
}

func (n *NNEvaluator) Close() {
	if n.session != nil {
		n.session.Destroy()
	}
	for _, v := range n.inputs {
		v.Destroy()
	}
	for _, v := range n.outputs {
		v.Destroy()
	}
}

func (n *NNEvaluator) Evaluate(pos *xionghan.Position) (*NNResult, error) {
	return n.EvaluateWithStage(pos, 0, -1)
}

func (n *NNEvaluator) EvaluateWithStage(pos *xionghan.Position, stage int, chosenSquare int) (*NNResult, error) {
	resChan := make(chan evalResponse, 1)
	n.queue <- evalRequest{pos: pos, stage: stage, chosenSquare: chosenSquare, result: resChan}
	resp := <-resChan
	if resp.err != nil {
		return nil, resp.err
	}
	if resp.res == nil {
		return nil, errors.New("nn evaluator returned empty result")
	}
	return resp.res, nil
}

func (n *NNEvaluator) batchLoop() {
	requests := make([]evalRequest, 0, MaxBatchSize)
	for {
		requests = requests[:0]
		req, ok := <-n.queue
		if !ok {
			return
		}
		requests = append(requests, req)

		timeout := time.After(BatchTimeout)
	collect:
		for len(requests) < MaxBatchSize {
			select {
			case r := <-n.queue:
				requests = append(requests, r)
			case <-timeout:
				break collect
			}
		}
		n.processBatch(requests)
	}
}

func (n *NNEvaluator) processBatch(requests []evalRequest) {
	batchSize := len(requests)
	var wg sync.WaitGroup
	for i, req := range requests {
		wg.Add(1)
		go func(idx int, r evalRequest) {
			defer wg.Done()
			n.fillOne(idx, r.pos, r.stage, r.chosenSquare)
		}(i, req)
	}
	wg.Wait()

	if batchSize < MaxBatchSize {
		n.clearBatchTail(batchSize)
	}

	// EXECUTE INFERENCE
	err := n.session.Run()
	if err != nil {
		fmt.Printf("CRITICAL: NN Session Run Error: %v\n", err)
		// Return error to all waiting requests.
		for _, req := range requests {
			req.result <- evalResponse{err: err}
		}
		return
	}

	n.totalBatches++
	n.totalItems += int64(batchSize)

	// Post-process: Softmax and convert to fixed color perspective.
	// KataGomo value logits are [nextPlayerWin, nextPlayerLoss, draw].
	for i, req := range requests {
		// Value head raw logits.
		v := n.value[i*3 : i*3+3]

		maxLogit := v[0]
		if v[1] > maxLogit {
			maxLogit = v[1]
		}
		if v[2] > maxLogit {
			maxLogit = v[2]
		}

		e0 := math.Exp(float64(v[0] - maxLogit))
		e1 := math.Exp(float64(v[1] - maxLogit))
		e2 := math.Exp(float64(v[2] - maxLogit))
		sum := e0 + e1 + e2

		nextWin := float32(e0 / sum)
		nextLoss := float32(e1 / sum)

		// Convert from next-player perspective to fixed color perspective.
		// In this project:
		// - Go Black == KataGo P_WHITE
		// - Go Red   == KataGo P_BLACK
		var blackWin, redWin float32
		if req.pos.SideToMove == xionghan.Black {
			// nextPlayer is Black -> nextWin is black win prob.
			blackWin = nextWin
			redWin = nextLoss
		} else {
			// nextPlayer is Red -> nextWin is red win prob.
			redWin = nextWin
			blackWin = nextLoss
		}

		res := &NNResult{
			WinProb:  blackWin,
			LossProb: redWin,
			Score:    redWin - blackWin,
		}
		rawPolicy := n.policy[i*PolicySize : (i+1)*PolicySize]
		policyForBoard := rawPolicy
		if req.pos.SideToMove == xionghan.Black {
			policyForBoard = unflipPolicyY(rawPolicy)
		}
		legalMask, legalCount := buildPolicyLegalMask(req.pos, req.stage, req.chosenSquare)
		res.Policy = postProcessPolicy(policyForBoard, &legalMask, legalCount)
		req.result <- evalResponse{res: res}
	}

	if n.totalBatches%500 == 0 {
		fmt.Printf("NN Stats: Avg BatchSize=%.1f, Last Sample ValueLogit0=%.4f\n",
			float64(n.totalItems)/float64(n.totalBatches), n.value[0])
	}
}

func (n *NNEvaluator) fillOne(batchIdx int, pos *xionghan.Position, stage int, chosenSquare int) {
	planeSize := BoardSize * BoardSize
	spatialOffset := batchIdx * NumSpatialFeatures * planeSize
	globalOffset := batchIdx * NumGlobalFeatures

	subBin := n.binInput[spatialOffset : spatialOffset+NumSpatialFeatures*planeSize]
	for i := range subBin {
		subBin[i] = 0
	}
	subGlobal := n.globalInput[globalOffset : globalOffset+NumGlobalFeatures]
	for i := range subGlobal {
		subGlobal[i] = 0
	}

	pla := pos.SideToMove
	flipY := pla == xionghan.Black

	// Plane 0: On board
	for i := 0; i < planeSize; i++ {
		subBin[i] = 1.0
	}

	// Pieces: 1-11 (Own), 12-22 (Opponent)
	// Order: Chariot, Horse, Cannon, Elephant, Advisor, King, Pawn, Lei, Feng, Wei
	for sq := 0; sq < xionghan.NumSquares; sq++ {
		pc := pos.Board.Squares[sq]
		if pc == 0 {
			continue
		}

		pt := pc.Type()
		side := pc.Side()

		var featureIdx int
		if side == pla {
			featureIdx = int(pt) // 1-10 (Lei=8, Feng=9, Wei=10)
		} else {
			featureIdx = int(pt) + 11 // 12-21
		}

		if featureIdx < 23 {
			featureSq := mapSquareForNN(sq, flipY)
			subBin[featureIdx*planeSize+featureSq] = 1.0
		}
	}

	// Plane 23: Chosen piece (for Stage 1)
	if stage == 1 && chosenSquare >= 0 && chosenSquare < planeSize {
		featureChosenSq := mapSquareForNN(chosenSquare, flipY)
		subBin[23*planeSize+featureChosenSq] = 1.0
	}

	// Global 0: nextPlayer == Black (KataGo P_WHITE)
	if pla == xionghan.Black {
		subGlobal[0] = 1.0
	}

	// Global 1: Stage (0 = choose, 1 = place)
	subGlobal[1] = float32(stage)

	results := computeResultsBeforeNN(pos, stage, chosenSquare)

	// Global 2: resultsBeforeNN.inited
	subGlobal[2] = 1.0

	// Global 3: resultsBeforeNN.winner == C_EMPTY (draw)
	if results.winnerClass == resultWinnerDraw {
		subGlobal[3] = 1.0
	}

	// Global 4: resultsBeforeNN.winner == nextPlayer
	if results.winnerClass == resultWinnerNext {
		subGlobal[4] = 1.0
	}

	// Global 5: resultsBeforeNN.winner == oppPlayer
	if results.winnerClass == resultWinnerOpp {
		subGlobal[5] = 1.0
	}

	// Plane 24: resultsBeforeNN.myOnlyLoc
	if results.myOnlyLoc >= 0 && results.myOnlyLoc < planeSize {
		featureOnlySq := mapSquareForNN(results.myOnlyLoc, flipY)
		subBin[24*planeSize+featureOnlySq] = 1.0
	} else if results.myOnlyPass {
		// Global 6: resultsBeforeNN.myOnlyLoc == PASS_LOC
		subGlobal[6] = 1.0
	}

	// Global 7, 8: Parity for asymmetrical boards (though here 13x13 is symmetric)
	if BoardSize%2 != 0 {
		subGlobal[7] = 1.0
		subGlobal[8] = 1.0
	}
}

func computeResultsBeforeNN(pos *xionghan.Position, stage int, chosenSquare int) resultsBeforeNN {
	out := resultsBeforeNN{
		winnerClass: resultWinnerUnknown,
		myOnlyLoc:   -1,
		myOnlyPass:  false,
	}

	legalMoves := pos.GenerateLegalMoves(false)
	legalCount := 0

	if stage == 0 {
		var seenFrom [xionghan.NumSquares]bool
		for _, mv := range legalMoves {
			if mv.From >= 0 && mv.From < xionghan.NumSquares && !seenFrom[mv.From] {
				seenFrom[mv.From] = true
				legalCount++
			}
		}
	} else if stage == 1 {
		for _, mv := range legalMoves {
			if mv.From == chosenSquare {
				legalCount++
			}
		}
	}

	// Match C++ ResultsBeforeNN::init semantics:
	// only when choose-stage has no legal move, mark opponent as winner.
	if stage == 0 && legalCount == 0 {
		out.winnerClass = resultWinnerOpp
	}

	return out
}

func buildPolicyLegalMask(pos *xionghan.Position, stage int, chosenSquare int) ([PolicySize]bool, int) {
	var mask [PolicySize]bool
	legalCount := 0

	legalMoves := pos.GenerateLegalMoves(false)
	if stage == 0 {
		var seenFrom [xionghan.NumSquares]bool
		for _, mv := range legalMoves {
			if mv.From >= 0 && mv.From < xionghan.NumSquares && !seenFrom[mv.From] {
				seenFrom[mv.From] = true
				mask[mv.From] = true
				legalCount++
			}
		}
	} else if stage == 1 {
		for _, mv := range legalMoves {
			if mv.From == chosenSquare && mv.To >= 0 && mv.To < xionghan.NumSquares && !mask[mv.To] {
				mask[mv.To] = true
				legalCount++
			}
		}
	}

	return mask, legalCount
}

func postProcessPolicy(raw []float32, legalMask *[PolicySize]bool, legalCount int) []float32 {
	out := make([]float32, PolicySize)

	if legalCount <= 0 {
		for i := range out {
			out[i] = -1.0
		}
		return out
	}

	maxPolicy := math.Inf(-1)
	for i := 0; i < PolicySize; i++ {
		if (*legalMask)[i] {
			v := float64(raw[i])
			if v > maxPolicy {
				maxPolicy = v
			}
		} else {
			out[i] = -1.0
		}
	}

	temp := NNPolicyTemperature
	if math.IsNaN(temp) || math.IsInf(temp, 0) || temp <= 0 {
		temp = 1.0
	}
	invTemp := 1.0 / temp

	policySum := 0.0
	for i := 0; i < PolicySize; i++ {
		if !(*legalMask)[i] {
			continue
		}
		v := math.Exp((float64(raw[i]) - maxPolicy) * invTemp)
		out[i] = float32(v)
		policySum += v
	}

	if math.IsNaN(policySum) || math.IsInf(policySum, 0) || policySum <= 0 {
		uniform := float32(1.0 / float64(legalCount))
		for i := 0; i < PolicySize; i++ {
			if (*legalMask)[i] {
				out[i] = uniform
			} else {
				out[i] = -1.0
			}
		}
		return out
	}

	invSum := float32(1.0 / policySum)
	for i := 0; i < PolicySize; i++ {
		if (*legalMask)[i] {
			out[i] *= invSum
		} else {
			out[i] = -1.0
		}
	}
	return out
}

func mapSquareForNN(sq int, flipY bool) int {
	if !flipY {
		return sq
	}
	r := sq / BoardSize
	c := sq % BoardSize
	rr := BoardSize - 1 - r
	return rr*BoardSize + c
}

func unflipPolicyY(raw []float32) []float32 {
	out := make([]float32, PolicySize)
	for sq := 0; sq < BoardSize*BoardSize; sq++ {
		out[sq] = raw[mapSquareForNN(sq, true)]
	}
	// pass move
	out[PolicySize-1] = raw[PolicySize-1]
	return out
}

func (n *NNEvaluator) clearBatchTail(startIdx int) {
	spatialSize := NumSpatialFeatures * BoardSize * BoardSize
	for i := startIdx * spatialSize; i < MaxBatchSize*spatialSize; i++ {
		n.binInput[i] = 0
	}
	for i := startIdx * NumGlobalFeatures; i < MaxBatchSize*NumGlobalFeatures; i++ {
		n.globalInput[i] = 0
	}
}
