package mcts

import (
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"xionghan/internal/engine"
	"xionghan/internal/xionghan"
)

type Searcher struct {
	nn     *engine.NNEvaluator
	params SearchParams
}

func NewSearcher(nn *engine.NNEvaluator, params SearchParams) *Searcher {
	return &Searcher{
		nn:     nn,
		params: params,
	}
}

func (s *Searcher) Search(pos *xionghan.Position) *engine.SearchResult {
	start := time.Now()
	root := NewNode(xionghan.Move{}, nil, pos.SideToMove)

	// 并行执行仿真
	var wg sync.WaitGroup
	simsPerThread := s.params.Simulations / s.params.NumThreads
	if simsPerThread < 1 {
		simsPerThread = 1
	}

	for t := 0; t < s.params.NumThreads; t++ {
		wg.Add(1)
		go func(threadIdx int) {
			defer wg.Done()
			for i := 0; i < simsPerThread; i++ {
				// 超时退出
				if s.params.MaxTime > 0 && time.Since(start) > s.params.MaxTime {
					break
				}
				s.playout(root, pos)
			}
		}(t)
	}
	wg.Wait()

	// 选择访问量最大的走法
	bestMove := xionghan.Move{}
	maxVisits := int64(-1)
	for mv, child := range root.Children {
		if child.Stats.Visits > maxVisits {
			maxVisits = child.Stats.Visits
			bestMove = mv
		}
	}

	// 统一输出胜率（针对红方）
	redWinProb := (root.Stats.UtilityAvg + 1.0) / 2.0
	score := int((redWinProb*2.0 - 1.0) * 10000)

	return &engine.SearchResult{
		BestMove: bestMove,
		Score:    score,
		WinProb:  float32(redWinProb),
		Nodes:    root.Stats.Visits,
		TimeUsed: time.Since(start),
		PV:       []xionghan.Move{bestMove},
	}
}

func (s *Searcher) playout(root *MCTSNode, pos *xionghan.Position) {
	node := root
	currPos := pos
	var path []*MCTSNode
	path = append(path, node)

	// Selection
	for {
		// 如果节点尚未展开或已是终端节点，则停止下行
		if atomic.LoadInt32(&node.State) != StateExpanded || node.IsTerminal {
			break
		}

		mv, nextNode := s.selectChildPUCT(node)
		if nextNode == nil {
			break
		}

		// Virtual Loss
		atomic.AddInt32(&nextNode.VirtualLosses, 1)
		
		node = nextNode
		path = append(path, node)

		nextPos, ok := currPos.ApplyMove(mv)
		if !ok {
			break
		}
		currPos = nextPos
	}

	// Expansion & Evaluation
	var utility float64 // Red perspective: 1.0 Red Win, -1.0 Black Win
	if node.IsTerminal {
		utility = s.getTerminalUtility(node)
	} else {
		// 神经网络评估（多线程并发调用 NN，engine 内部有批处理优化）
		res, err := s.nn.Evaluate(currPos)
		if err != nil {
			utility = 0.0 // 兜底：和棋
		} else {
			s.expandNode(node, currPos, res)
			// res.LossProb 是红方胜率 (0-1)，转换为 (-1, 1)
			utility = float64(res.LossProb*2.0 - 1.0)
		}
	}

	// Backpropagation
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		n.RecordPlayout(utility, 1.0)
		// 释放 Virtual Loss (注意：这里需跳过 root 或对应处理)
		if i > 0 {
			atomic.AddInt32(&n.VirtualLosses, -1)
		}
	}
}

func (s *Searcher) selectChildPUCT(node *MCTSNode) (xionghan.Move, *MCTSNode) {
	node.mu.Lock()
	defer node.mu.Unlock()

	var bestMove xionghan.Move
	var bestChild *MCTSNode
	maxSelectionValue := -1e20

	totalWeight := float64(node.Stats.WeightSum)
	cpuct := s.params.GetCpuct(totalWeight)
	
	// FPU (First Play Urgency)
	fpuValue := s.getFPU(node)

	for mv, child := range node.Children {
		childWeight := float64(atomic.LoadInt64(&child.Stats.Visits))
		// Virtual Loss 惩罚：模拟正在被探索的节点价值降低
		vLoss := float64(atomic.LoadInt32(&child.VirtualLosses))
		childWeight += vLoss
		
		var childUtility float64
		if childWeight > 0 {
			childUtility = child.GetUtilityForSelection(node.NextPla)
			// 如果有虚拟损失，utility 需要被拉低（从当前走子方的视角）
			if vLoss > 0 {
				vLossFactor := vLoss / (vLoss + childWeight)
				childUtility = childUtility * (1 - vLossFactor) + (-1.0 * vLossFactor) // 简化：-1 代表最差价值
			}
		} else {
			childUtility = fpuValue
		}

		prior := float64(node.PriorMap[mv])
		exploreValue := cpuct * prior * math.Sqrt(totalWeight+1.0) / (1.0 + childWeight)
		
		selectionValue := childUtility + exploreValue
		if selectionValue > maxSelectionValue {
			maxSelectionValue = selectionValue
			bestMove = mv
			bestChild = child
		}
	}

	return bestMove, bestChild
}

func (s *Searcher) getFPU(node *MCTSNode) float64 {
	// KataGo 风格 FPU：根据父节点评估值减去一定比例
	// 简化：使用父节点相对于当前方的 utility 减去 FPU 常数
	parentUtility := node.GetUtilityForSelection(node.NextPla)
	return parentUtility - s.params.FpuReductionMax
}

func (s *Searcher) expandNode(node *MCTSNode, pos *xionghan.Position, res *engine.NNResult) {
	// 原子状态转换：Unevaluated -> Evaluating -> Expanded
	if !atomic.CompareAndSwapInt32(&node.State, StateUnevaluated, StateEvaluating) {
		return
	}

	moves := pos.GenerateLegalMoves(true)
	if len(moves) == 0 {
		node.IsTerminal = true
		node.mu.Lock()
		node.State = StateExpanded
		node.mu.Unlock()
		return
	}

	node.mu.Lock()
	node.PriorMap = make(map[xionghan.Move]float32)
	totalPolicy := float32(0)
	for _, mv := range moves {
		// 这里暂且使用 Stage 0 的简单映射，之后可完善 Stage 1
		p := res.Policy[mv.From]
		node.PriorMap[mv] = p
		totalPolicy += p
	}
	
	// 归一化 Prior
	if totalPolicy > 0 {
		for mv := range node.PriorMap {
			node.PriorMap[mv] /= totalPolicy
		}
	} else {
		uniform := 1.0 / float32(len(moves))
		for _, mv := range moves {
			node.PriorMap[mv] = uniform
		}
	}

	// 创建子节点
	nextPla := xionghan.Black
	if node.NextPla == xionghan.Black {
		nextPla = xionghan.Red
	}

	for _, mv := range moves {
		node.Children[mv] = NewNode(mv, node, nextPla)
	}

	node.State = StateExpanded
	node.mu.Unlock()
}

func (s *Searcher) getTerminalUtility(node *MCTSNode) float64 {
	// 1.0 Red win, -1.0 Black win
	if node.Winner == 2 {
		return 1.0
	} else if node.Winner == 3 {
		return -1.0
	}
	return 0.0 // Draw or unknown
}
