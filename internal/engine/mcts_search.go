package engine

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"xionghan/internal/xionghan"
)

// MCTSNode MCTS 搜索节点
type MCTSNode struct {
	mu sync.Mutex

	Move     xionghan.Move
	NextPla  xionghan.Side
	Parent   *MCTSNode
	Children map[xionghan.Move]*MCTSNode
	State    int32 

	PriorMap map[xionghan.Move]float32
	NNValue  float64 
	
	Visits        int64   
	WeightSum     float64 
	UtilityAvg    float64 
	UtilitySqAvg  float64 
	VirtualLosses int32   

	IsTerminal bool
	Hash       uint64 
}

func NewMCTSNode(mv xionghan.Move, pla xionghan.Side, hash uint64) *MCTSNode {
	return &MCTSNode{
		Move:     mv,
		NextPla:  pla,
		Hash:     hash,
		Children: make(map[xionghan.Move]*MCTSNode),
		State:    StateUnevaluated,
	}
}

const (
	mctsCpuctExploration     = 1.1
	mctsCpuctExplorationBase = 10000.0
	mctsCpuctExplorationLog  = 0.45
	mctsFpuReductionMax      = 0.2
	mctsContempt             = 0.03 // 稍稍加强进攻偏好

	StateUnevaluated = iota
	StateEvaluating
	StateExpanded
)

func (e *Engine) runMCTS(pos *xionghan.Position, cfg SearchConfig) SearchResult {
	start := time.Now()
	h := pos.EnsureHash()

	e.poolMu.Lock()
	if e.mctsPool == nil {
		e.mctsPool = make(map[uint64]*MCTSNode, 1<<16)
	}
	root, ok := e.mctsPool[h]
	if !ok {
		root = NewMCTSNode(xionghan.Move{}, pos.SideToMove, h)
		e.mctsPool[h] = root
	}
	e.mctsRoot = root
	e.poolMu.Unlock()

	if atomic.LoadInt32(&root.State) == StateUnevaluated {
		res, err := e.nn.Evaluate(pos)
		if err != nil {
			e.markNNFailure()
			return SearchResult{}
		}
		e.expandMCTSNode(root, pos, res)
	}

	repBase := newRepetitionState(cfg)

	numThreads := 16
	simsPerThread := cfg.MCTSSimulations / numThreads
	if simsPerThread < 1 { simsPerThread = 1 }

	var wg sync.WaitGroup
	for t := 0; t < numThreads; t++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < simsPerThread; i++ {
				if cfg.TimeLimit > 0 && time.Since(start) > cfg.TimeLimit {
					break
				}
				e.mctsPlayout(root, pos, cfg, repBase.clone())
			}
		}()
	}
	wg.Wait()

	bestMove := xionghan.Move{}
	maxVisits := int64(-1)
	for mv, child := range root.Children {
		v := atomic.LoadInt64(&child.Visits)
		if v > maxVisits {
			maxVisits = v
			bestMove = mv
		}
	}

	// 内存池控制
	if len(e.mctsPool) > 300000 {
		e.poolMu.Lock()
		e.mctsPool = make(map[uint64]*MCTSNode, 1<<16)
		e.mctsPool[h] = root
		e.poolMu.Unlock()
	}

	redWinProb := (root.UtilityAvg + 1.0) / 2.0
	return SearchResult{
		BestMove: bestMove,
		Score:    int((redWinProb*2.0 - 1.0) * 10000),
		WinProb:  float32(redWinProb),
		Nodes:    atomic.LoadInt64(&root.Visits),
		TimeUsed: time.Since(start),
		PV:       []xionghan.Move{bestMove},
	}
}

func (e *Engine) mctsPlayout(root *MCTSNode, pos *xionghan.Position, cfg SearchConfig, rep *repetitionState) {
	node := root
	currPos := pos
	var path []*MCTSNode
	path = append(path, node)

	for {
		if atomic.LoadInt32(&node.State) != StateExpanded || node.IsTerminal {
			break
		}

		mv, nextNode := e.selectMCTSChild(node, rep)
		if nextNode == nil {
			break
		}

		atomic.AddInt32(&nextNode.VirtualLosses, 1)
		node = nextNode
		path = append(path, node)

		if rep.enabled {
			rep.push(node.Hash)
		}

		nextPos, ok := currPos.ApplyMove(mv)
		if !ok {
			break
		}
		currPos = nextPos
	}

	var utility float64
	if node.IsTerminal {
		if currPos.SideToMove == xionghan.Red {
			utility = -1.0 
		} else {
			utility = 1.0
		}
	} else {
		res, err := e.nn.Evaluate(currPos)
		if err != nil {
			utility = node.UtilityAvg
		} else {
			e.expandMCTSNode(node, currPos, res)
			utility = float64(res.LossProb*2.0 - 1.0)
			
			// Contempt (平局厌恶)
			if utility > -0.05 && utility < 0.05 {
				if currPos.SideToMove == xionghan.Red { utility -= mctsContempt } else { utility += mctsContempt }
			}
		}
	}

	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		n.mu.Lock()
		n.Visits++
		n.WeightSum += 1.0
		n.UtilityAvg += (utility - n.UtilityAvg) / float64(n.Visits)
		n.UtilitySqAvg += (utility*utility - n.UtilitySqAvg) / float64(n.Visits)
		n.mu.Unlock()
		
		if i > 0 {
			atomic.AddInt32(&n.VirtualLosses, -1)
		}
	}
}

