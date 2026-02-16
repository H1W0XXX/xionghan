package xionghan

// IsAttacked 判断 sq 这个格子是否被 side 这一方攻击
func (p *Position) IsAttacked(sq int, bySide Side) bool {
	r, c := sq/Cols, sq%Cols

	// 1. 直线攻击 (车、炮、檑、王对脸)
	dirs := []struct{ dr, dc int }{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	for _, d := range dirs {
		count := 0
		for i := 1; i < 13; i++ {
			nr, nc := r+d.dr*i, c+d.dc*i
			if nr < 0 || nr >= Rows || nc < 0 || nc >= Cols {
				break
			}
			targetSq := nr*Cols + nc
			pc := p.Board.Squares[targetSq]
			if pc == 0 {
				continue
			}
			
			if pc.Side() == bySide {
				pt := pc.Type()
				if count == 0 {
					// 车直接攻击，或者王对脸
					if pt == PieceRook || pt == PieceKing {
						return true
					}
				} else if count == 1 {
					// 炮或檑隔子攻击
					if pt == PieceCannon || pt == PieceLei {
						return true
					}
				}
				break // 遇到子就停
			} else {
				// 遇到我方子（作为炮架）
				count++
				if count > 1 {
					break
				}
			}
		}
	}

	// 2. 马攻击
	knightDirs := []struct{ dr, dc, mr, mc int }{
		{-2, -1, -1, 0}, {-2, 1, -1, 0},
		{2, -1, 1, 0}, {2, 1, 1, 0},
		{-1, -2, 0, -1}, {1, -2, 0, -1},
		{-1, 2, 0, 1}, {1, 2, 0, 1},
	}
	for _, d := range knightDirs {
		nr, nc := r+d.dr, c+d.dc
		if nr >= 0 && nr < Rows && nc >= 0 && nc < Cols {
			// 检查蹩马腿
			mr, mc := r+d.mr, c+d.mc
			if p.Board.Squares[mr*Cols+mc] == 0 {
				pc := p.Board.Squares[nr*Cols+nc]
				if pc != 0 && pc.Side() == bySide && pc.Type() == PieceKnight {
					return true
				}
			}
		}
	}

	// 3. 兵攻击
	// 获取对方兵的前进方向：红向上(-1)，黑走向下(+1)
	pawnMoveDir := pawnDir(bySide)

	// 检查对方兵是否在能吃到 sq 的位置
	// 兵只能直走吃(未过河)或横走吃(已过河)
	checkPawn := func(pr, pc int) bool {
		if pr < 0 || pr >= Rows || pc < 0 || pc >= Cols { return false }
		targetPc := p.Board.Squares[pr*Cols+pc]
		return targetPc != 0 && targetPc.Side() == bySide && targetPc.Type() == PiecePawn
	}

	// 对方兵在 sq 的“后方”（即 pr + pawnMoveDir = r => pr = r - pawnMoveDir）
	if checkPawn(r-pawnMoveDir, c) { return true }
	// 如果对方兵已经过河，还可以横向攻击
	// 注意这里判断对方兵是否过河，需要用对方兵所在的行，但由于横向攻击行不变，直接用 r 即可
	if pawnPassedWall(bySide, r) {
		if c > 0 && checkPawn(r, c-1) { return true }
		if c < Cols-1 && checkPawn(r, c+1) { return true }
	}

	return false
}

// IsInCheck 判断 side 这一方的王是否被将军
func (p *Position) IsInCheck(side Side) bool {
	kingSq := -1
	for sq, pc := range p.Board.Squares {
		if pc != 0 && pc.Side() == side && pc.Type() == PieceKing {
			kingSq = sq
			break
		}
	}
	if kingSq == -1 {
		return false // 王都没了
	}
	return p.IsAttacked(kingSq, opposite(side))
}

// IsAttackedByPawn 专门判断 sq 是否被 bySide 的兵攻击
func (p *Position) IsAttackedByPawn(sq int, bySide Side) bool {
	r, c := sq/Cols, sq%Cols
	pawnMoveDir := pawnDir(bySide)

	checkPawn := func(pr, pc int) bool {
		if pr < 0 || pr >= Rows || pc < 0 || pc >= Cols {
			return false
		}
		targetPc := p.Board.Squares[pr*Cols+pc]
		return targetPc != 0 && targetPc.Side() == bySide && targetPc.Type() == PiecePawn
	}

	if checkPawn(r-pawnMoveDir, c) {
		return true
	}
	// 过长城后的横向攻击
	if pawnPassedWall(bySide, r) {
		if c > 0 && checkPawn(r, c-1) {
			return true
		}
		if c < Cols-1 && checkPawn(r, c+1) {
			return true
		}
	}
	return false
}
