package xionghan

type leiTarget struct {
	Pos int
	Leg []int // 前后两个邻居格（在盘内的）
}

var leiAdj [NumSquares][]leiTarget

func init() {
	initLeiAdj()
}

func initLeiAdj() {
	for sq := 0; sq < NumSquares; sq++ {
		row, col := rowOf(sq), colOf(sq)
		var arr []leiTarget
		for idx, d := range leiRingDirs {
			r := row + d[0]
			c := col + d[1]
			if !onBoard(r, c) {
				continue
			}
			to := indexOf(r, c)
			var legs []int
			prev := (idx + 7) % 8
			next := (idx + 1) % 8
			for _, id := range []int{prev, next} {
				r2 := row + leiRingDirs[id][0]
				c2 := col + leiRingDirs[id][1]
				if onBoard(r2, c2) {
					legs = append(legs, indexOf(r2, c2))
				}
			}
			arr = append(arr, leiTarget{Pos: to, Leg: legs})
		}
		leiAdj[sq] = arr
	}
}

// 檑：走=八方向像皇后（只走空格，不吃）；吃=只吃周围8格“落单棋子”
func genLeiMoves(p *Position, from int, moves *[]Move) {
	row, col := rowOf(from), colOf(from)
	side := p.Board.Squares[from].Side()

	// 1. 走子：8 方向任意步，只能落空格
	for _, d := range append(rookDirs, bishopDirs...) {
		r, c := row+d[0], col+d[1]
		for onBoard(r, c) {
			to := indexOf(r, c)
			if p.Board.Squares[to] != 0 {
				break // 不可吃子、不可越子
			}
			*moves = append(*moves, Move{From: from, To: to})
			r += d[0]
			c += d[1]
		}
	}

	// 2. 吃子：周围环上“落单棋子”
	for _, t := range leiAdj[from] {
		pc := p.Board.Squares[t.Pos]
		if pc == 0 || pc.Side() == side {
			continue
		}
		lone := true
		for _, leg := range t.Leg {
			if p.Board.Squares[leg] != 0 {
				lone = false
				break
			}
		}
		if lone {
			*moves = append(*moves, Move{From: from, To: t.Pos})
		}
	}
}