func (e *Engine) selectMCTSChild(node *MCTSNode, rep *repetitionState) (xionghan.Move, *MCTSNode) {
	node.mu.Lock()
	defer node.mu.Unlock()

	var bestMove xionghan.Move
	var bestChild *MCTSNode
	maxPUCT := -1e20

	vis := float64(node.Visits)
	
	stdev := math.Sqrt(math.Max(0, node.UtilitySqAvg - node.UtilityAvg*node.UtilityAvg))
	stdevFactor := 1.0 + 0.5*(stdev/0.4 - 1.0)
	if stdevFactor < 0.5 { stdevFactor = 0.5 }
	if stdevFactor > 2.0 { stdevFactor = 2.0 }

	cpuct := (mctsCpuctExploration + mctsCpuctExplorationLog*math.Log((vis+mctsCpuctExplorationBase)/mctsCpuctExplorationBase)) * stdevFactor
	totalVisitsSqrt := math.Sqrt(vis + 0.01)
	
	// --- 对齐 C++: 动态 FPU 逻辑 ---
	policyProbMassVisited := float32(0)
	for mv, child := range node.Children {
		if atomic.LoadInt64(&child.Visits) > 0 {
			policyProbMassVisited += node.PriorMap[mv]
		}
	}
	// 随探索进度动态缩放的 FPU 减分
	fpuReduction := mctsFpuReductionMax * math.Sqrt(float64(policyProbMassVisited))
	fpuBase := node.NNValue
	if node.NextPla == xionghan.Black { fpuBase = -fpuBase }
	fpuValue := fpuBase - fpuReduction

	for mv, child := range node.Children {
		if rep.enabled && !rep.canEnter(child.Hash) { continue }

		v := float64(atomic.LoadInt64(&child.Visits))
		vLoss := float64(atomic.LoadInt32(&child.VirtualLosses))
		childWeight := v + vLoss
		
		var q float64
		if childWeight > 0 {
			q = child.UtilityAvg
			if node.NextPla == xionghan.Black { q = -q }
			if vLoss > 0 {
				q = (q*v + (-1.0)*vLoss) / childWeight
			}
			// Soft Repetition Penalty (软重复惩罚)
			if rep.enabled && rep.base[child.Hash] > 0 {
				q -= 0.15 * float64(rep.base[child.Hash])
			}
		} else {
			q = fpuValue
		}

		prior := float64(node.PriorMap[mv])
		u := cpuct * prior * totalVisitsSqrt / (1.0 + childWeight)
		
		puct := q + u
		if puct > maxPUCT {
			maxPUCT = puct
			bestMove = mv
			bestChild = child
		}
	}
	return bestMove, bestChild
}

func (e *Engine) expandMCTSNode(node *MCTSNode, pos *xionghan.Position, res *NNResult) {
	if !atomic.CompareAndSwapInt32(&node.State, StateUnevaluated, StateEvaluating) {
		return
	}

	// 仅在展开时使用专家规则过滤，确保 MCTS 站在高起点上
	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	moves = e.FilterUrgentPawnThreatMoves(pos, moves)
	moves = e.FilterBlunderMoves(pos, moves)
	moves = e.FilterVCFMoves(pos, moves)

	if len(moves) == 0 {
		node.IsTerminal = true
		atomic.StoreInt32(&node.State, StateExpanded)
		return
	}

	node.mu.Lock()
	defer node.mu.Unlock()

	node.NNValue = float64(res.LossProb*2.0 - 1.0)
	node.PriorMap = make(map[xionghan.Move]float32)

	fromGroups := make(map[int][]xionghan.Move)
	for _, mv := range moves {
		fromGroups[mv.From] = append(fromGroups[mv.From], mv)
	}

	type stage1Res struct {
		from int
		res  *NNResult
	}
	resChan := make(chan stage1Res, len(fromGroups))
	var wg sync.WaitGroup

	for from := range fromGroups {
		from := from
		pFrom := res.Policy[from]
		if pFrom <= 1e-6 {
			resChan <- stage1Res{from, nil}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _ := e.nn.EvaluateWithStage(pos, 1, from)
			resChan <- stage1Res{from, r}
		}()
	}

	go func() {
		wg.Wait()
		close(resChan)
	}()

	totalP := float32(0)
	type childInfo struct {
		mv   xionghan.Move
		hash uint64
		p    float32
	}
	var children []childInfo

	for r := range resChan {
		fromMoves := fromGroups[r.from]
		pFrom := res.Policy[r.from]
		for _, mv := range fromMoves {
			var p float32
			if r.res == nil {
				p = pFrom * (1.0 / float32(len(fromMoves)))
			} else {
				p = pFrom * r.res.Policy[mv.To]
			}
			
			nextPos, ok := pos.ApplyMove(mv)
			if !ok { continue }
			
			// 显著加强进攻导向 (Prior Biasing)
			if pos.Board.Squares[mv.To] != 0 { p *= 1.5 }
			if e.CanCaptureKingNext(nextPos) { p *= 2.0 }

			children = append(children, childInfo{mv, nextPos.EnsureHash(), p})
			node.PriorMap[mv] = p
			totalP += p
		}
	}

	if totalP > 0 {
		inv := 1.0 / totalP
		for mv := range node.PriorMap {
			node.PriorMap[mv] *= inv
		}
	}

	nextPla := xionghan.Black
	if node.NextPla == xionghan.Black { nextPla = xionghan.Red }
	
	for _, ci := range children {
		e.poolMu.Lock()
		childNode, ok := e.mctsPool[ci.hash]
		if !ok {
			childNode = NewMCTSNode(ci.mv, nextPla, ci.hash)
			e.mctsPool[ci.hash] = childNode
		}
		node.Children[ci.mv] = childNode
		e.poolMu.Unlock()
	}
	atomic.StoreInt32(&node.State, StateExpanded)
}
