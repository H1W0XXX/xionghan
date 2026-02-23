package engine

import (
	"sync"
	"sync/atomic"
)

const nnEvalCacheCap = 500_000

type nnEvalCache struct {
	mu sync.RWMutex
	m  map[uint64]int
}

type Engine struct {
	tt    map[uint64]ttEntry // TT 移到 tt.go定义
	nodes int64

	// 无锁置换表：使用固定大小数组代替 map
	blunderTT      []uint64
	blunderReplyTT []uint64

	UseNN bool
	nn    *NNEvaluator

	// Shared per-search abort flag. Set to 1 when any NN eval fails.
	nnAbort *uint32

	// Shared NN value cache keyed by position hash.
	nnCache *nnEvalCache

	// MCTS 持久化状态
	mctsRoot *MCTSNode
	mctsPool map[uint64]*MCTSNode
	poolMu   sync.Mutex
}

func NewEngine() *Engine {
	abort := uint32(0)
	return &Engine{
		tt:             make(map[uint64]ttEntry, 1<<18),
		blunderTT:      make([]uint64, 1<<18),
		blunderReplyTT: make([]uint64, 1<<18),
		nnAbort:        &abort,
		nnCache: &nnEvalCache{
			m: make(map[uint64]int, 1<<18),
		},
	}
}

// CloneForGame creates an engine instance for one game.
// It keeps independent search caches (TT/blunder/NN cache), but shares NN runtime.
func (e *Engine) CloneForGame() *Engine {
	cloned := NewEngine()
	if e == nil {
		return cloned
	}
	cloned.UseNN = e.UseNN
	cloned.nn = e.nn
	return cloned
}

func (e *Engine) InitNN(modelPath, libPath string) error {
	nn, err := NewNNEvaluator(modelPath, libPath)
	if err != nil {
		return err
	}
	e.nn = nn
	e.UseNN = true
	return nil
}

func (e *Engine) resetNNAbort() {
	if e.nnAbort == nil {
		abort := uint32(0)
		e.nnAbort = &abort
	}
	atomic.StoreUint32(e.nnAbort, 0)
}

func (e *Engine) markNNFailure() {
	if e.nnAbort == nil {
		abort := uint32(0)
		e.nnAbort = &abort
	}
	atomic.StoreUint32(e.nnAbort, 1)
}

func (e *Engine) hasNNFailure() bool {
	return e.nnAbort != nil && atomic.LoadUint32(e.nnAbort) != 0
}

func (e *Engine) getNNEvalFromCache(key uint64) (int, bool) {
	if e.nnCache == nil {
		return 0, false
	}
	e.nnCache.mu.RLock()
	v, ok := e.nnCache.m[key]
	e.nnCache.mu.RUnlock()
	return v, ok
}

func (e *Engine) storeNNEvalCache(key uint64, score int) {
	if e.nnCache == nil {
		return
	}
	e.nnCache.mu.Lock()
	if len(e.nnCache.m) > nnEvalCacheCap {
		e.nnCache.m = make(map[uint64]int, 1<<18)
	}
	e.nnCache.m[key] = score
	e.nnCache.mu.Unlock()
}
