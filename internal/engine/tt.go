package engine

import "xionghan/internal/xionghan"

const (
	ttExact int8 = iota
	ttUpperBound
	ttLowerBound
)

// 简单 TT 条目
type ttEntry struct {
	Key   uint64
	Depth int
	Score int
	Flag  int8
	Move  xionghan.Move
}

// 存入 TT（你之前的简单策略）
func (e *Engine) storeTT(key uint64, depth int, score int, flag int8, mv xionghan.Move) {
	// 不加锁 ———— *你自己要求的*
	if len(e.tt) > 1_000_000 {
		e.tt = make(map[uint64]ttEntry, 1<<18)
	}
	old, ok := e.tt[key]
	replace := !ok || depth > old.Depth
	if ok && depth == old.Depth && flag == ttExact && old.Flag != ttExact {
		replace = true
	}
	if replace {
		e.tt[key] = ttEntry{
			Key:   key,
			Depth: depth,
			Score: score,
			Flag:  flag,
			Move:  mv,
		}
	}
}

// Position 哈希键：优先使用增量维护的 Zobrist。
func hashPosition(p *xionghan.Position) uint64 {
	return p.EnsureHash()
}
