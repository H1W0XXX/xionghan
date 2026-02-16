package xionghan

// 尉：走=水平偶数格，不越子，不吃；吃=左右第2格，中间不能有子
func genWeiMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	side := p.Board.Squares[from].Side()

	// 1. 走子：左右偶数格，不越子、不落在有子格
	for _, dc := range []int{-1, +1} {
		for step := 2; ; step += 2 {
			c2 := col + dc*step
			if !onBoard(row, c2) {
				break
			}
			block := false
			for mid := col + dc; mid != c2; mid += dc {
				if p.Board.Squares[indexOf(row, mid)] != 0 {
					block = true
					break
				}
			}
			if block {
				break
			}
			to := indexOf(row, c2)
			if p.Board.Squares[to] != 0 {
				break
			}
			*moves = append(*moves, Move{From: from, To: to})
		}
	}

	// 2. 吃子：左右第2格，中间不能有子
	for _, dc := range []int{-1, +1} {
		mc := col + dc
		tc := col + 2*dc
		if !onBoard(row, tc) || !onBoard(row, mc) {
			continue
		}
		if p.Board.Squares[indexOf(row, mc)] != 0 {
			continue
		}
		to := indexOf(row, tc)
		dst := p.Board.Squares[to]
		if dst != 0 && dst.Side() != side {
			*moves = append(*moves, Move{From: from, To: to})
		}
	}
}
