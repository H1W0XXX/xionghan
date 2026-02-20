package engine

import "xionghan/internal/xionghan"

type leiLockSetup struct {
	leftRook   int
	leftKnight int
	leftLei    int

	rightRook   int
	rightKnight int
	rightLei    int
}

var (
	blackLeiLockSetup = leiLockSetup{
		leftRook:    boardSq(0, 2),
		leftKnight:  boardSq(1, 3),
		leftLei:     boardSq(0, 4),
		rightRook:   boardSq(0, 10),
		rightKnight: boardSq(1, 9),
		rightLei:    boardSq(0, 8),
	}
	redLeiLockSetup = leiLockSetup{
		leftRook:    boardSq(12, 2),
		leftKnight:  boardSq(11, 3),
		leftLei:     boardSq(12, 4),
		rightRook:   boardSq(12, 10),
		rightKnight: boardSq(11, 9),
		rightLei:    boardSq(12, 8),
	}
)

func boardSq(row, col int) int {
	return row*xionghan.Cols + col
}

func leiLockSetupForSide(side xionghan.Side) (leiLockSetup, bool) {
	if side == xionghan.Red {
		return redLeiLockSetup, true
	}
	if side == xionghan.Black {
		return blackLeiLockSetup, true
	}
	return leiLockSetup{}, false
}

func pieceAtIs(pos *xionghan.Position, sq int, side xionghan.Side, pt xionghan.PieceType) bool {
	if sq < 0 || sq >= xionghan.NumSquares {
		return false
	}
	pc := pos.Board.Squares[sq]
	return pc != 0 && pc.Side() == side && pc.Type() == pt
}

// FilterLeiLockedMoves:
// 在子力>=42时，如果同侧某一边的“马+车”仍与初始位一致，则该边的檑不能移动。
func (e *Engine) FilterLeiLockedMoves(pos *xionghan.Position, moves []xionghan.Move) []xionghan.Move {
	_ = e
	if len(moves) <= 1 {
		return moves
	}
	if pos.TotalPieces() < 42 {
		return moves
	}

	setup, ok := leiLockSetupForSide(pos.SideToMove)
	if !ok {
		return moves
	}

	leftLocked := pieceAtIs(pos, setup.leftRook, pos.SideToMove, xionghan.PieceRook) &&
		pieceAtIs(pos, setup.leftKnight, pos.SideToMove, xionghan.PieceKnight)
	rightLocked := pieceAtIs(pos, setup.rightRook, pos.SideToMove, xionghan.PieceRook) &&
		pieceAtIs(pos, setup.rightKnight, pos.SideToMove, xionghan.PieceKnight)

	if !leftLocked && !rightLocked {
		return moves
	}

	filtered := make([]xionghan.Move, 0, len(moves))
	for _, mv := range moves {
		pc := pos.Board.Squares[mv.From]
		if pc == 0 || pc.Side() != pos.SideToMove || pc.Type() != xionghan.PieceLei {
			filtered = append(filtered, mv)
			continue
		}
		if leftLocked && mv.From == setup.leftLei {
			continue
		}
		if rightLocked && mv.From == setup.rightLei {
			continue
		}
		filtered = append(filtered, mv)
	}
	return filtered
}
