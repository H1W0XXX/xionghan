package engine

import "xionghan/internal/xionghan"

// 简单 TT 条目
type ttEntry struct {
	Key   uint64
	Depth int
	Score int
	Move  xionghan.Move
}

// 存入 TT（你之前的简单策略）
func (e *Engine) storeTT(key uint64, depth int, score int, mv xionghan.Move) {
	// 不加锁 ———— *你自己要求的*
	if len(e.tt) > 1_000_000 {
		e.tt = make(map[uint64]ttEntry, 1<<18)
	}
	old, ok := e.tt[key]
	if !ok || depth >= old.Depth {
		e.tt[key] = ttEntry{
			Key:   key,
			Depth: depth,
			Score: score,
			Move:  mv,
		}
	}
}

// FNV-1a 哈希函数
func hashPosition(p *xionghan.Position) uint64 {
	const (
		offset64 = 1469598103934665603
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < xionghan.NumSquares; i++ {
		b := byte(p.Board.Squares[i])
		h ^= uint64(b)
		h *= prime64
	}
	h ^= uint64(p.SideToMove)
	h *= prime64
	return h
}
