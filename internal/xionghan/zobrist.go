package xionghan

import "sync"

const zobristPieceTypes = 11 // PieceType 范围 [1..10]，0 保留空位不用

var (
	zobristOnce sync.Once

	zobristPieces [2][zobristPieceTypes][NumSquares]uint64
	zobristSide   uint64
)

func initZobrist() {
	zobristOnce.Do(func() {
		seed := uint64(0x9E3779B97F4A7C15)
		next := func() uint64 {
			seed += 0x9E3779B97F4A7C15
			z := seed
			z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
			z = (z ^ (z >> 27)) * 0x94D049BB133111EB
			return z ^ (z >> 31)
		}

		for side := 0; side < 2; side++ {
			for pt := 1; pt < zobristPieceTypes; pt++ {
				for sq := 0; sq < NumSquares; sq++ {
					zobristPieces[side][pt][sq] = next()
				}
			}
		}
		zobristSide = next()
	})
}

func pieceHashKey(pc Piece, sq int) uint64 {
	if pc == 0 || sq < 0 || sq >= NumSquares {
		return 0
	}

	var sideIdx int
	switch pc.Side() {
	case Red:
		sideIdx = 0
	case Black:
		sideIdx = 1
	default:
		return 0
	}

	pt := int(pc.Type())
	if pt <= 0 || pt >= zobristPieceTypes {
		return 0
	}
	return zobristPieces[sideIdx][pt][sq]
}

// CalculateHash 全量计算当前局面的 Zobrist 哈希。
func (p *Position) CalculateHash() uint64 {
	initZobrist()

	var h uint64
	for sq := 0; sq < NumSquares; sq++ {
		pc := p.Board.Squares[sq]
		if pc == 0 {
			continue
		}
		h ^= pieceHashKey(pc, sq)
	}
	if p.SideToMove == Black {
		h ^= zobristSide
	}
	return h
}

// EnsureHash 确保 Position.Hash 已初始化；返回当前哈希值。
func (p *Position) EnsureHash() uint64 {
	if p.Hash == 0 {
		p.Hash = p.CalculateHash()
	}
	return p.Hash
}
