package engine

import (
	"sort"
	"xionghan/internal/xionghan"
)

const (
	vcfDepthCap         = 24    // 大幅提升搜索上限
	vcfDefaultDepth     = 8
	vcfNodeBudgetBase   = 32000 // 增加基础预算
	vcfNodeBudgetPerPly = 8000
)

const (
	vcfModeAttack uint64 = 0xA5A5A5A5A5A5A5A5
	vcfModeDefend uint64 = 0x5A5A5A5A5A5A5A5A
)

type vcfTTEntry struct {
	Depth  int
	Result bool
	Move   xionghan.Move // 记录最佳走法用于排序
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
	// 在子力较少时更容易产生绝杀，增加搜索资源
	if pos.TotalPieces() > 44 {
		return VCFResult{CanWin: false}
	}

	ctx := &vcfContext{
		tt:         make(map[uint64]vcfTTEntry, 1<<16), // 加大置换表
		inPath:     make(map[uint64]bool, 1<<10),
		nodeBudget: vcfNodeBudgetBase + maxDepth*vcfNodeBudgetPerPly,
	}

	var bestMove xionghan.Move
	// 迭代加深：从 2 层开始逐步搜到 maxDepth，利用置换表优化排序
	for d := 2; d <= maxDepth; d += 2 {
		found, move := e.vcfRootSearch(pos, d, ctx)
		if found {
			return VCFResult{CanWin: true, Move: move}
		}
		bestMove = move
		if ctx.reachNodeBudget() {
			break
		}
	}

	return VCFResult{CanWin: false, Move: bestMove}
}

func (e *Engine) vcfRootSearch(pos *xionghan.Position, depth int, ctx *vcfContext) (bool, xionghan.Move) {
	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	e.scoreVCFMoves(pos, moves, ctx)
	
	// 排序：权重越高越优先尝试
	sort.Slice(moves, func(i, j int) bool {
		return moves[i].Score > moves[j].Score
	})

	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}

		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			return true, mv
		}

		// 攻击方必须将军
		if !nextPos.IsInCheck(nextPos.SideToMove) {
			continue
		}

		if !e.vcfDefenderCanEscape(nextPos, depth-1, ctx) {
			return true, mv
		}
	}
	return false, xionghan.Move{}
}

// scoreVCFMoves 启发式评分：车 > 檑 = 炮 > 马
func (e *Engine) scoreVCFMoves(pos *xionghan.Position, moves []xionghan.Move, ctx *vcfContext) {
	key := hashPosition(pos) ^ vcfModeAttack
	ttMove := xionghan.Move{}
	if entry, ok := ctx.tt[key]; ok {
		ttMove = entry.Move
	}

	for i := range moves {
		mv := &moves[i]
		// 1. 置换表走法最高优先级
		if mv.From == ttMove.From && mv.To == ttMove.To {
			mv.Score = 1000
			continue
		}

		pc := pos.Board.Squares[mv.From]
		pt := pc.Type()
		target := pos.Board.Squares[mv.To]

		// 2. 吃子将军评分更高 (MVV-LVA)
		if target != 0 {
			mv.Score = 100 + int(target.Type())
		}

		// 3. 子力权重评分
		switch pt {
		case xionghan.PieceKing: // 王面对面将军或吃王
			mv.Score += 500
		case xionghan.PieceRook:
			mv.Score += 80
		case xionghan.PieceLei, xionghan.PieceCannon:
			mv.Score += 60
		case xionghan.PieceKnight:
			mv.Score += 40
		case xionghan.PiecePawn, xionghan.PieceFeng:
			mv.Score += 20
		}
	}
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
	e.scoreVCFMoves(pos, moves, ctx)
	sort.Slice(moves, func(i, j int) bool {
		return moves[i].Score > moves[j].Score
	})

	result := false
	var bestMove xionghan.Move
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}

		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			result = true
			bestMove = mv
			break
		}

		if !nextPos.IsInCheck(nextPos.SideToMove) {
			continue
		}

		if !e.vcfDefenderCanEscape(nextPos, depth-1, ctx) {
			result = true
			bestMove = mv
			break
		}
	}
	ctx.tt[key] = vcfTTEntry{
		Depth:  depth,
		Result: result,
		Move:   bestMove,
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
	var bestMove xionghan.Move
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}
		if !e.vcfAttackerCanForce(nextPos, depth-1, ctx) {
			result = true // 防守方只要找到一个不被 VCF 的走法就算逃脱
			bestMove = mv
			break
		}
	}
	ctx.tt[key] = vcfTTEntry{
		Depth:  depth,
		Result: result,
		Move:   bestMove,
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
