package xionghan

func genPawnMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	pc := p.Board.Squares[from]
	if pc == 0 {
		return
	}
	side := pc.Side()
	dir := pawnDir(side)

	// 先判断当前格子是不是已经“过长城”
	passed := pawnPassedWall(side, row)

	if passed {
		// ===== 已过长城：左右 + 前一格 =====

		// 前一格（可以吃子）
		r1 := row + dir
		if onBoard(r1, col) {
			to := indexOf(r1, col)
			dst := p.Board.Squares[to]
			if dst == 0 || dst.Side() != side {
				*moves = append(*moves, Move{From: from, To: to})
			}
		}

		// 左右一格（可以吃子）
		for _, dc := range []int{-1, +1} {
			c2 := col + dc
			if !onBoard(row, c2) {
				continue
			}
			to := indexOf(row, c2)
			dst := p.Board.Squares[to]
			if dst == 0 || dst.Side() != side {
				*moves = append(*moves, Move{From: from, To: to})
			}
		}
		return
	}

	// ===== 未过长城：构造“往前的一条射线” =====
	var ray []int
	for r := row + dir; onBoard(r, col); r += dir {
		to := indexOf(r, col)
		ray = append(ray, to)

		// 一旦到达敌境（pawnPassedWall==true），就把这一格作为最后一个候选，然后停
		if pawnPassedWall(side, r) {
			break
		}
	}

	if len(ray) == 0 {
		return
	}

	// ===== 未过长城时的走法（对标 XiongHanZu.GenerateMoves）=====
	for i, to := range ray {
		dst := p.Board.Squares[to]

		if dst == 0 {
			// 空格：可以走，继续往前
			*moves = append(*moves, Move{From: from, To: to})
			continue
		}

		// 遇到子了：
		if i == 0 && dst.Side() != side {
			// 只有“第一步”可以吃子
			*moves = append(*moves, Move{From: from, To: to})
		}
		// 不管能不能吃，遇到子以后都不能再往前
		break
	}
}
