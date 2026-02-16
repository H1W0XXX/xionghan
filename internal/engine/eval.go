package engine

import (
	"xionghan/internal/xionghan"
)

// ======= 基础子力估值（你原来的，保留） =======

var pieceValue = map[xionghan.PieceType]int{
	xionghan.PieceKing:     9999999, // 皇/单
	xionghan.PieceRook:     500,
	xionghan.PieceCannon:   480,
	xionghan.PieceKnight:   460,
	xionghan.PieceLei:      520, // 檑：残局很厉害，可以略高一点
	xionghan.PieceFeng:     360, // 锋
	xionghan.PieceWei:      260, // 卫
	xionghan.PiecePawn:     120, // 卒
	xionghan.PieceElephant: 250,
	xionghan.PieceAdvisor:  250,
}

// 从红方视角：score = 红方 - 黑方
// 材料 + “简单位置分” 一起算
func evaluateMaterialPositional(pos *xionghan.Position) int {
	score := 0

	for sq := 0; sq < xionghan.NumSquares; sq++ {
		pc := pos.Board.Squares[sq]
		if pc == 0 {
			continue
		}
		side := pc.Side()
		pt := pc.Type()
		base := pieceValue[pt]

		r := sq / xionghan.Cols
		c := sq % xionghan.Cols

		// 位置相关加成（从该子方视角）
		posBonus := piecePositionalBonus(pt, side, r, c)

		val := base + posBonus
		if side == xionghan.Red {
			score += val
		} else if side == xionghan.Black {
			score -= val
		}
	}

	return score
}

// 计算某个棋子在 (row, col) 的位置加成（从 ownSide 视角）
// 所有返回值都是“对这个 side 的加分”，外面再根据红/黑做正负号变换
func piecePositionalBonus(pt xionghan.PieceType, side xionghan.Side, row, col int) int {
	midCol := xionghan.Cols / 2
	// 自家方向上的“前进距离”，0 表示还在家附近，越大说明越向对方推进
	advance := rankFromSide(side, row)

	centerDist := abs(col - midCol)
	centerBonus := (6 - centerDist) // 越靠近中路分越高（范围大致 [-6,6]）

	switch pt {
	case xionghan.PiecePawn:
		return pawnPosBonus(side, row, col, advance, centerBonus)
	case xionghan.PieceRook:
		// 车：中央 & 列/行不太靠边更好
		b := centerBonus * 4
		if row > 2 && row < xionghan.Rows-3 {
			b += 4
		}
		return b
	case xionghan.PieceCannon:
		// 炮：中路略好，过长城略加分
		b := centerBonus * 3
		if pawnPassedWallLocal(side, row) {
			b += 8
		}
		return b
	case xionghan.PieceLei:
		// 檑：视作更偏攻击的长程子，喜欢中路+越过长城
		b := centerBonus * 5
		if pawnPassedWallLocal(side, row) {
			b += 12
		}
		return b
	case xionghan.PieceKnight:
		// 马：靠中比边强，略偏喜欢在自己半场中段
		b := centerBonus * 5
		if advance >= 2 && advance <= 6 {
			b += 4
		}
		return b
	case xionghan.PieceFeng:
		// 锋：可以当成“冲锋型中子”，喜欢中路、过长城、向前推进
		b := centerBonus * 4
		b += advance * 2
		if pawnPassedWallLocal(side, row) {
			b += 10
		}
		return b
	case xionghan.PieceWei:
		// 卫：偏防守/辅助，贴近九宫和自家王附近给一点分
		return weiPosBonus(side, row, col, centerBonus)
	case xionghan.PieceElephant:
		// 相：在自己半场并且接近九宫更好
		return elephantPosBonus(side, row, col)
	case xionghan.PieceAdvisor:
		// 士：待在九宫内部的给挺高分，跑出去的扣分
		return advisorPosBonus(side, row, col)
	case xionghan.PieceKing:
		// 王：九宫内 + 中央列稍微好一点。真正安全性另算
		b := 0
		if isInPalaceLocal(side, row, col) {
			b += 8
			if col == midCol {
				b += 4
			}
		} else {
			b -= 12 // 王早出宫一般是负面（残局另外在安全里减轻）
		}
		return b
	}

	return 0
}

