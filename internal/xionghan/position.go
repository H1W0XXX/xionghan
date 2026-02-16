package xionghan

const (
	BoardRows = 13
	BoardCols = 13
)

func (p *Position) kingsFace() bool {
	redKing := -1
	blackKing := -1

	for sq, pc := range p.Board.Squares {
		if pc == 0 {
			continue
		}
		if pc.Type() != PieceKing {
			continue
		}
		if pc.Side() == Red {
			redKing = sq
		} else {
			blackKing = sq
		}
	}

	if redKing == -1 || blackKing == -1 {
		// 有一方王已经没了：对局终结，但不存在“对脸”问题
		return false
	}

	rx, ry := redKing/BoardCols, redKing%BoardCols
	bx, by := blackKing/BoardCols, blackKing%BoardCols

	if ry != by {
		// 不在同一列，不可能对脸
		return false
	}

	// 检查两王之间是否有挡子
	if rx > bx {
		rx, bx = bx, rx
	}
	for x := rx + 1; x < bx; x++ {
		sq := x*BoardCols + ry
		if p.Board.Squares[sq] != 0 {
			return false // 中间有子，不算“对脸”
		}
	}
	return true // 同列且中间无子 -> 王对脸，非法
}

func (p *Position) KingExists(side Side) bool {
	for _, pc := range p.Board.Squares {
		if pc != 0 && pc.Type() == PieceKing && pc.Side() == side {
			return true
		}
	}
	return false
}
