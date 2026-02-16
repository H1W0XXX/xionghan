package xionghan

var (
	fengStations  [NumSquares]bool
	fengRoad      [NumSquares]bool
	fengMoveTable [NumSquares][][]int // [from][dir] -> 一条轨道路径
)

func init() {
	initFengTables()
}

func initFengTables() {
	const fengStep = 3
	const startPos = 0 // 先用左上角，后面你可以根据规则改

	// 1. 锋站：对角线每次跳 fengStep
	var q []int
	fengStations[startPos] = true
	q = append(q, startPos)
	for qi := 0; qi < len(q); qi++ {
		now := q[qi]
		r, c := rowOf(now), colOf(now)
		for _, d := range bishopDirs {
			r2 := r + d[0]*fengStep
			c2 := c + d[1]*fengStep
			if !onBoard(r2, c2) {
				continue
			}
			to := indexOf(r2, c2)
			if !fengStations[to] {
				fengStations[to] = true
				q = append(q, to)
			}
		}
	}

	// 2. 轨道：以每个锋站为中心，向四个对角方向延伸 fengStep-1 步
	for sq := 0; sq < NumSquares; sq++ {
		if !fengStations[sq] {
			continue
		}
		fengRoad[sq] = true
		r, c := rowOf(sq), colOf(sq)
		for _, d := range bishopDirs {
			for step := 1; step < fengStep; step++ {
				r2 := r + d[0]*step
				c2 := c + d[1]*step
				if !onBoard(r2, c2) {
					break
				}
				fengRoad[indexOf(r2, c2)] = true
			}
		}
	}

	// 3. 每个格子的轨道路径预计算
	for sq := 0; sq < NumSquares; sq++ {
		if !fengRoad[sq] {
			continue
		}
		r, c := rowOf(sq), colOf(sq)
		var dirs [][]int
		for _, d := range bishopDirs {
			var line []int
			r2, c2 := r+d[0], c+d[1]
			for onBoard(r2, c2) {
				to := indexOf(r2, c2)
				if !fengRoad[to] {
					break
				}
				line = append(line, to)
				if fengStations[to] {
					break
				}
				r2 += d[0]
				c2 += d[1]
			}
			if len(line) > 0 {
				dirs = append(dirs, line)
			}
		}
		fengMoveTable[sq] = dirs
	}
}

// 锋：只能走轨道；走子不越子、不越锋站；吃子只有“从锋站出发”时可以吃到路径上的第一个敌子
func genFengMoves(p *Position, from int, moves *[]Move) {
	if !fengRoad[from] {
		return
	}
	side := p.Board.Squares[from].Side()
	canAttack := fengStations[from]
	for _, line := range fengMoveTable[from] {
		for _, to := range line {
			dst := p.Board.Squares[to]
			if dst == 0 {
				*moves = append(*moves, Move{From: from, To: to})
			} else {
				if canAttack && dst.Side() != side {
					*moves = append(*moves, Move{From: from, To: to})
				}
				break
			}
			if fengStations[to] {
				break
			}
		}
	}
}
