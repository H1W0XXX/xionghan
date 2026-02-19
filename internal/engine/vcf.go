package engine

import (
	"xionghan/internal/xionghan"
)

// VCFResult 连将搜索结果
type VCFResult struct {
	CanWin bool
	Move   xionghan.Move
}

// VCFSearch 寻找连将胜
func (e *Engine) VCFSearch(pos *xionghan.Position, maxDepth int) VCFResult {
	if maxDepth > 4 {
		maxDepth = 4
	}
	if pos.TotalPieces() > 43 {
		return VCFResult{CanWin: false}
	}

	history := make(map[uint64]bool)
	history[hashPosition(pos)] = true

	moves := pos.GenerateLegalMoves(true)
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok { continue }

		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			return VCFResult{CanWin: true, Move: mv}
		}

		if !nextPos.IsInCheck(nextPos.SideToMove) {
			continue
		}

		if !e.canEscapeVCF(nextPos, maxDepth-1, history) {
			return VCFResult{CanWin: true, Move: mv}
		}
	}

	return VCFResult{CanWin: false}
}

func (e *Engine) checkVCF(pos *xionghan.Position, depth int, history map[uint64]bool) bool {
	if depth <= 0 { return false }
	h := hashPosition(pos)
	if history[h] { return false }
	history[h] = true
	defer delete(history, h)

	moves := pos.GenerateLegalMoves(true)
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok { continue }

		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing { return true }

		if !nextPos.IsInCheck(nextPos.SideToMove) { continue }

		if !e.canEscapeVCF(nextPos, depth-1, history) {
			return true
		}
	}
	return false
}

func (e *Engine) canEscapeVCF(pos *xionghan.Position, depth int, history map[uint64]bool) bool {
	moves := pos.GenerateLegalMoves(true)
	if len(moves) == 0 { return false }

	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok { continue }
		if !e.checkVCF(nextPos, depth-1, history) {
			return true 
		}
	}
	return false
}

func (e *Engine) CanCaptureKingNext(pos *xionghan.Position) bool {
	moves := pos.GenerateLegalMoves(true)
	for _, mv := range moves {
		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			return true
		}
	}
	return false
}
