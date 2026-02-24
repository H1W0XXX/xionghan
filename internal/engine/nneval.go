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
	"time"
	"xionghan/internal/xionghan"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	NumSpatialFeatures  = 25
	NumGlobalFeatures   = 19
	BoardSize           = 13
	PolicySize          = BoardSize*BoardSize + 1
	defaultMaxBatchSize = 512
	BatchTimeout        = 5 * time.Millisecond
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

type nnRuntime struct {
	batchSize int
	modelPath string
	session   *ort.AdvancedSession
	mu        sync.Mutex

	binInput    []float32
	globalInput []float32
	policy      []float32
	value       []float32

	inputs  []ort.Value
	outputs []ort.Value
}

type NNEvaluator struct {
	runtimes []*nnRuntime
	maxBatch int
	queue    chan evalRequest

	selectedProvider string // 导出供 MCTS 调度使用

	// Stats
	totalItems   int64
	totalBatches int64
}

const ansiReset = "\033[0m"

func init() {
	// 极致尽早重定向日志，防止 PowerShell 将 stderr 误认为错误而变红
	log.SetOutput(os.Stdout)
}

func prependPathEnv(key, value string) {
	if value == "" {
		return
	}
	old := os.Getenv(key)
	if old == "" {
		setNativeEnv(key, value)
		return
	}
	setNativeEnv(key, value+string(os.PathListSeparator)+old)
}

func NewNNEvaluator(modelPath string, libPath string) (*NNEvaluator, error) {
	// 1. Prepare Environment
	absCachePath, _ := filepath.Abs("trt_cache")
	os.MkdirAll(absCachePath, 0755)

	absModelPath, err := resolveModelPath(modelPath)
	if err != nil {
		return nil, fmt.Errorf("resolve onnx model path: %w", err)
	}
	modelProfiles := make([]struct {
		batch int
		path  string
	}, 0, 12)
	for b := 1; b <= defaultMaxBatchSize; b <<= 1 {
		modelProfiles = append(modelProfiles, struct {
			batch int
			path  string
		}{batch: b, path: absModelPath})
	}

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
		absLibPath, err := resolveORTSharedLibraryPath(libPath)
		if err != nil {
			return nil, fmt.Errorf("resolve onnxruntime shared library path: %w", err)
		}

		// Ensure lib dir is in PATH for dependencies
		libDir := filepath.Dir(absLibPath)
		prependPathEnv("PATH", libDir)
		configureORTSearchPath(libDir)

		ort.SetSharedLibraryPath(absLibPath)
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("initialize onnxruntime environment with %s: %w", absLibPath, err)
		}
		log.Printf("NN: ONNX Runtime shared library: %s%s", absLibPath, ansiReset)
		fmt.Print(ansiReset) // 强行重置可能由 ORT 产生的颜色码
	}
	log.Printf("NN: ONNX model (dynamic, single file): %s, runtime max batch=%d%s", absModelPath, defaultMaxBatchSize, ansiReset)

	providers := []struct {
		name  string
		setup func(*ort.SessionOptions) error
	}{}
	if runtime.GOOS == "darwin" {
		providers = append(providers,
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{
				"CoreML",
				func(so *ort.SessionOptions) error {
					// Prefer CoreMLV2, fallback to legacy API for older ORT builds.
					if e := so.AppendExecutionProviderCoreMLV2(map[string]string{}); e == nil {
						return nil
					}
					return so.AppendExecutionProviderCoreML(0)
				},
			},
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{"CPU", func(so *ort.SessionOptions) error { return nil }},
		)
	} else if runtime.GOOS == "windows" {
		providers = append(providers,
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{
				"TensorRT",
				func(so *ort.SessionOptions) error {
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
				},
			},
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{
				"CUDA",
				func(so *ort.SessionOptions) error {
					cudaOpts, e := ort.NewCUDAProviderOptions()
					if e != nil {
						return e
					}
					defer cudaOpts.Destroy()
					return so.AppendExecutionProviderCUDA(cudaOpts)
				},
			},
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{"DirectML", func(so *ort.SessionOptions) error { return so.AppendExecutionProviderDirectML(0) }},
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{"CPU", func(so *ort.SessionOptions) error { return nil }},
		)
	} else {
		providers = append(providers,
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{
				"XNNPACK",
				func(so *ort.SessionOptions) error {
					return so.AppendExecutionProviderXNNPACK(map[string]string{
						"intra_op_num_threads": "4",
					})
				},
			},
			struct {
				name  string
				setup func(*ort.SessionOptions) error
			}{"CPU", func(so *ort.SessionOptions) error { return nil }},
		)
	}

	var runtimes []*nnRuntime
	selectedProvider := ""
	for _, p := range providers {
		log.Printf("NN: Attempting to initialize with %s...%s", p.name, ansiReset)
		tryRuntimes := make([]*nnRuntime, 0, len(modelProfiles))
		ok := true
		for _, mp := range modelProfiles {
			rt, errBuild := createRuntimeForProfile(mp.path, mp.batch, p.setup)
			if errBuild != nil {
				log.Printf("NN: %s profile b=%d init failed: %v%s", p.name, mp.batch, errBuild, ansiReset)
				for _, built := range tryRuntimes {
					built.destroy()
				}
				ok = false
				break
			}
			tryRuntimes = append(tryRuntimes, rt)
		}
		if !ok {
			continue
		}
		// Run one synchronous warmup on the smallest profile to ensure provider is truly usable.
		if len(tryRuntimes) > 0 {
			log.Printf("NN: Warming up %s profile b=%d...%s", p.name, tryRuntimes[0].batchSize, ansiReset)
			if errRun := runWarmup(tryRuntimes[0]); errRun != nil {
				log.Printf("NN: %s profile b=%d warmup failed: %v%s", p.name, tryRuntimes[0].batchSize, errRun, ansiReset)
				for _, built := range tryRuntimes {
					built.destroy()
				}
				continue
			}
		}
		runtimes = tryRuntimes
		selectedProvider = p.name
		log.Printf("NN: Successfully initialized with %s (%d profiles).%s", p.name, len(runtimes), ansiReset)
		break
	}

	if len(runtimes) == 0 {
		return nil, fmt.Errorf("failed to initialize NN with any provider")
	}

	maxBatch := 0
	for _, rt := range runtimes {
		if rt.batchSize > maxBatch {
			maxBatch = rt.batchSize
		}
	}
	if maxBatch <= 0 {
		maxBatch = defaultMaxBatchSize
	}

	n := &NNEvaluator{
		runtimes:         runtimes,
		maxBatch:         maxBatch,
		selectedProvider: selectedProvider,
		queue:            make(chan evalRequest, maxBatch*10),
	}

	go n.batchLoop()
	go n.warmupProfilesAsync(selectedProvider)

	return n, nil
}

