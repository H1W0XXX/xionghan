package mcts

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"xionghan/internal/engine"
	"xionghan/internal/xionghan"
)

// MCTSConfig MCTS 搜索配置
type MCTSConfig struct {
	Simulations    int           // 仿真次数（Playouts）
	TimeLimit      time.Duration // 时间限制
	Cpuct          float32       // 探索常数（通常 1.0 - 5.0）
	Temperature    float32       // 根节点选择着法的温度
	DirichletNoise float32       // 根节点添加的 Dirichlet 噪声（用于多样性）
}

// MCTSNode MCTS 搜索节点
type MCTSNode struct {
	mu sync.Mutex

	move     xionghan.Move
	parent   *MCTSNode
	children map[xionghan.Move]*MCTSNode

	visitCount int
	totalValue float32 // 累计胜率值（对 SideToMove 的胜率贡献，通常 0-1）
	prior      float32 // 神经网络给出的先验概率

	isExpanded bool
	isTerminal bool
	winner     int // 0: unknown, 1: draw, 2: next win, 3: opp win
}

// SearchResult MCTS 搜索结果
type SearchResult struct {
	BestMove xionghan.Move
	WinProb  float32
	Sims     int
	TimeUsed time.Duration
	PV       []xionghan.Move
}

func NewNode(mv xionghan.Move, parent *MCTSNode, prior float32) *MCTSNode {
	return &MCTSNode{
		move:     mv,
		parent:   parent,
		children: make(map[xionghan.Move]*MCTSNode),
		prior:    prior,
	}
}

// QValue 节点的平均胜率值
func (n *MCTSNode) QValue() float32 {
	if n.visitCount == 0 {
		return 0.5 // 未访问节点的默认胜率
	}
	return n.totalValue / float32(n.visitCount)
}

// MCTSSearcher MCTS 搜索执行器
type MCTSSearcher struct {
	nn  *engine.NNEvaluator
	cfg MCTSConfig
}

func NewSearcher(nn *engine.NNEvaluator, cfg MCTSConfig) *MCTSSearcher {
	if cfg.Cpuct <= 0 {
		cfg.Cpuct = 1.25 // 默认探索常数
	}
	if cfg.Temperature <= 0 {
		cfg.Temperature = 1.0
	}
	return &MCTSSearcher{
		nn:  nn,
		cfg: cfg,
	}
}

// Search 执行 MCTS 搜索
func (s *MCTSSearcher) Search(pos *xionghan.Position) SearchResult {
	start := time.Now()
	root := NewNode(xionghan.Move{}, nil, 1.0)

	// 1. 根节点展开
	s.expandNode(root, pos)

	// 2. 仿真循环
	for i := 0; i < s.cfg.Simulations; i++ {
		// 如果有时间限制且已超时，则退出循环
		if s.cfg.TimeLimit > 0 && time.Since(start) > s.cfg.TimeLimit {
			break
		}

		s.simulate(root, pos)
	}

	// 3. 选择最佳着法（通常根据访问次数选择）
	bestMove := s.selectBestMove(root)
	
	// 计算胜率（红方胜率）
	winProb := root.QValue()
	if pos.SideToMove == xionghan.Black {
		winProb = 1.0 - winProb
	}

	return SearchResult{
		BestMove: bestMove,
		WinProb:  winProb,
		Sims:     root.visitCount,
		TimeUsed: time.Since(start),
		PV:       []xionghan.Move{bestMove},
	}
}

