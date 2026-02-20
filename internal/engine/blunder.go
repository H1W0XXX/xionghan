package engine

import "xionghan/internal/xionghan"

const (
	blunderUnknown uint8 = iota
	blunderKeep
	blunderPrune
)

const (
	blunderReplyNoComp uint8 = iota
	blunderReplyHasComp
)

const (
	blunderMoveSalt  uint64 = 0x9e3779b97f4a7c15
	blunderReplySalt uint64 = 0xc2b2ae3d27d4eb4f
	blunderTTCap            = 1_000_000
)

// FilterBlunderMoves 过滤“纯送子”弱智步：
// 仅针对车/炮/马/檑/兵，且本步不吃子、不将军。
func (e *Engine) FilterBlunderMoves(pos *xionghan.Position, moves []xionghan.Move) []xionghan.Move {
	if len(moves) <= 1 {
		return moves
	}

	safeMoves := make([]xionghan.Move, 0, len(moves))
	for _, mv := range moves {
		if e.shouldPruneBlunderMove(pos, mv) {
			continue
		}
		safeMoves = append(safeMoves, mv)
	}

	if len(safeMoves) == 0 {
		return moves
	}
	return safeMoves
}

func (e *Engine) shouldPruneBlunderMove(pos *xionghan.Position, mv xionghan.Move) bool {
	key := blunderMoveKey(pos, mv)
	if v, ok := e.blunderTT[key]; ok {
		return v == blunderPrune
	}

	prune := e.computeBlunderPrune(pos, mv)
	if len(e.blunderTT) > blunderTTCap {
		e.blunderTT = make(map[uint64]uint8, 1<<16)
	}
	if prune {
		e.blunderTT[key] = blunderPrune
	} else {
		e.blunderTT[key] = blunderKeep
	}
	return prune
}

func (e *Engine) computeBlunderPrune(pos *xionghan.Position, mv xionghan.Move) bool {
	moving := pos.Board.Squares[mv.From]
	if moving == 0 || moving.Side() != pos.SideToMove {
		return false
	}
	if !isBlunderFilterPiece(moving.Type()) {
		return false
	}

	target := pos.Board.Squares[mv.To]
	if target != 0 {
		return false
	}

	nextPos, ok := pos.ApplyMove(mv)
	if !ok {
		return false
	}
	// 将军步不在“纯送子”过滤范围。
	if nextPos.IsInCheck(nextPos.SideToMove) {
		return false
	}

	oppMoves := nextPos.GenerateLegalMoves(false)
	for _, reply := range oppMoves {
		if reply.To != mv.To {
			continue
		}
		attacker := nextPos.Board.Squares[reply.From]
		if attacker == 0 || attacker.Side() != nextPos.SideToMove {
			continue
		}

		afterCapture, ok := nextPos.ApplyMove(reply)
		if !ok {
			continue
		}
		// 只要对手存在一条吃回分支，让我方“既不能回吃也不能将军”，就视为纯送子。
		if !e.hasRecaptureOrCheck(afterCapture, mv.To) {
			return true
		}
	}

	// 对手没法立刻吃掉该子，或所有吃回都能被我方立即反制，不算纯送子。
	return false
}

func (e *Engine) hasRecaptureOrCheck(pos *xionghan.Position, targetSq int) bool {
	key := blunderReplyKey(pos, targetSq)
	if v, ok := e.blunderReplyTT[key]; ok {
		return v == blunderReplyHasComp
	}

	moves := pos.GenerateLegalMoves(false)
	for _, mv := range moves {
		if mv.To == targetSq {
			dst := pos.Board.Squares[targetSq]
			if dst != 0 && dst.Side() != pos.SideToMove {
				e.storeBlunderReply(key, true)
				return true
			}
		}

		after, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}
		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			e.storeBlunderReply(key, true)
			return true
		}
		if after.IsInCheck(after.SideToMove) {
			e.storeBlunderReply(key, true)
			return true
		}
	}

	e.storeBlunderReply(key, false)
	return false
}

func (e *Engine) storeBlunderReply(key uint64, hasComp bool) {
	if len(e.blunderReplyTT) > blunderTTCap {
		e.blunderReplyTT = make(map[uint64]uint8, 1<<16)
	}
	if hasComp {
		e.blunderReplyTT[key] = blunderReplyHasComp
	} else {
		e.blunderReplyTT[key] = blunderReplyNoComp
	}
}

func isBlunderFilterPiece(pt xionghan.PieceType) bool {
	switch pt {
	case xionghan.PieceRook, xionghan.PieceCannon, xionghan.PieceKnight, xionghan.PieceLei, xionghan.PiecePawn:
		return true
	default:
		return false
	}
}

func blunderMoveKey(pos *xionghan.Position, mv xionghan.Move) uint64 {
	moveBits := (uint64(uint16(mv.From)) << 16) | uint64(uint16(mv.To))
	return hashPosition(pos) ^ blunderMoveSalt ^ (moveBits * 0x9ddfea08eb382d69)
}

func blunderReplyKey(pos *xionghan.Position, targetSq int) uint64 {
	return hashPosition(pos) ^ blunderReplySalt ^ (uint64(targetSq+1) * 0x517cc1b727220a95)
}
