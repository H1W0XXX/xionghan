package xionghan

// 中象马 8 种“日”字：终点 + 马腿
var knightLegMoves = [8]struct {
	Dr, Dc int // 终点
	Br, Bc int // 马腿
}{
	{-2, -1, -1, 0},
	{-2, +1, -1, 0},
	{-1, -2, 0, -1},
	{-1, +2, 0, +1},
	{+1, -2, 0, -1},
	{+1, +2, 0, +1},
	{+2, -1, +1, 0},
	{+2, +1, +1, 0},
}

func genKnightMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	side := p.Board.Squares[from].Side()

	// 1. 普通日字马
	for _, m := range knightLegMoves {
		r := row + m.Dr
		c := col + m.Dc
		if !onBoard(r, c) {
			continue
		}
		br := row + m.Br
		bc := col + m.Bc
		if p.Board.Squares[indexOf(br, bc)] != 0 {
			continue // 憋马腿
		}
		to := indexOf(r, c)
		dst := p.Board.Squares[to]
		if dst == 0 || dst.Side() != side {
			*moves = append(*moves, Move{From: from, To: to})
		}
	}

	// 2. 直三：上下左右三格，中间两格必须全空
	straightDirs := [][2]int{{-1, 0}, {+1, 0}, {0, -1}, {0, +1}}
	for _, d := range straightDirs {
		r1, c1 := row+d[0], col+d[1]
		r2, c2 := row+2*d[0], col+2*d[1]
		r3, c3 := row+3*d[0], col+3*d[1]
		if !onBoard(r3, c3) {
			continue
		}
		if p.Board.Squares[indexOf(r1, c1)] != 0 {
			continue
		}
		if p.Board.Squares[indexOf(r2, c2)] != 0 {
			continue
		}
		to := indexOf(r3, c3)
		dst := p.Board.Squares[to]
		if dst == 0 || dst.Side() != side {
			*moves = append(*moves, Move{From: from, To: to})
		}
	}
}
