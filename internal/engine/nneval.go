package engine

import (
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
)

type evalRequest struct {
	pos          *xionghan.Position
	stage        int
	chosenSquare int
	result       chan *NNResult
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
	if n.session != nil { n.session.Destroy() }
	for _, v := range n.inputs { v.Destroy() }
	for _, v := range n.outputs { v.Destroy() }
}

func (n *NNEvaluator) Evaluate(pos *xionghan.Position) (*NNResult, error) {
	return n.EvaluateWithStage(pos, 0, -1)
}

func (n *NNEvaluator) EvaluateWithStage(pos *xionghan.Position, stage int, chosenSquare int) (*NNResult, error) {
	resChan := make(chan *NNResult, 1)
	n.queue <- evalRequest{pos: pos, stage: stage, chosenSquare: chosenSquare, result: resChan}
	return <-resChan, nil
}

func (n *NNEvaluator) batchLoop() {
	requests := make([]evalRequest, 0, MaxBatchSize)
	for {
		requests = requests[:0]
		req, ok := <-n.queue
		if !ok { return }
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
		// Return error to all waiting requests
		for _, req := range requests {
			req.result <- nil 
		}
		return
	}

	n.totalBatches++
	n.totalItems += int64(batchSize)

	// Post-process: Softmax and distribution
	for i, req := range requests {
		// Value Head: 3 logits [Win, Loss, Draw]
		v := n.value[i*3 : i*3+3]
		
		maxLogit := v[0]
		if v[1] > maxLogit { maxLogit = v[1] }
		if v[2] > maxLogit { maxLogit = v[2] }
		
		e0 := math.Exp(float64(v[0] - maxLogit))
		e1 := math.Exp(float64(v[1] - maxLogit))
		e2 := math.Exp(float64(v[2] - maxLogit))
		sum := e0 + e1 + e2

		// 经验证，模型为固定视角输出：
		// Logit 0 (e0) 始终代表黑方 (Black/Green) 胜率
		// Logit 1 (e1) 始终代表红方 (Red) 胜率
		blackWin := float32(e0 / sum)
		redWin := float32(e1 / sum)

		res := &NNResult{
			Policy:   make([]float32, PolicySize),
			WinProb:  blackWin,
			LossProb: redWin,
			Score:    redWin - blackWin,
		}
		copy(res.Policy, n.policy[i*PolicySize:(i+1)*PolicySize])
		req.result <- res
	}

	if n.totalBatches%500 == 0 {
		fmt.Printf("NN Stats: Avg BatchSize=%.1f, Last Sample WinProb=%.4f\n", 
			float64(n.totalItems)/float64(n.totalBatches), n.value[0])
	}
}

func (n *NNEvaluator) fillOne(batchIdx int, pos *xionghan.Position, stage int, chosenSquare int) {
	planeSize := BoardSize * BoardSize
	spatialOffset := batchIdx * NumSpatialFeatures * planeSize
	globalOffset := batchIdx * NumGlobalFeatures

	subBin := n.binInput[spatialOffset : spatialOffset+NumSpatialFeatures*planeSize]
	for i := range subBin { subBin[i] = 0 }
	subGlobal := n.globalInput[globalOffset : globalOffset+NumGlobalFeatures]
	for i := range subGlobal { subGlobal[i] = 0 }

	pla := pos.SideToMove

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
			subBin[featureIdx*planeSize+sq] = 1.0
		}
	}

	// Plane 23: Chosen piece (for Stage 1)
	if stage == 1 && chosenSquare >= 0 && chosenSquare < planeSize {
		subBin[23*planeSize+chosenSquare] = 1.0
	}

	// Global 0: nextPlayer == Black (KataGo P_WHITE)
	if pla == xionghan.Black {
		subGlobal[0] = 1.0
	}

	// Global 1: Stage (0 = choose, 1 = place)
	subGlobal[1] = float32(stage)

	// Global 2: resultsBeforeNN.inited
	subGlobal[2] = 1.0

	// Global 3: resultsBeforeNN.winner == C_EMPTY (1.0 means game is ongoing)
	subGlobal[3] = 1.0

	// Global 4: resultsBeforeNN.winner == nextPlayer
	subGlobal[4] = 0.0

	// Global 5: resultsBeforeNN.winner == oppPlayer
	subGlobal[5] = 0.0

	// Global 7, 8: Parity for asymmetrical boards (though here 13x13 is symmetric)
	if BoardSize%2 != 0 {
		subGlobal[7] = 1.0
		subGlobal[8] = 1.0
	}
}

func (n *NNEvaluator) clearBatchTail(startIdx int) {
	spatialSize := NumSpatialFeatures * BoardSize * BoardSize
	for i := startIdx * spatialSize; i < MaxBatchSize*spatialSize; i++ { n.binInput[i] = 0 }
	for i := startIdx * NumGlobalFeatures; i < MaxBatchSize*NumGlobalFeatures; i++ { n.globalInput[i] = 0 }
}
