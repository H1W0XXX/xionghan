package engine

import (
	"math"
	"sort"
	"sync/atomic"
	"time"

	"xionghan/internal/xionghan"
)

const (
	// 一个足够大的值，当成正负无穷
	scoreInf = 1_000_000_000
)

// 搜索配置
type SearchConfig struct {
	MaxDepth  int           // 最大搜索深度（ply）
	TimeLimit time.Duration // 搜索时间上限（0 表示不限制）
}

// 搜索结果
type SearchResult struct {
	BestMove xionghan.Move   // 最佳着法（当前位置）
	Score    int             // 评估分（正：红方好，负：黑方好）
	WinProb  float32         // 红方胜率
	Depth    int             // 实际搜索到的深度
	Nodes    int64           // 节点数
	TimeUsed time.Duration   // 花费时间
	PV       []xionghan.Move // 主变（这里只放了根节点的最佳着法，其它你以后可以扩展）
}

// 从红方视角的评价：正数红方好，负数黑方好
// 为了 alpha-beta，调用时根据 sideToMove 做“极大/极小”选择。
func Evaluate(pos *xionghan.Position) int {
	materialPos := evaluateMaterialPositional(pos)
	kingSafety := evaluateKingSafety(pos)
	mobility := evaluateMobility(pos)

	tempo := 0
	if pos.SideToMove == xionghan.Red {
		tempo = tempoBonus
	} else if pos.SideToMove == xionghan.Black {
		tempo = -tempoBonus
	}

	return materialPos + kingSafety + mobility + tempo
}

// 搜索层调用这个
func (e *Engine) eval(pos *xionghan.Position) int {
	if e.UseNN && e.nn != nil {
		res, err := e.nn.Evaluate(pos)
		if err == nil {
			// 将胜率/分数转换为整数分。
			// NN 输出 winProb 是 P_WHITE (Black) 的胜率，lossProb 是 P_BLACK (Red) 的胜率。
			// 搜索视角是 Red 为正，所以 score = RedWinProb - BlackWinProb
			winLoss := res.LossProb - res.WinProb
			return int(winLoss * 10000)
		}
	}
	return Evaluate(pos)
}

// 根节点搜索：带简单迭代加深（根节点内部并行）
func (e *Engine) Search(pos *xionghan.Position, cfg SearchConfig) SearchResult {
	// 绝杀剪枝：如果当前能直接吃到对方的王，直接返回该走法，不再进行任何搜索
	moves := pos.GenerateLegalMoves(true)
	for _, mv := range moves {
		targetPiece := pos.Board.Squares[mv.To]
		if targetPiece != 0 && targetPiece.Type() == xionghan.PieceKing {
			return SearchResult{
				BestMove: mv,
				Score:    scoreInf,
				WinProb:  1.0,
				Depth:    1,
				Nodes:    1,
				TimeUsed: 0,
				PV:       []xionghan.Move{mv},
			}
		}
	}

	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 3
	}
	start := time.Now()
	atomic.StoreInt64(&e.nodes, 0)

	bestMove := xionghan.Move{}
	bestScore := 0
	bestDepth := 0

	deadline := time.Time{}
	if cfg.TimeLimit > 0 {
		deadline = start.Add(cfg.TimeLimit)
	}

	for depth := 1; depth <= cfg.MaxDepth; depth++ {
		if !deadline.IsZero() && time.Now().After(deadline) {
			break
		}
		score, move := e.alphaBetaRoot(pos, depth, -scoreInf, scoreInf, deadline)
		if move.From == 0 && move.To == 0 {
			// 搜不到有效着法（可能是无子可动）
			break
		}
		bestMove = move
		bestScore = score
		bestDepth = depth
	}

	winProb := (float32(bestScore)/10000.0 + 1.0) / 2.0
	if winProb < 0 {
		winProb = 0
	}
	if winProb > 1 {
		winProb = 1
	}

	return SearchResult{
		BestMove: bestMove,
		Score:    bestScore,
		WinProb:  winProb,
		Depth:    bestDepth,
		Nodes:    atomic.LoadInt64(&e.nodes),
		TimeUsed: time.Since(start),
		PV:       []xionghan.Move{bestMove},
	}
}

