package engine

import (
	"math"
	"math/rand"
	"sort"
	"sync/atomic"
	"time"

	"xionghan/internal/xionghan"
)

const (
	// 一个足够大的值，当成正负无穷
	scoreInf = 1_000_000_000

	vcfDepthFilter = 5
	vcfDepthRoot   = 6

	// If root top-2 scores are close enough, randomize between them for less deterministic play.
	rootTopTwoRandomGap = 60
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

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
	NNFailed bool            // 搜索期间 NN 推理是否失败
}

// 从红方视角的评价：正数红方好，负数黑方好
// 为了 alpha-beta，调用时根据 sideToMove 做“极大/极小”选择。
func Evaluate(pos *xionghan.Position) int {
	// Legacy handcrafted evaluation is intentionally disabled.
	// Keep this function as a placeholder to avoid accidental reuse.
	/*
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
	*/
	_ = pos
	return 0
}

// 搜索层调用这个
func (e *Engine) eval(pos *xionghan.Position) int {
	if e.nn == nil {
		// Handcrafted eval is disabled; NN is required.
		e.markNNFailure()
		return 0
	}
	key := hashPosition(pos)
	if cached, ok := e.getNNEvalFromCache(key); ok {
		return cached
	}
	res, err := e.nn.Evaluate(pos)
	if err == nil && res != nil {
		// 将胜率/分数转换为整数分。
		// NN 输出 winProb 是 P_WHITE (Black) 的胜率，lossProb 是 P_BLACK (Red) 的胜率。
		// 搜索视角是 Red 为正，所以 score = RedWinProb - BlackWinProb
		winLoss := res.LossProb - res.WinProb
		score := int(winLoss * 10000)
		e.storeNNEvalCache(key, score)
		return score
	}
	// 不回退手工评估：标记 NN 故障并中止本次搜索。
	e.markNNFailure()
	return 0
}

// FilterVCFMoves 过滤掉会导致被对方连将绝杀或直接吃王的走法。
func (e *Engine) FilterVCFMoves(pos *xionghan.Position, moves []xionghan.Move) []xionghan.Move {
	if pos.TotalPieces() > 43 || len(moves) <= 1 {
		return moves
	}

	var safeMoves []xionghan.Move
	for _, mv := range moves {
		nextPos, ok := pos.ApplyMove(mv)
		if !ok {
			continue
		}

		// 1. 检查对手是否能在下一手直接吃王（预防非将军的杀招）
		if e.CanCaptureKingNext(nextPos) {
			continue
		}

		// 2. 检查对手是否能进入 VCF 连将杀（预防必杀局）
		vcf := e.VCFSearch(nextPos, vcfDepthFilter)
		if vcf.CanWin {
			continue
		}

		safeMoves = append(safeMoves, mv)
	}

	// 如果所有走法都导致输棋，则不进行过滤，维持原样（等死）
	if len(safeMoves) == 0 {
		return moves
	}
	return safeMoves
}

