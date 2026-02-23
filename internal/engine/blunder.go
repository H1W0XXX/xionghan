package engine

import (
	"sync/atomic"
	"xionghan/internal/xionghan"
)

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
)

// FilterBlunderMoves 过滤“纯送子”弱智步
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
	mask := uint64(len(e.blunderTT) - 1)
	idx := key & mask

	// 原子读取 (Lock-free)
	entry := atomic.LoadUint64(&e.blunderTT[idx])
	// 校验 key (高 56 位)
	if (entry >> 8) == (key >> 8) {
		return uint8(entry&0xFF) == blunderPrune
	}

	prune := e.computeBlunderPrune(pos, mv)
	
	// 原子写入 (总是覆盖)
	val := blunderKeep
	if prune {
		val = blunderPrune
	}
	newEntry := (key & 0xFFFFFFFFFFFFFF00) | uint64(val)
	atomic.StoreUint64(&e.blunderTT[idx], newEntry)
	
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
		if !e.hasRecaptureOrCheck(afterCapture, mv.To) {
			return true
		}
	}

	return false
}

func (e *Engine) hasRecaptureOrCheck(pos *xionghan.Position, targetSq int) bool {
	key := blunderReplyKey(pos, targetSq)
	mask := uint64(len(e.blunderReplyTT) - 1)
	idx := key & mask

	entry := atomic.LoadUint64(&e.blunderReplyTT[idx])
	if (entry >> 8) == (key >> 8) {
		return uint8(entry&0xFF) == blunderReplyHasComp
	}

	moves := pos.GenerateLegalMoves(false)
	hasComp := false
	for _, mv := range moves {
		if mv.To == targetSq {
			dst := pos.Board.Squares[targetSq]
			if dst != 0 && dst.Side() != pos.SideToMove {
				hasComp = true
				break
			}
		}

		after, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}
		target := pos.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing {
			hasComp = true
			break
		}
		if after.IsInCheck(after.SideToMove) {
			hasComp = true
			break
		}
	}

	// 存入 (无锁写入)
	val := blunderReplyNoComp
	if hasComp {
		val = blunderReplyHasComp
	}
	newEntry := (key & 0xFFFFFFFFFFFFFF00) | uint64(val)
	atomic.StoreUint64(&e.blunderReplyTT[idx], newEntry)
	
	return hasComp
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
