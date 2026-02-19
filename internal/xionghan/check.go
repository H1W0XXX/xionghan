package xionghan

// IsAttacked 判断 sq 这个格子是否被 bySide 这一方攻击。
// 采用走法模拟：只要对方任何一个棋子能合法地“走到”这个位置，就说明该位置被攻击。
func (p *Position) IsAttacked(sq int, bySide Side) bool {
	// 关键：GeneratePseudoMovesForSide 会使用棋子本身的规则生成所有可能的落点
	pseudoMoves := p.GeneratePseudoMovesForSide(bySide)
	for _, mv := range pseudoMoves {
		if mv.To == sq {
			return true
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