func pawnPosBonus(side xionghan.Side, row, col, advance, centerBonus int) int {
	b := 0

	// 兵本身前进带点分（鼓励向前）
	b += advance * 3

	// 过长城大加分
	if pawnPassedWallLocal(side, row) {
		b += 15
		// 在中路的话再鼓励一些
		midCol := xionghan.Cols / 2
		if col >= midCol-1 && col <= midCol+1 {
			b += 8
		}
	}

	// 兵在最底线（老兵）略微减分（已经失去前进能力）
	if (side == xionghan.Red && row == 0) || (side == xionghan.Black && row == xionghan.Rows-1) {
		b -= 8
	}

	// 稍微鼓励中心兵
	b += centerBonus * 2

	return b
}

func weiPosBonus(side xionghan.Side, row, col, centerBonus int) int {
	b := 0
	midCol := xionghan.Cols / 2

	// 靠近自家九宫略加分
	if side == xionghan.Red {
		if row >= xionghan.Rows-5 {
			b += 5
		}
	} else if side == xionghan.Black {
		if row <= 4 {
			b += 5
		}
	}

	// 靠中路一点点鼓励
	if col >= midCol-1 && col <= midCol+1 {
		b += 4
	}

	// 少量中心奖励
	b += centerBonus * 1

	return b
}

func elephantPosBonus(side xionghan.Side, row, col int) int {
	b := 0
	// 相尽量在自己半场
	if side == xionghan.Red && row < xionghan.WallRow {
		b -= 6
	}
	if side == xionghan.Black && row > xionghan.WallRow {
		b -= 6
	}

	// 稍微靠近九宫给点分
	midCol := xionghan.Cols / 2
	if col >= midCol-2 && col <= midCol+2 {
		b += 2
	}

	return b
}

func advisorPosBonus(side xionghan.Side, row, col int) int {
	b := 0
	if isInPalaceLocal(side, row, col) {
		b += 12
	} else {
		b -= 6
	}
	return b
}

// 一些权重，可之后慢慢调
const (
	kingMissingAdvisorPenalty  = 40
	kingMissingElephantPenalty = 30
	kingOutOfPalacePenalty     = 40

	kingRookDirectPressure   = 45
	kingCannonScreenPressure = 35
	kingLeiPressure          = 35 // 先按炮的屏风来算，规则细节你以后可以再适配
)

func evaluateKingSafety(pos *xionghan.Position) int {
	score := 0

	for _, side := range []xionghan.Side{xionghan.Red, xionghan.Black} {
		kingSq := findKing(pos, side)
		if kingSq == -1 {
			continue // 已经被吃？这一般在终局判定里处理
		}

		row := kingSq / xionghan.Cols
		col := kingSq % xionghan.Cols

		// 统计士相数量
		numAdvisor, numElephant := countAdvisorElephant(pos, side)

		penalty := 0

		if numAdvisor < 2 {
			penalty += (2 - numAdvisor) * kingMissingAdvisorPenalty
		}
		if numElephant < 2 {
			penalty += (2 - numElephant) * kingMissingElephantPenalty
		}

		if !isInPalaceLocal(side, row, col) {
			penalty += kingOutOfPalacePenalty
		}

		// 直线车/炮/檑的威胁（只看最近一门，简单版）
		penalty += straightLongRangePressure(pos, side, kingSq)

		if side == xionghan.Red {
			score -= penalty // 红王不安全 -> 对红来说是减分
		} else {
			score += penalty // 黑王不安全 -> 对红来说是加分
		}
	}

	return score
}

