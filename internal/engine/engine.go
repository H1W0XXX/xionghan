package engine

type Engine struct {
	tt    map[uint64]ttEntry // TT 移到 tt.go定义
	nodes int64

	UseNN bool
	nn    *NNEvaluator
}

func NewEngine() *Engine {
	return &Engine{
		tt: make(map[uint64]ttEntry, 1<<18),
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
