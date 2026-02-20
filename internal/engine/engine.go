package engine

import "sync/atomic"

type Engine struct {
	tt    map[uint64]ttEntry // TT 移到 tt.go定义
	nodes int64

	blunderTT      map[uint64]uint8
	blunderReplyTT map[uint64]uint8

	UseNN bool
	nn    *NNEvaluator

	// Shared per-search abort flag. Set to 1 when any NN eval fails.
	nnAbort *uint32
}

func NewEngine() *Engine {
	abort := uint32(0)
	return &Engine{
		tt:             make(map[uint64]ttEntry, 1<<18),
		blunderTT:      make(map[uint64]uint8, 1<<16),
		blunderReplyTT: make(map[uint64]uint8, 1<<16),
		nnAbort:        &abort,
	}
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