// 根节点搜索：带简单迭代加深（根节点内部并行）
func (e *Engine) Search(pos *xionghan.Position, cfg SearchConfig) SearchResult {
	e.resetNNAbort()

	// 1. 绝杀判定：直接吃王
	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
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

	// 2. VCF 连将赢判定（抢杀）
	if pos.TotalPieces() <= 43 {
		vcfRes := e.VCFSearch(pos, vcfDepthRoot)
		if vcfRes.CanWin {
			return SearchResult{
				BestMove: vcfRes.Move,
				Score:    900000,
				WinProb:  1.0,
				Depth:    vcfDepthRoot,
				Nodes:    100,
				TimeUsed: 0,
				PV:       []xionghan.Move{vcfRes.Move},
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
		if e.hasNNFailure() {
			bestMove = xionghan.Move{}
			bestDepth = 0
			break
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			break
		}
		score, move := e.alphaBetaRoot(pos, depth, -scoreInf, scoreInf, deadline)
		if e.hasNNFailure() {
			bestMove = xionghan.Move{}
			bestDepth = 0
			break
		}
		if move.From == 0 && move.To == 0 {
			// 搜不到有效着法（可能是无子可动）
			break
		}
		bestMove = move
		bestScore = score
		bestDepth = depth
	}

	// Default: map search score (red-positive) to [0,1].
	winProb := (float32(bestScore)/10000.0 + 1.0) / 2.0
	if winProb < 0 {
		winProb = 0
	}
	if winProb > 1 {
		winProb = 1
	}
	// UI label is "Red Win %". Prefer root NN red-win probability (fixed color view)
	// to avoid shallow minimax max/min amplification that can look overly extreme.
	if e.UseNN && e.nn != nil && !e.hasNNFailure() {
		if root, err := e.nn.Evaluate(pos); err == nil {
			winProb = root.LossProb // fixed red win prob
		}
	}

	return SearchResult{
		BestMove: bestMove,
		Score:    bestScore,
		WinProb:  winProb,
		Depth:    bestDepth,
		Nodes:    atomic.LoadInt64(&e.nodes),
		TimeUsed: time.Since(start),
		PV:       []xionghan.Move{bestMove},
		NNFailed: e.hasNNFailure(),
	}
}

// 根节点：根据 SideToMove 决定是 max 还是 min，并行搜索每个着法
func (e *Engine) alphaBetaRoot(pos *xionghan.Position, depth int, alpha, beta int, deadline time.Time) (int, xionghan.Move) {
	if e.hasNNFailure() {
		return 0, xionghan.Move{}
	}

	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	moves = e.FilterBlunderMoves(pos, moves)
	moves = e.FilterVCFMoves(pos, moves)
	if len(moves) == 0 {
		// 没招就直接返回静态评估
		return e.eval(pos), xionghan.Move{}
	}

	// 1. 两阶段推理进行排序
	if e.UseNN && e.nn != nil {
		// Stage 0: 获取 From 概率
		res0, err := e.nn.EvaluateWithStage(pos, 0, -1)
		if err == nil && res0 != nil {
			// 为了效率，我们将 From 位置相同的招法分组，并对每组调用一次 Stage 1
			fromGroups := make(map[int][]int) // From -> indices in moves
			for i, mv := range moves {
				fromGroups[mv.From] = append(fromGroups[mv.From], i)
			}

			type moveScore struct {
				idx  int
				prob float32
			}
			scores := make([]moveScore, len(moves))

			for from, indices := range fromGroups {
				// Stage 1: 为该选定的棋子获取 To 概率
				res1, err := e.nn.EvaluateWithStage(pos, 1, from)
				if err != nil || res1 == nil {
					e.markNNFailure()
					return 0, xionghan.Move{}
				}
				fromProb := res0.Policy[from]
				for _, idx := range indices {
					toProb := res1.Policy[moves[idx].To]
					scores[idx] = moveScore{idx: idx, prob: fromProb * toProb}
				}
			}

			// IMPORTANT:
			// Do not sort `moves` directly by `scores[i]`, because indices in the
			// comparator are positions *during sorting*, while `scores` is indexed
			// by original move order. That would decouple move/prob pairing.
			sort.SliceStable(scores, func(i, j int) bool {
				return scores[i].prob > scores[j].prob
			})
			sortedMoves := make([]xionghan.Move, len(moves))
			for i, sc := range scores {
				sortedMoves[i] = moves[sc.idx]
			}
			copy(moves, sortedMoves)
		} else {
			e.markNNFailure()
			return 0, xionghan.Move{}
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
			tt:             make(map[uint64]ttEntry, 1<<14),
			blunderTT:      make(map[uint64]uint8, 1<<13),
			blunderReplyTT: make(map[uint64]uint8, 1<<13),
			nn:             e.nn,
			UseNN:          e.UseNN,
			nnAbort:        e.nnAbort,
			nnCache:        e.nnCache,
		}
		score := local.alphaBeta(children[0].child, depth-1, alpha, beta, deadline)
		if local.nodes != 0 {
			atomic.AddInt64(&e.nodes, local.nodes)
		}
		bestMove := children[0].move
		// 根节点也存一下 TT（全局 tt 仍然单线程访问）
		e.storeTT(key, depth, score, ttExact, bestMove)
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
				tt:             make(map[uint64]ttEntry, 1<<14),
				blunderTT:      make(map[uint64]uint8, 1<<13),
				blunderReplyTT: make(map[uint64]uint8, 1<<13),
				nn:             e.nn,
				UseNN:          e.UseNN,
				nnAbort:        e.nnAbort,
				nnCache:        e.nnCache,
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
	rootResults := make([]rootResult, 0, len(children))
	for i := 0; i < len(children); i++ {
		r := <-results
		rootResults = append(rootResults, r)
	}

	if len(rootResults) == 0 {
		return e.eval(pos), xionghan.Move{}
	}

	if side == xionghan.Red {
		sort.SliceStable(rootResults, func(i, j int) bool {
			return rootResults[i].score > rootResults[j].score
		})
	} else {
		sort.SliceStable(rootResults, func(i, j int) bool {
			return rootResults[i].score < rootResults[j].score
		})
	}

	best := rootResults[0]
	if len(rootResults) >= 2 {
		gap := rootResults[0].score - rootResults[1].score
		if gap < 0 {
			gap = -gap
		}
		if gap <= rootTopTwoRandomGap && rand.Intn(2) == 1 {
			best = rootResults[1]
		}
	}
	bestMove = best.move
	bestScore := best.score
	if e.hasNNFailure() {
		return 0, xionghan.Move{}
	}

	if bestMove.From == 0 && bestMove.To == 0 {
		// 理论上不会走到这里，兜底一下
		return e.eval(pos), xionghan.Move{}
	}

	// 根节点存 TT（全局 tt 依然只有主 goroutine 访问）
	e.storeTT(key, depth, bestScore, ttExact, bestMove)
	return bestScore, bestMove
}

// 内部递归：标准 alpha-beta（在并行版本里由每个局部 Engine 独享调用）
func (e *Engine) alphaBeta(pos *xionghan.Position, depth int, alpha, beta int, deadline time.Time) int {
	e.nodes++
	if e.hasNNFailure() {
		return 0
	}

	if depth <= 0 {
		return e.eval(pos)
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		// 超时：返回当前静态评估（不完美，但能保证退出）
		return e.eval(pos)
	}

	key := hashPosition(pos)
	origAlpha, origBeta := alpha, beta
	ttMove := xionghan.Move{}
	if entry, ok := e.tt[key]; ok {
		ttMove = entry.Move
		if entry.Depth >= depth {
			switch entry.Flag {
			case ttExact:
				return entry.Score
			case ttUpperBound:
				if entry.Score <= alpha {
					return entry.Score
				}
				if entry.Score < beta {
					beta = entry.Score
				}
			case ttLowerBound:
				if entry.Score >= beta {
					return entry.Score
				}
				if entry.Score > alpha {
					alpha = entry.Score
				}
			}
			if alpha >= beta {
				return entry.Score
			}
		}
	}

	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	moves = e.FilterBlunderMoves(pos, moves)
	moves = e.FilterVCFMoves(pos, moves)
	if len(moves) == 0 {
		// 没招，简单直接评估（以后可以做将死检测）
		return e.eval(pos)
	}

	side := pos.SideToMove
	orderMovesByCaptureFirst(pos, moves)
	if ttMove.From != 0 || ttMove.To != 0 {
		for i := range moves {
			if moves[i].From == ttMove.From && moves[i].To == ttMove.To {
				moves[0], moves[i] = moves[i], moves[0]
				break
			}
		}
	}

	var bestScore int
	bestMove := xionghan.Move{}
	if side == xionghan.Red {
		bestScore = math.MinInt
		for i := range moves {
			child, ok := pos.ApplyMove(moves[i])
			if !ok {
				continue
			}
			score := e.alphaBeta(child, depth-1, alpha, beta, deadline)
			if e.hasNNFailure() {
				return 0
			}
			if score > bestScore {
				bestScore = score
				bestMove = moves[i]
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
			if e.hasNNFailure() {
				return 0
			}
			if score < bestScore {
				bestScore = score
				bestMove = moves[i]
			}
			if score < beta {
				beta = score
			}
			if alpha >= beta {
				break
			}
		}
	}

	if bestMove.From == 0 && bestMove.To == 0 {
		return e.eval(pos)
	}

	flag := ttExact
	if bestScore <= origAlpha {
		flag = ttUpperBound
	} else if bestScore >= origBeta {
		flag = ttLowerBound
	}

	// 存入 TT（这里写的是局部 Engine 的 tt，不会有并发冲突）
	e.storeTT(key, depth, bestScore, flag, bestMove)
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
