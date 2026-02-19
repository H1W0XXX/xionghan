package xionghan

import (
	"strings"
	"unicode"
)

const (
	Rows       = 13
	Cols       = 13
	NumSquares = Rows * Cols

	WallRow = 6 // 中间“长城”，0-based 第 7 行
)

func indexOf(row, col int) int { return row*Cols + col }
func rowOf(sq int) int         { return sq / Cols }
func colOf(sq int) int         { return sq % Cols }

func onBoard(row, col int) bool {
	return row >= 0 && row < Rows && col >= 0 && col < Cols
}

func opposite(side Side) Side {
	if side == Red {
		return Black
	}
	if side == Black {
		return Red
	}
	return NoSide
}

// 兵的前进方向：红向上(-1)，黑向下(+1)
func pawnDir(side Side) int {
	if side == Red {
		return -1
	}
	if side == Black {
		return +1
	}
	return 0
}

// 是否已经“过长城”
func pawnPassedWall(side Side, row int) bool {
	if side == Red {
		return row < WallRow
	}
	if side == Black {
		return row > WallRow
	}
	return false
}

// 是否在九宫
func inPalace(side Side, row, col int) bool {
	midCol := Cols / 2 // 6
	if col < midCol-1 || col > midCol+1 {
		return false
	}
	if side == Black {
		return row >= 1 && row <= 3
	}
	if side == Red {
		return row >= Rows-4 && row <= Rows-2 // 9..11
	}
	return false
}

var letterToPieceType = map[rune]PieceType{
	'a': PieceRook,     // 车 chariot
	'b': PieceKnight,   // 马 horse
	'c': PieceElephant, // 相 / 都 elephant
	'd': PieceAdvisor,  // 士 / 氏 advisor
	'e': PieceKing,     // 皇 / 单 king
	'f': PieceCannon,   // 炮 cannon
	'g': PiecePawn,     // 兵 / 卒 pawn
	'h': PieceLei,      // 檑 lei
	'i': PieceFeng,     // 锋 feng
	'j': PieceWei,      // 卫 wei
}

func pieceToChar(p Piece) rune {
	if p == 0 {
		return '.'
	}
	pt := p.Type()
	var base rune
	for k, v := range letterToPieceType {
		if v == pt {
			base = k
			break
		}
	}
	if base == 0 {
		return '.'
	}
	if p.Side() == Red {
		return unicode.ToUpper(base)
	}
	return base
}

// 你之前看到的盘面
const initialBoardString = `i.a.h...h.a.i
...bcdedcb...
.............
.f.........f.
..g.g.g.g.g..
j...........j
.............
J...........J
..G.G.G.G.G..
.F.........F.
.............
...BCDEDCB...
I.A.H...H.A.I`

func parseInitialBoard() Board {
	var b Board
	lines := make([]string, 0, Rows)
	for _, line := range strings.Split(initialBoardString, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) != Rows {
		panic("initialBoardString 行数不为 13")
	}
	for r := 0; r < Rows; r++ {
		if len(lines[r]) != Cols {
			panic("initialBoardString 列数不为 13")
		}
		for c, ch := range lines[r] {
			if ch == '.' {
				continue
			}
			isUpper := unicode.IsUpper(ch)
			base := unicode.ToLower(ch)
			pt, ok := letterToPieceType[base]
			if !ok {
				panic("unknown piece letter: " + string(ch))
			}
			side := Black
			if isUpper {
				side = Red
			}
			b.Squares[indexOf(r, c)] = makePiece(side, pt)
		}
	}
	return b
}

func NewInitialPosition() *Position {
	pos := &Position{
		Board:      parseInitialBoard(),
		SideToMove: Red, // 红先
	}
	pos.Hash = pos.CalculateHash()
	return pos
}