// simulate 执行单次仿真：Selection -> Expansion -> Evaluation -> Backup
func (s *MCTSSearcher) simulate(root *MCTSNode, pos *xionghan.Position) {
	node := root
	currPos := pos

	// Selection: 选择子节点直到到达叶子或未展开节点
	var path []*MCTSNode
	path = append(path, node)

	for node.isExpanded && !node.isTerminal {
		node.mu.Lock()
		bestChildMv, bestChild := s.selectChildPUCT(node)
		node.mu.Unlock()

		node = bestChild
		path = append(path, node)
		
		// 应用走法更新局面
		nextPos, ok := currPos.ApplyMove(bestChildMv)
		if !ok {
			// 理论上不会发生，因为子节点是根据合法着法生成的
			break
		}
		currPos = nextPos
	}

	// Expansion & Evaluation: 展开并利用神经网络评估
	var value float32
	if node.isTerminal {
		// 终端节点根据胜负定值
		value = s.getTerminalValue(node)
	} else {
		// 调用神经网络进行评估
		nnRes, err := s.nn.Evaluate(currPos)
		if err != nil {
			// 兜底：如果神经网络失败，使用中性值
			value = 0.5
		} else {
			s.expandWithNN(node, currPos, nnRes)
			// NN 输出的是当前走子方的胜率
			value = nnRes.LossProb // 简化：假设 LossProb 是当前局面对于当前走子方的胜率（需匹配具体模型输出含义）
			// 注意：这里需要根据具体模型输出调整胜率归一化方向
			if currPos.SideToMove == xionghan.Red {
				value = nnRes.LossProb // Red 胜率
			} else {
				value = nnRes.WinProb // Black 胜率（即 1 - Red胜率）
			}
		}
	}

	// Backpropagation: 反向更新各层级节点
	s.backpropagate(path, value, pos.SideToMove)
}

func (s *MCTSSearcher) selectChildPUCT(node *MCTSNode) (xionghan.Move, *MCTSNode) {
	var bestMove xionghan.Move
	var bestChild *MCTSNode
	maxPUCT := float32(-1.0e10)

	totalVisitSqrt := float32(math.Sqrt(float64(node.visitCount)))

	for mv, child := range node.children {
		// PUCT = Q + C_puct * P * sqrt(sum_N) / (1 + N)
		q := child.QValue()
		u := s.cfg.Cpuct * child.prior * totalVisitSqrt / (1.0 + float32(child.visitCount))
		puct := q + u

		if puct > maxPUCT {
			maxPUCT = puct
			bestMove = mv
			bestChild = child
		}
	}
	return bestMove, bestChild
}

func (s *MCTSSearcher) expandNode(node *MCTSNode, pos *xionghan.Position) {
	nnRes, err := s.nn.Evaluate(pos)
	if err != nil {
		return
	}
	s.expandWithNN(node, pos, nnRes)
}

func (s *MCTSSearcher) expandWithNN(node *MCTSNode, pos *xionghan.Position, res *engine.NNResult) {
	node.mu.Lock()
	defer node.mu.Unlock()

	if node.isExpanded {
		return
	}

	moves := pos.GenerateLegalMoves(true)
	if len(moves) == 0 {
		node.isTerminal = true
		// 检查胜负（此处需补充棋局终局判断）
		return
	}

	for _, mv := range moves {
		// 从神经网络结果中获取该走法的 Prior
		// 注意：此处需要将 Move 映射到 NN 的 Policy 索引，暂假设 Policy 按坐标排列
		// 简化处理：根据从/到坐标提取概率（需适配 engine.go 中的映射逻辑）
		p := res.Policy[mv.From] * 0.5 + res.Policy[mv.To] * 0.5 // 临时占位，实际应根据 NN 结构严谨映射
		node.children[mv] = NewNode(mv, node, p)
	}

	node.isExpanded = true
}

func (s *MCTSSearcher) backpropagate(path []*MCTSNode, value float32, rootSide int) {
	// 反向遍历路径并更新
	// value 通常是红方胜率
	for _, node := range path {
		node.mu.Lock()
		node.visitCount++
		node.totalValue += value
		node.mu.Unlock()
	}
}

func (s *MCTSSearcher) selectBestMove(root *MCTSNode) xionghan.Move {
	var bestMove xionghan.Move
	maxVisits := -1

	for mv, child := range root.children {
		if child.visitCount > maxVisits {
			maxVisits = child.visitCount
			bestMove = mv
		}
	}
	return bestMove
}

func (s *MCTSSearcher) getTerminalValue(node *MCTSNode) float32 {
	// 简单实现，后续应根据胜负手判定
	return 0.5
}