func runWarmup(rt *nnRuntime) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.session.Run()
}

func (n *NNEvaluator) warmupProfilesAsync(providerName string) {
	if len(n.runtimes) <= 1 {
		return
	}
	log.Printf("NN: Background warmup started for %s (%d pending profiles).%s", providerName, len(n.runtimes)-1, ansiReset)
	for i := len(n.runtimes) - 1; i >= 1; i-- {
		rt := n.runtimes[i]
		log.Printf("NN: Background warming %s profile b=%d...%s", providerName, rt.batchSize, ansiReset)
		if err := runWarmup(rt); err != nil {
			log.Printf("NN: Background warmup failed for %s profile b=%d: %v%s", providerName, rt.batchSize, err, ansiReset)
			return
		}
	}
	log.Printf("NN: Background warmup finished for %s.%s", providerName, ansiReset)
}

func createRuntimeForProfile(modelPath string, batchSize int, setupProvider func(*ort.SessionOptions) error) (*nnRuntime, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("invalid batch size %d", batchSize)
	}
	binInput := make([]float32, batchSize*NumSpatialFeatures*BoardSize*BoardSize)
	globalInput := make([]float32, batchSize*NumGlobalFeatures)
	policy := make([]float32, batchSize*PolicySize)
	value := make([]float32, batchSize*3)

	binShape := ort.NewShape(int64(batchSize), int64(NumSpatialFeatures), int64(BoardSize), int64(BoardSize))
	globalShape := ort.NewShape(int64(batchSize), int64(NumGlobalFeatures))
	policyShape := ort.NewShape(int64(batchSize), int64(PolicySize))
	valueShape := ort.NewShape(int64(batchSize), 3)

	inputTensor1, err := ort.NewTensor(binShape, binInput)
	if err != nil {
		return nil, err
	}
	inputTensor2, err := ort.NewTensor(globalShape, globalInput)
	if err != nil {
		inputTensor1.Destroy()
		return nil, err
	}
	outputTensor1, err := ort.NewTensor(policyShape, policy)
	if err != nil {
		inputTensor1.Destroy()
		inputTensor2.Destroy()
		return nil, err
	}
	outputTensor2, err := ort.NewTensor(valueShape, value)
	if err != nil {
		inputTensor1.Destroy()
		inputTensor2.Destroy()
		outputTensor1.Destroy()
		return nil, err
	}

	inputNames := []string{"bin_inputs", "global_inputs"}
	outputNames := []string{"policy", "value"}
	inputs := []ort.Value{inputTensor1, inputTensor2}
	outputs := []ort.Value{outputTensor1, outputTensor2}

	so, err := ort.NewSessionOptions()
	if err != nil {
		outputTensor2.Destroy()
		outputTensor1.Destroy()
		inputTensor2.Destroy()
		inputTensor1.Destroy()
		return nil, err
	}
	_ = so.SetLogSeverityLevel(3)
	if err := setupProvider(so); err != nil {
		so.Destroy()
		outputTensor2.Destroy()
		outputTensor1.Destroy()
		inputTensor2.Destroy()
		inputTensor1.Destroy()
		return nil, err
	}

	session, err := ort.NewAdvancedSession(modelPath, inputNames, outputNames, inputs, outputs, so)
	so.Destroy()
	if err != nil {
		outputTensor2.Destroy()
		outputTensor1.Destroy()
		inputTensor2.Destroy()
		inputTensor1.Destroy()
		return nil, err
	}

	return &nnRuntime{
		batchSize:   batchSize,
		modelPath:   modelPath,
		session:     session,
		binInput:    binInput,
		globalInput: globalInput,
		policy:      policy,
		value:       value,
		inputs:      inputs,
		outputs:     outputs,
	}, nil
}

