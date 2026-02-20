package engine

import (
	"xionghan/internal/xionghan"
)

const (
	vcfDepthCap         = 8
	vcfDefaultDepth     = 6
	vcfNodeBudgetBase   = 12000
	vcfNodeBudgetPerPly = 4000
)

const (
	vcfModeAttack uint64 = 0xA5A5A5A5A5A5A5A5
	vcfModeDefend uint64 = 0x5A5A5A5A5A5A5A5A
)

type vcfTTEntry struct {
	Depth  int
	Result bool
}

type vcfContext struct {
	tt         map[uint64]vcfTTEntry
	inPath     map[uint64]bool
	nodes      int
	nodeBudget int
}

// VCFResult 连将搜索结果
type VCFResult struct {
	CanWin bool
	Move   xionghan.Move
}

// VCFSearch 寻找连将胜
func (e *Engine) VCFSearch(pos *xionghan.Position, maxDepth int) VCFResult {
	if maxDepth <= 0 {
		maxDepth = vcfDefaultDepth
	}
	if maxDepth > vcfDepthCap {
		maxDepth = vcfDepthCap
	}
	if pos.TotalPieces() > 43 {
		return VCFResult{CanWin: false}
	}

	ctx := &vcfContext{
		tt:         make(map[uint64]vcfTTEntry, 1<<12),
		inPath:     make(map[uint64]bool, 1<<10),
		nodeBudget: vcfNodeBudgetBase + maxDepth*vcfNodeBudgetPerPly,
	}

	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}

		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			return VCFResult{CanWin: true, Move: mv}
		}

		if !nextPos.IsInCheck(nextPos.SideToMove) {
			continue
		}

		if !e.vcfDefenderCanEscape(nextPos, maxDepth-1, ctx) {
			return VCFResult{CanWin: true, Move: mv}
		}
	}

	return VCFResult{CanWin: false}
}

func (e *Engine) vcfAttackerCanForce(pos *xionghan.Position, depth int, ctx *vcfContext) bool {
	if depth <= 0 {
		return false
	}
	if ctx.reachNodeBudget() {
		return false
	}
	key := hashPosition(pos) ^ vcfModeAttack
	if ctx.inPath[key] {
		return false
	}
	if entry, ok := ctx.tt[key]; ok && entry.Depth >= depth {
		return entry.Result
	}
	ctx.inPath[key] = true
	defer delete(ctx.inPath, key)

	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	result := false
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}

		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			result = true
			break
		}

		if !nextPos.IsInCheck(nextPos.SideToMove) {
			continue
		}

		if !e.vcfDefenderCanEscape(nextPos, depth-1, ctx) {
			result = true
			break
		}
	}
	ctx.tt[key] = vcfTTEntry{
		Depth:  depth,
		Result: result,
	}
	return result
}

func (e *Engine) vcfDefenderCanEscape(pos *xionghan.Position, depth int, ctx *vcfContext) bool {
	if depth <= 0 {
		return true
	}
	if ctx.reachNodeBudget() {
		return true
	}
	key := hashPosition(pos) ^ vcfModeDefend
	if ctx.inPath[key] {
		return true
	}
	if entry, ok := ctx.tt[key]; ok && entry.Depth >= depth {
		return entry.Result
	}
	ctx.inPath[key] = true
	defer delete(ctx.inPath, key)

	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	if len(moves) == 0 {
		ctx.tt[key] = vcfTTEntry{
			Depth:  depth,
			Result: false,
		}
		return false
	}

	result := false
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}
		if !e.vcfAttackerCanForce(nextPos, depth-1, ctx) {
			result = true
			break
		}
	}
	ctx.tt[key] = vcfTTEntry{
		Depth:  depth,
		Result: result,
	}
	return result
}

func (ctx *vcfContext) reachNodeBudget() bool {
	ctx.nodes++
	return ctx.nodes > ctx.nodeBudget
}

func (e *Engine) CanCaptureKingNext(pos *xionghan.Position) bool {
	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	for _, mv := range moves {
		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			return true
		}
	}
	return false
}
