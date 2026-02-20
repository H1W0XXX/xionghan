package engine

import "xionghan/internal/xionghan"

// FilterUrgentPawnThreatMoves 在“非被将且非被立即绝杀风险”下，强制优先处理被兵下一手可吃的大子。
// 目标子仅包含：车、马、炮、檑。
func (e *Engine) FilterUrgentPawnThreatMoves(pos *xionghan.Position, moves []xionghan.Move) []xionghan.Move {
	if len(moves) <= 1 {
		return moves
	}

	side := pos.SideToMove
	if side != xionghan.Red && side != xionghan.Black {
		return moves
	}
	opp := oppositeSide(side)
	// 任一方被将军时，先走战术应将/攻将，不启用该过滤。
	if pos.IsInCheck(side) || pos.IsInCheck(opp) {
		return moves
	}

	threatened := threatenedSquaresByEnemyPawn(pos, side)
	if len(threatened) == 0 {
		return moves
	}
	// 若任一方存在一步吃王或 VCF 强杀线，局面进入强制战术态，不启用该强制过滤。
	if canSideCaptureKingNext(pos, side) ||
		canSideCaptureKingNext(pos, opp) ||
		e.isUnderVCFThreat(pos, side) ||
		e.isUnderVCFThreat(pos, opp) {
		return moves
	}

	forcedSafe := make([]xionghan.Move, 0, len(moves))
	forcedAny := make([]xionghan.Move, 0, len(moves))
	for _, mv := range moves {
		if _, ok := threatened[mv.From]; !ok {
			continue
		}
		forcedAny = append(forcedAny, mv)

		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}
		if !nextPos.IsAttackedByPawn(mv.To, opp) {
			forcedSafe = append(forcedSafe, mv)
		}
	}

	if len(forcedSafe) > 0 {
		return forcedSafe
	}
	if len(forcedAny) > 0 {
		return forcedAny
	}
	return moves
}

func threatenedSquaresByEnemyPawn(pos *xionghan.Position, side xionghan.Side) map[int]struct{} {
	opp := oppositeSide(side)
	oppPos := *pos
	oppPos.SideToMove = opp
	oppPos.Hash = 0

	out := make(map[int]struct{}, 4)
	oppMoves := oppPos.GenerateLegalMoves(false)
	for _, mv := range oppMoves {
		attacker := oppPos.Board.Squares[mv.From]
		if attacker == 0 || attacker.Type() != xionghan.PiecePawn {
			continue
		}
		target := oppPos.Board.Squares[mv.To]
		if target == 0 || target.Side() != side {
			continue
		}
		switch target.Type() {
		case xionghan.PieceRook, xionghan.PieceKnight, xionghan.PieceCannon, xionghan.PieceLei:
			out[mv.To] = struct{}{}
		}
	}
	return out
}

func canSideCaptureKingNext(pos *xionghan.Position, attacker xionghan.Side) bool {
	if attacker != xionghan.Red && attacker != xionghan.Black {
		return false
	}
	tmp := *pos
	tmp.SideToMove = attacker
	tmp.Hash = 0

	moves := tmp.GenerateLegalMoves(false)
	for _, mv := range moves {
		target := tmp.Board.Squares[mv.To]
		if target != 0 && target.Type() == xionghan.PieceKing && target.Side() != attacker {
			return true
		}
	}
	return false
}

func (e *Engine) isUnderVCFThreat(pos *xionghan.Position, attacker xionghan.Side) bool {
	if attacker != xionghan.Red && attacker != xionghan.Black {
		return false
	}
	tmp := *pos
	tmp.SideToMove = attacker
	tmp.Hash = 0
	return e.VCFSearch(&tmp, vcfDepthFilter).CanWin
}

func oppositeSide(side xionghan.Side) xionghan.Side {
	if side == xionghan.Red {
		return xionghan.Black
	}
	if side == xionghan.Black {
		return xionghan.Red
	}
	return xionghan.NoSide
}
