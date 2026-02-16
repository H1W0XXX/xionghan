package xionghan

// 伪合法（不考虑自己王被将军），先够你测试走法 & 前端交互
func (p *Position) GeneratePseudoMoves() []Move {
	side := p.SideToMove
	var moves []Move
	for sq := 0; sq < NumSquares; sq++ {
		pc := p.Board.Squares[sq]
		if pc == 0 || pc.Side() != side {
			continue
		}
		switch pc.Type() {
		case PieceRook:
			genRookMoves(p, sq, &moves)
		case PieceCannon:
			genCannonMoves(p, sq, &moves)
		case PieceKnight:
			genKnightMoves(p, sq, &moves)
		case PieceElephant:
			genElephantMoves(p, sq, &moves)
		case PieceAdvisor:
			genAdvisorMoves(p, sq, &moves)
		case PieceKing:
			genKingMoves(p, sq, &moves)
		case PiecePawn:
			genPawnMoves(p, sq, &moves)
		case PieceLei:
			genLeiMoves(p, sq, &moves)
		case PieceFeng:
			genFengMoves(p, sq, &moves)
		case PieceWei:
			genWeiMoves(p, sq, &moves)
		}
	}
	return moves
}

// GenerateLegalMoves 生成合法走法
// isAI 为 true 时，会应用一些启发式过滤（如开局不动王、禁止送将）以优化搜索。
// isAI 为 false 时（PVP），只保留最基本的规则校验（如王对脸）。
func (p *Position) GenerateLegalMoves(isAI bool) []Move {
	pseudo := p.GeneratePseudoMoves()
	out := make([]Move, 0, len(pseudo))
	side := p.SideToMove

	// AI 搜索时的启发式统计
	var totalPieces int
	var myPieceCount int
	var currentlyInCheck bool
	if isAI {
		for _, pc := range p.Board.Squares {
			if pc != 0 {
				totalPieces++
				if pc.Side() == side {
					myPieceCount++
				}
			}
		}
		currentlyInCheck = p.IsInCheck(side)
	}

	for _, mv := range pseudo {
		// 0. 绝杀判定：如果这一步直接吃掉对方的王，那绝对合法且必须走（游戏结束）
		// 不需要管什么王对脸、自杀、送子，因为对手已经没了，游戏直接胜利
		target := p.Board.Squares[mv.To]
		if target != 0 && target.Type() == PieceKing {
			out = append(out, mv)
			continue
		}

		np, ok := p.ApplyMove(mv)
		if !ok {
			continue
		}

		// ① 不能王对脸（绝对非法，任何时候都拦截）
		if np.kingsFace() {
			continue
		}

		if isAI {
			// ② 开局限制：禁止 AI 在早期乱动王和士
			if totalPieces >= 44 && !currentlyInCheck {
				pt := p.Board.Squares[mv.From].Type()
				if pt == PieceKing || pt == PieceAdvisor {
					continue
				}
			}

			// ③ 送王拦截：AI 搜索时禁止主动送将（除非只剩下王）
			if myPieceCount > 1 {
				if np.IsInCheck(side) {
					continue
				}
			}

			// ④ 避兔弱智送子：大子换小兵拦截
			if totalPieces > 30 {
				movingPiece := p.Board.Squares[mv.From]
				mpt := movingPiece.Type()
				// 如果移动的是 车、炮、马、檑
				if mpt == PieceRook || mpt == PieceCannon || mpt == PieceKnight || mpt == PieceLei {
					// 且移动后被对方的小兵盯着
					if np.IsAttackedByPawn(mv.To, opposite(side)) {
						// 除非这步棋本身能吃到对方同等或更高价值的子（先简化处理：如果是吃子步，且目标不是兵/卫/锋，则允许）
						targetPiece := p.Board.Squares[mv.To]
						if targetPiece == 0 {
							// 纯送大子给兵吃，过滤掉
							continue
						}
						tpt := targetPiece.Type()
						if tpt == PiecePawn || tpt == PieceWei || tpt == PieceFeng {
							// 用大子换对方的小卒/卫/锋，也不划算，过滤掉
							continue
						}
					}
				}
			}
		}

		out = append(out, mv)
	}
	return out
}

// 应用走子：这里默认传进来的就是合法招（由上层检查）
func (p *Position) ApplyMove(m Move) (*Position, bool) {
	if m.From < 0 || m.From >= NumSquares || m.To < 0 || m.To >= NumSquares {
		return nil, false
	}
	pc := p.Board.Squares[m.From]
	if pc == 0 || pc.Side() != p.SideToMove {
		return nil, false
	}
	np := *p
	np.Board.Squares[m.To] = pc
	np.Board.Squares[m.From] = 0
	np.SideToMove = opposite(p.SideToMove)
	return &np, true
}