func findKing(pos *xionghan.Position, side xionghan.Side) int {
	for sq := 0; sq < xionghan.NumSquares; sq++ {
		pc := pos.Board.Squares[sq]
		if pc == 0 {
			continue
		}
		if pc.Side() == side && pc.Type() == xionghan.PieceKing {
			return sq
		}
	}
	return -1
}

func countAdvisorElephant(pos *xionghan.Position, side xionghan.Side) (numAdvisor, numElephant int) {
	for sq := 0; sq < xionghan.NumSquares; sq++ {
		pc := pos.Board.Squares[sq]
		if pc == 0 || pc.Side() != side {
			continue
		}
		switch pc.Type() {
		case xionghan.PieceAdvisor:
			numAdvisor++
		case xionghan.PieceElephant:
			numElephant++
		}
	}
	return
}

// 四个正方向上查：敌方 车/炮/檑 的直线威胁
func straightLongRangePressure(pos *xionghan.Position, side xionghan.Side, kingSq int) int {
	enemy := oppositeSide(side)
	kr := kingSq / xionghan.Cols
	kc := kingSq % xionghan.Cols

	type dir struct{ dr, dc int }
	dirs := []dir{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}

	total := 0

	for _, d := range dirs {
		r, c := kr+d.dr, kc+d.dc
		screenCount := 0

		for r >= 0 && r < xionghan.Rows && c >= 0 && c < xionghan.Cols {
			sq := r*xionghan.Cols + c
			pc := pos.Board.Squares[sq]

			if pc != 0 {
				if pc.Side() == enemy {
					switch pc.Type() {
					case xionghan.PieceRook:
						if screenCount == 0 {
							total += kingRookDirectPressure
						}
					case xionghan.PieceCannon:
						if screenCount == 1 {
							total += kingCannonScreenPressure
						}
					case xionghan.PieceLei:
						// 暂时按“类似炮”处理：隔一个子
						if screenCount == 1 {
							total += kingLeiPressure
						}
					}
				}
				screenCount++
				// 对车来说，一旦挡住就可以停；对炮/檑屏数>1 也没意义了
				if screenCount > 1 {
					break
				}
			}

			r += d.dr
			c += d.dc
		}
	}

	return total
}

const mobilityWeight = 2
const tempoBonus = 5

func evaluateMobility(pos *xionghan.Position) int {
	moves := pos.GenerateLegalMoves(true)
	steps := len(moves)

	if pos.SideToMove == xionghan.Red {
		return steps * mobilityWeight
	}
	if pos.SideToMove == xionghan.Black {
		return -steps * mobilityWeight
	}
	return 0
}

// 与 xionghan 里的逻辑保持一致（那边是小写的 inPalace/pawnPassedWall，这里复制一份）。
func isInPalaceLocal(side xionghan.Side, row, col int) bool {
	midCol := xionghan.Cols / 2 // 6

	if col < midCol-1 || col > midCol+1 {
		return false
	}
	if side == xionghan.Black {
		return row >= 1 && row <= 3
	}
	if side == xionghan.Red {
		return row >= xionghan.Rows-4 && row <= xionghan.Rows-2 // 9..11
	}
	return false
}

// 与 rules 的 pawnPassedWall 一致：红向上，黑向下
func pawnPassedWallLocal(side xionghan.Side, row int) bool {
	if side == xionghan.Red {
		return row < xionghan.WallRow
	}
	if side == xionghan.Black {
		return row > xionghan.WallRow
	}
	return false
}

func oppositeSide(side xionghan.Side) xionghan.Side {
	if side == xionghan.Red {
		return xionghan.Black
	}
	if side == xionghan.Black {
		return xionghan.Red
	}
	return xionghan.NoSide
}

// 自己这边的“前进距离”：家附近≈0，越靠近敌营数值越大
func rankFromSide(side xionghan.Side, row int) int {
	if side == xionghan.Red {
		return xionghan.Rows - 1 - row
	}
	if side == xionghan.Black {
		return row
	}
	return 0
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