// 根节点：根据 SideToMove 决定是 max 还是 min，并行搜索每个着法
func (e *Engine) alphaBetaRoot(pos *xionghan.Position, depth int, alpha, beta int, deadline time.Time) (int, xionghan.Move) {
	moves := pos.GenerateLegalMoves(true)
	if len(moves) == 0 {
		// 没招就直接返回静态评估
		return e.eval(pos), xionghan.Move{}
	}

	// 1. 两阶段推理进行排序
	if e.UseNN && e.nn != nil {
		// Stage 0: 获取 From 概率
		res0, err := e.nn.EvaluateWithStage(pos, 0, -1)
		if err == nil {
			// 为了效率，我们将 From 位置相同的招法分组，并对每组调用一次 Stage 1
			fromGroups := make(map[int][]int) // From -> indices in moves
			for i, mv := range moves {
				fromGroups[mv.From] = append(fromGroups[mv.From], i)
			}

			type moveScore struct {
				idx   int
				prob  float32
			}
			scores := make([]moveScore, len(moves))

			for from, indices := range fromGroups {
				// Stage 1: 为该选定的棋子获取 To 概率
				res1, err := e.nn.EvaluateWithStage(pos, 1, from)
				fromProb := res0.Policy[from]
				if err == nil {
					for _, idx := range indices {
						toProb := res1.Policy[moves[idx].To]
						scores[idx] = moveScore{idx: idx, prob: fromProb * toProb}
					}
				} else {
					for _, idx := range indices {
						scores[idx] = moveScore{idx: idx, prob: fromProb * 0.001}
					}
				}
			}

			sort.SliceStable(moves, func(i, j int) bool {
				return scores[i].prob > scores[j].prob
			})
		}
	} else {
		// 没有 NN 时，把吃子招提前一点
		orderMovesByCaptureFirst(pos, moves)
	}

	// 根节点用全局 TT 排序是安全的：这里还是单线程
	side := pos.SideToMove
	key := hashPosition(pos)
	if entry, ok := e.tt[key]; ok {
		// 把 entry.Move 提到前面
		for i := range moves {
			if moves[i].From == entry.Move.From && moves[i].To == entry.Move.To {
				moves[0], moves[i] = moves[i], moves[0]
				break
			}
		}
	}

	// 先同步生成所有子局面，避免并发操作 pos
	type childNode struct {
		move  xionghan.Move
		child *xionghan.Position
	}
	children := make([]childNode, 0, len(moves))
	for _, mv := range moves {
		child, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}
		children = append(children, childNode{
			move:  mv,
			child: child,
		})
	}

	if len(children) == 0 {
		return e.eval(pos), xionghan.Move{}
	}
	// 只有一个着法时没必要并行，直接用局部 Engine 跑一次
	if len(children) == 1 {
		local := &Engine{
			tt:    make(map[uint64]ttEntry, 1<<14),
			nn:    e.nn,
			UseNN: e.UseNN,
		}
		score := local.alphaBeta(children[0].child, depth-1, alpha, beta, deadline)
		if local.nodes != 0 {
			atomic.AddInt64(&e.nodes, local.nodes)
		}
		bestMove := children[0].move
		// 根节点也存一下 TT（全局 tt 仍然单线程访问）
		e.storeTT(key, depth, score, bestMove)
		return score, bestMove
	}

	// 有多个着法：并行
	type rootResult struct {
		move  xionghan.Move
		score int
	}

	results := make(chan rootResult, len(children))

	for _, ch := range children {
		ch := ch
		go func() {
			// 每个 goroutine 用自己的 Engine/TT，避免加锁和 map 竞争
			local := &Engine{
				tt:    make(map[uint64]ttEntry, 1<<14),
				nn:    e.nn,
				UseNN: e.UseNN,
			}
			score := local.alphaBeta(ch.child, depth-1, alpha, beta, deadline)
			if local.nodes != 0 {
				atomic.AddInt64(&e.nodes, local.nodes)
			}
			results <- rootResult{
				move:  ch.move,
				score: score,
			}
		}()
	}

	bestMove := xionghan.Move{}
	var bestScore int
	if side == xionghan.Red {
		bestScore = math.MinInt
	} else {
		bestScore = math.MaxInt
	}

	for i := 0; i < len(children); i++ {
		r := <-results

		// 第一个有效结果直接作为初始 best
		if bestMove.From == 0 && bestMove.To == 0 {
			bestMove = r.move
			bestScore = r.score
			continue
		}

		if side == xionghan.Red {
			// 极大层
			if r.score > bestScore {
				bestScore = r.score
				bestMove = r.move
			}
		} else {
			// 极小层
			if r.score < bestScore {
				bestScore = r.score
				bestMove = r.move
			}
		}
	}

	if bestMove.From == 0 && bestMove.To == 0 {
		// 理论上不会走到这里，兜底一下
		return e.eval(pos), xionghan.Move{}
	}

	// 根节点存 TT（全局 tt 依然只有主 goroutine 访问）
	e.storeTT(key, depth, bestScore, bestMove)
	return bestScore, bestMove
}