func (n *NNEvaluator) Close() {
	for _, rt := range n.runtimes {
		rt.destroy()
	}
}

func (rt *nnRuntime) destroy() {
	if rt == nil {
		return
	}
	if rt.session != nil {
		rt.session.Destroy()
	}
	for _, v := range rt.inputs {
		v.Destroy()
	}
	for _, v := range rt.outputs {
		v.Destroy()
	}
}

func (n *NNEvaluator) selectRuntime(batchSize int) *nnRuntime {
	if len(n.runtimes) == 0 {
		return nil
	}
	for _, rt := range n.runtimes {
		if batchSize <= rt.batchSize {
			return rt
		}
	}
	return n.runtimes[len(n.runtimes)-1]
}

func (n *NNEvaluator) maxRuntimeBatch() int {
	if n.maxBatch > 0 {
		return n.maxBatch
	}
	maxBatch := 0
	for _, rt := range n.runtimes {
		if rt.batchSize > maxBatch {
			maxBatch = rt.batchSize
		}
	}
	if maxBatch <= 0 {
		maxBatch = defaultMaxBatchSize
	}
	n.maxBatch = maxBatch
	return maxBatch
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
	maxBatch := n.maxRuntimeBatch()
	requests := make([]evalRequest, 0, maxBatch)
	for {
		requests = requests[:0]
		req, ok := <-n.queue
		if !ok {
			return
		}
		requests = append(requests, req)

		timeout := time.After(BatchTimeout)
	collect:
		for len(requests) < maxBatch {
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
	if batchSize == 0 {
		return
	}

	plans := planInferenceBatches(batchSize, n.maxRuntimeBatch())
	offset := 0
	for _, cap := range plans {
		if offset >= batchSize {
			break
		}
		take := batchSize - offset
		if take > cap {
			take = cap
		}
		chunkReqs := requests[offset : offset+take]
		if err := n.runChunk(cap, chunkReqs); err != nil {
			for _, req := range requests[offset:] {
				req.result <- evalResponse{err: err}
			}
			return
		}
		offset += take
	}
}

func (n *NNEvaluator) runChunk(capacity int, requests []evalRequest) error {
	rt := n.selectRuntime(capacity)
	if rt == nil {
		return errors.New("no available nn runtime")
	}
	if len(requests) > rt.batchSize {
		return fmt.Errorf("chunk too large: %d > runtime batch %d", len(requests), rt.batchSize)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var wg sync.WaitGroup
	for i, req := range requests {
		wg.Add(1)
		go func(idx int, r evalRequest) {
			defer wg.Done()
			n.fillOne(rt, idx, r.pos, r.stage, r.chosenSquare)
		}(i, req)
	}
	wg.Wait()

	if len(requests) < rt.batchSize {
		n.clearBatchTail(rt, len(requests))
	}

	// EXECUTE INFERENCE
	err := rt.session.Run()
	if err != nil {
		return err
	}

	n.totalBatches++
	n.totalItems += int64(len(requests))

	// Post-process: Softmax and convert to fixed color perspective.
	// KataGomo value logits are [nextPlayerWin, nextPlayerLoss, draw].
	for i, req := range requests {
		// Value head raw logits.
		v := rt.value[i*3 : i*3+3]

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
		rawPolicy := rt.policy[i*PolicySize : (i+1)*PolicySize]
		policyForBoard := rawPolicy
		if req.pos.SideToMove == xionghan.Black {
			policyForBoard = unflipPolicyY(rawPolicy)
		}
		legalMask, legalCount := buildPolicyLegalMask(req.pos, req.stage, req.chosenSquare)
		res.Policy = postProcessPolicy(policyForBoard, &legalMask, legalCount)
		req.result <- evalResponse{res: res}
	}

	if n.totalBatches%500 == 0 {
		last := float32(0)
		if len(rt.value) > 0 {
			last = rt.value[0]
		}
		fmt.Printf("NN Stats: Avg BatchSize=%.1f, Last Sample ValueLogit0=%.4f, RuntimeBatch=%d\n",
			float64(n.totalItems)/float64(n.totalBatches), last, rt.batchSize)
	}
	return nil
}

func planInferenceBatches(total int, maxBatch int) []int {
	if total <= 0 || maxBatch <= 0 {
		return nil
	}
	out := make([]int, 0, 8)
	full := total / maxBatch
	for i := 0; i < full; i++ {
		out = append(out, maxBatch)
	}
	rem := total % maxBatch
	if rem > 0 {
		out = append(out, planTailByRule(rem, maxBatch)...)
	}
	return out
}

func planTailByRule(n int, cap int) []int {
	if n <= 0 {
		return nil
	}
	if cap <= 1 {
		return []int{1}
	}
	if n >= cap {
		return []int{cap}
	}

	half := cap / 2
	quarter := cap / 4
	if quarter < 1 {
		quarter = 1
	}

	// Rule: if above (half + quarter), pad up to cap.
	if n > half+quarter {
		return []int{cap}
	}
	// Otherwise split as half + next lower power-of-two padding.
	if n > half {
		rem := n - half
		second := nextPow2(rem)
		if second > quarter {
			second = quarter
		}
		return []int{half, second}
	}
	return planTailByRule(n, half)
}

func nextPow2(v int) int {
	if v <= 1 {
		return 1
	}
	p := 1
	for p < v {
		p <<= 1
	}
	return p
}

func (n *NNEvaluator) fillOne(rt *nnRuntime, batchIdx int, pos *xionghan.Position, stage int, chosenSquare int) {
	planeSize := BoardSize * BoardSize
	spatialOffset := batchIdx * NumSpatialFeatures * planeSize
	globalOffset := batchIdx * NumGlobalFeatures

	subBin := rt.binInput[spatialOffset : spatialOffset+NumSpatialFeatures*planeSize]
	for i := range subBin {
		subBin[i] = 0
	}
	subGlobal := rt.globalInput[globalOffset : globalOffset+NumGlobalFeatures]
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

func (n *NNEvaluator) clearBatchTail(rt *nnRuntime, startIdx int) {
	spatialSize := NumSpatialFeatures * BoardSize * BoardSize
	for i := startIdx * spatialSize; i < rt.batchSize*spatialSize; i++ {
		rt.binInput[i] = 0
	}
	for i := startIdx * NumGlobalFeatures; i < rt.batchSize*NumGlobalFeatures; i++ {
		rt.globalInput[i] = 0
	}
}
