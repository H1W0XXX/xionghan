package xionghan

type Side int8

const (
	NoSide Side = -1
	Red    Side = 0
	Black  Side = 1
)

type PieceType int8

const (
	PieceNone     PieceType = iota
	PieceRook               // 车
	PieceKnight             // 马
	PieceCannon             // 炮
	PieceElephant           // 相
	PieceAdvisor            // 士
	PieceKing               // 皇帝 / 单于
	PiecePawn               // 卒
	PieceLei                // 檑
	PieceFeng               // 锋
	PieceWei                // 尉
)

type Piece int8 // 0=空；>0 红；<0 黑；abs=PieceType

func makePiece(side Side, pt PieceType) Piece {
	if pt == PieceNone || side == NoSide {
		return 0
	}
	if side == Red {
		return Piece(pt)
	}
	return -Piece(pt)
}

func (p Piece) Type() PieceType {
	if p < 0 {
		return PieceType(-p)
	}
	return PieceType(p)
}

func (p Piece) Side() Side {
	if p == 0 {
		return NoSide
	}
	if p > 0 {
		return Red
	}
	return Black
}

type Board struct {
	Squares [NumSquares]Piece
}

type Move struct {
	From  int `json:"from"`
	To    int `json:"to"`
	Score int `json:"-"` // 用于搜索排序，不进行 JSON 序列化
}

// Position = 棋盘 + 轮到谁走（先不管王车易位之类）
type Position struct {
	Board      Board
	SideToMove Side
	Hash       uint64
}
