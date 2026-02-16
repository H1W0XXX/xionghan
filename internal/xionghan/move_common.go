package xionghan

// 车：横竖随便走
func genRookMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	side := p.SideToMove
	for _, d := range rookDirs {
		r, c := row+d[0], col+d[1]
		for onBoard(r, c) {
			to := indexOf(r, c)
			pc := p.Board.Squares[to]
			if pc == 0 {
				*moves = append(*moves, Move{From: from, To: to})
			} else {
				if pc.Side() != side {
					*moves = append(*moves, Move{From: from, To: to})
				}
				break
			}
			r += d[0]
			c += d[1]
		}
	}
}

// 炮：车走法 + 隔一子吃
func genCannonMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	side := p.SideToMove
	for _, d := range rookDirs {
		r, c := row+d[0], col+d[1]

		// 走子阶段：直到第一个棋子
		for onBoard(r, c) {
			to := indexOf(r, c)
			pc := p.Board.Squares[to]
			if pc == 0 {
				*moves = append(*moves, Move{From: from, To: to})
				r += d[0]
				c += d[1]
				continue
			}
			r += d[0]
			c += d[1]
			break
		}

		// 吃子阶段：越过炮架，遇到第一子可吃
		for onBoard(r, c) {
			to := indexOf(r, c)
			pc := p.Board.Squares[to]
			if pc != 0 {
				if pc.Side() != side {
					*moves = append(*moves, Move{From: from, To: to})
				}
				break
			}
			r += d[0]
			c += d[1]
		}
	}
}

// 相：田字 + 不过长城
func genElephantMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	pc := p.Board.Squares[from]
	side := pc.Side()
	for _, d := range bishopDirs {
		r := row + 2*d[0]
		c := col + 2*d[1]
		mr := row + d[0]
		mc := col + d[1]
		if !onBoard(r, c) {
			continue
		}
		if p.Board.Squares[indexOf(mr, mc)] != 0 {
			continue
		}
		if side == Red && r < WallRow {
			continue
		}
		if side == Black && r > WallRow {
			continue
		}
		dst := p.Board.Squares[indexOf(r, c)]
		if dst == 0 || dst.Side() != side {
			*moves = append(*moves, Move{From: from, To: indexOf(r, c)})
		}
	}
}

// 士：九宫内斜走一格
func genAdvisorMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	side := p.Board.Squares[from].Side()
	for _, d := range bishopDirs {
		r := row + d[0]
		c := col + d[1]
		if !onBoard(r, c) {
			continue
		}
		if !inPalace(side, r, c) {
			continue
		}
		dst := p.Board.Squares[indexOf(r, c)]
		if dst == 0 || dst.Side() != side {
			*moves = append(*moves, Move{From: from, To: indexOf(r, c)})
		}
	}
}

// 将：九宫内上下左右一格（暂时不处理“对将”规则）
func genKingMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	side := p.Board.Squares[from].Side()
	for _, d := range rookDirs {
		r := row + d[0]
		c := col + d[1]
		if !onBoard(r, c) {
			continue
		}
		if !inPalace(side, r, c) {
			continue
		}
		dst := p.Board.Squares[indexOf(r, c)]
		if dst == 0 || dst.Side() != side {
			*moves = append(*moves, Move{From: from, To: indexOf(r, c)})
		}
	}
}
