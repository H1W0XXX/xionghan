package xionghan

// IsAttacked 判断 sq 这个格子是否被 bySide 这一方攻击。
// 采用走法模拟：只要对方任何一个棋子能合法地“走到”这个位置，就说明该位置被攻击。
func (p *Position) IsAttacked(sq int, bySide Side) bool {
	// VCF 优化：在判断攻击时，忽略无法将军的子力（象、士、卫）
	for s := 0; s < NumSquares; s++ {
		pc := p.Board.Squares[s]
		if pc == 0 || pc.Side() != bySide {
			continue
		}

		pt := pc.Type()
		// 象、士、卫 无法将军，直接跳过其走法生成
		if pt == PieceElephant || pt == PieceAdvisor || pt == PieceWei {
			continue
		}

		// 优化：兵、马、檑 只有在对方半场（过长城）才可能将军，在自己半场直接跳过
		if pt == PiecePawn || pt == PieceKnight || pt == PieceLei {
			r := s / Cols
			isOpponentHalf := false
			if bySide == Red {
				isOpponentHalf = r < WallRow // 红方在上方（0-5行）为对方半场
			} else {
				isOpponentHalf = r > WallRow // 黑方在下方（7-12行）为对方半场
			}
			if !isOpponentHalf {
				continue
			}
		}

		var moves []Move
		switch pt {
		case PieceRook:
			genRookMoves(p, s, &moves)
		case PieceCannon:
			genCannonMoves(p, s, &moves)
		case PieceKnight:
			genKnightMoves(p, s, &moves)
		case PieceKing:
			genKingMoves(p, s, &moves)
		case PiecePawn:
			genPawnMoves(p, s, &moves)
		case PieceLei:
			genLeiMoves(p, s, &moves)
		case PieceFeng:
			genFengMoves(p, s, &moves)
		}

		for _, mv := range moves {
			if mv.To == sq {
				return true
			}
		}
	}
	return false
}

// IsInCheck 判断 side 这一方的王是否被将军
func (p *Position) IsInCheck(side Side) bool {
	kingSq := -1
	for s, pc := range p.Board.Squares {
		if pc != 0 && pc.Side() == side && pc.Type() == PieceKing {
			kingSq = s
			break
		}
	}
	if kingSq == -1 {
		return false
	}
	// 对方是否能走到我方王的位置
	return p.IsAttacked(kingSq, opposite(side))
}

// IsAttackedByPawn 专门用于 AI 启发式过滤
func (p *Position) IsAttackedByPawn(sq int, bySide Side) bool {
	r, c := sq/Cols, sq%Cols
	pawnMoveDir := pawnDir(bySide)
	checkP := func(pr, pc int) bool {
		if pr < 0 || pr >= Rows || pc < 0 || pc >= Cols { return false }
		targetPc := p.Board.Squares[pr*Cols+pc]
		return targetPc != 0 && targetPc.Side() == bySide && targetPc.Type() == PiecePawn
	}
	if checkP(r-pawnMoveDir, c) { return true }
	if pawnPassedWall(bySide, r) {
		if (c > 0 && checkP(r, c-1)) || (c < Cols-1 && checkP(r, c+1)) { return true }
	}
	return false
}
