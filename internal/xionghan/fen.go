package xionghan

import (
	"errors"
	"strings"
	"unicode"
)

// 简单 FEN-like：13行用“/”隔开，空位用数字压缩；空格后 w/b 表示先后
func (p *Position) Encode() string {
	var sb strings.Builder
	for r := 0; r < Rows; r++ {
		if r > 0 {
			sb.WriteByte('/')
		}
		empty := 0
		for c := 0; c < Cols; c++ {
			sq := indexOf(r, c)
			pc := p.Board.Squares[sq]
			if pc == 0 {
				empty++
				continue
			}
			if empty > 0 {
				sb.WriteByte(byte('0' + empty))
				empty = 0
			}
			sb.WriteRune(pieceToChar(pc))
		}
		if empty > 0 {
			sb.WriteByte(byte('0' + empty))
		}
	}
	sb.WriteByte(' ')
	if p.SideToMove == Red {
		sb.WriteByte('w')
	} else {
		sb.WriteByte('b')
	}
	return sb.String()
}

var ErrInvalidFEN = errors.New("invalid FEN")

func DecodePosition(fen string) (*Position, error) {
	parts := strings.Split(fen, " ")
	if len(parts) < 2 {
		return nil, ErrInvalidFEN
	}
	rows := strings.Split(parts[0], "/")
	if len(rows) != Rows {
		return nil, ErrInvalidFEN
	}
	var b Board
	for r := 0; r < Rows; r++ {
		row := rows[r]
		c := 0
		for _, ch := range row {
			if c >= Cols {
				return nil, ErrInvalidFEN
			}
			if ch >= '1' && ch <= '9' {
				n := int(ch - '0')
				c += n
				continue
			}
			if ch == '.' {
				c++
				continue
			}
			isUpper := unicode.IsUpper(ch)
			base := unicode.ToLower(ch)
			pt, ok := letterToPieceType[base]
			if !ok {
				return nil, ErrInvalidFEN
			}
			side := Black
			if isUpper {
				side = Red
			}
			b.Squares[indexOf(r, c)] = makePiece(side, pt)
			c++
		}
		if c != Cols {
			return nil, ErrInvalidFEN
		}
	}
	var stm Side
	if parts[1] == "w" {
		stm = Red
	} else {
		stm = Black
	}
	pos := &Position{
		Board:      b,
		SideToMove: stm,
	}
	pos.Hash = pos.CalculateHash()
	return pos, nil
}