// 内部递归：标准 alpha-beta（在并行版本里由每个局部 Engine 独享调用）
func (e *Engine) alphaBeta(pos *xionghan.Position, depth int, alpha, beta int, deadline time.Time) int {
	e.nodes++

	if depth <= 0 {
		return e.eval(pos)
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		// 超时：返回当前静态评估（不完美，但能保证退出）
		return e.eval(pos)
	}

	key := hashPosition(pos)
	if entry, ok := e.tt[key]; ok && entry.Depth >= depth {
		return entry.Score
	}

	moves := pos.GenerateLegalMoves(true)
	if len(moves) == 0 {
		// 没招，简单直接评估（以后可以做将死检测）
		return e.eval(pos)
	}

	side := pos.SideToMove
	orderMovesByCaptureFirst(pos, moves)

	var bestScore int
	if side == xionghan.Red {
		bestScore = math.MinInt
		for i := range moves {
			child, ok := pos.ApplyMove(moves[i])
			if !ok {
				continue
			}
			score := e.alphaBeta(child, depth-1, alpha, beta, deadline)
			if score > bestScore {
				bestScore = score
			}
			if score > alpha {
				alpha = score
			}
			if alpha >= beta {
				break
			}
		}
	} else {
		bestScore = math.MaxInt
		for i := range moves {
			child, ok := pos.ApplyMove(moves[i])
			if !ok {
				continue
			}
			score := e.alphaBeta(child, depth-1, alpha, beta, deadline)
			if score < bestScore {
				bestScore = score
			}
			if score < beta {
				beta = score
			}
			if alpha >= beta {
				break
			}
		}
	}

	// 存入 TT（这里写的是局部 Engine 的 tt，不会有并发冲突）
	e.storeTT(key, depth, bestScore, xionghan.Move{})
	return bestScore
}

// 一个非常粗暴的“吃子优先”排序
func orderMovesByCaptureFirst(pos *xionghan.Position, moves []xionghan.Move) {
	board := pos.Board
	swap := func(i, j int) { moves[i], moves[j] = moves[j], moves[i] }
	n := len(moves)
	for i := 0; i < n; i++ {
		if board.Squares[moves[i].To] == 0 {
			// 从后往前找一个“吃子招”
			for j := n - 1; j > i; j-- {
				if board.Squares[moves[j].To] != 0 {
					swap(i, j)
					break
				}
			}
		}
	}
}
