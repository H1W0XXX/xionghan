package engine

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"xionghan/internal/xionghan"
)

// MCTSNode MCTS 搜索节点
type MCTSNode struct {
	mu sync.RWMutex

	Move     xionghan.Move
	NextPla  xionghan.Side
	Parent   *MCTSNode
	Children map[xionghan.Move]*MCTSNode
	// EdgeVisits stores visits on parent->child edges.
	// This avoids transposition pollution from using child node visits directly.
	EdgeVisits map[xionghan.Move]int64
	State      int32

	PriorMap map[xionghan.Move]float32
	NNValue  float64

	Visits        int64
	WeightSum     float64
	UtilityAvg    float64
	UtilitySqAvg  float64
	VirtualLosses int32

	IsTerminal int32
	Hash       uint64
}

func NewMCTSNode(mv xionghan.Move, pla xionghan.Side, hash uint64) *MCTSNode {
	return &MCTSNode{
		Move:       mv,
		NextPla:    pla,
		Hash:       hash,
		Children:   make(map[xionghan.Move]*MCTSNode),
		EdgeVisits: make(map[xionghan.Move]int64),
		State:      StateUnevaluated,
	}
}

const (
	mctsCpuctExploration     = 1.1
	mctsCpuctExplorationBase = 10000.0
	mctsCpuctExplorationLog  = 0.45
	mctsFpuReductionMax      = 0.2
	mctsContempt             = 0.03
	// KataGo-style root LCB guard: penalize high-variance root branches.
	mctsRootLCBEnabled      = true
	mctsRootLCBStdevs       = 4.0
	mctsRootLCBMinVisitProp = 0.1

	StateUnevaluated = iota
	StateEvaluating
	StateExpanded
)

func (e *Engine) runMCTS(pos *xionghan.Position, cfg SearchConfig) SearchResult {
	start := time.Now()
	h := pos.EnsureHash()
	repBase := newRepetitionState(cfg)

	// 0. 绝杀判定：直接吃王
	moves := pos.GenerateLegalMoves(true)
	moves = e.FilterLeiLockedMoves(pos, moves)
	for _, mv := range moves {
		targetPiece := pos.Board.Squares[mv.To]
		if targetPiece != 0 && targetPiece.Type() == xionghan.PieceKing {
			if repBase.enabled {
				nextPos, ok := pos.ApplyMove(mv)
				if !ok || !repBase.canEnter(nextPos.EnsureHash()) {
					continue
				}
			}
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

	// 1. VCF 连将赢判定（抢杀）
	if pos.TotalPieces() <= 43 {
		vcfRes := e.VCFSearch(pos, vcfDepthRoot)
		if vcfRes.CanWin {
			if repBase.enabled {
				nextPos, ok := pos.ApplyMove(vcfRes.Move)
				if !ok || !repBase.canEnter(nextPos.EnsureHash()) {
					goto skipMCTSVCF
				}
			}
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
skipMCTSVCF:

	allowTransposition := !repBase.enabled

	var root *MCTSNode
	if allowTransposition {
		e.poolMu.Lock()
		if e.mctsPool == nil {
			e.mctsPool = make(map[uint64]*MCTSNode, 1<<16)
		}
		if len(e.mctsPool) > 300000 {
			e.mctsPool = make(map[uint64]*MCTSNode, 1<<16)
		}
		var ok bool
		root, ok = e.mctsPool[h]
		if !ok {
			root = NewMCTSNode(xionghan.Move{}, pos.SideToMove, h)
			e.mctsPool[h] = root
		}
		e.mctsRoot = root
		e.poolMu.Unlock()
	} else {
		// Repetition constraints are path-dependent, so disable transposition sharing.
		root = NewMCTSNode(xionghan.Move{}, pos.SideToMove, h)
		e.mctsRoot = root
	}

	// 1. 根节点展开：这里保留专家过滤，保证“起手不弱智”
	if atomic.LoadInt32(&root.State) == StateUnevaluated {
		res, err := e.nn.Evaluate(pos)
		if err != nil {
			e.markNNFailure()
			return SearchResult{}
		}
		// 特殊处理：根节点展开使用 full 模式
		e.expandMCTSNodeInternal(root, pos, res, true, allowTransposition)
	}

	// 动态线程
	numThreads := 16
	if e.nn != nil && (e.nn.selectedProvider == "XNNPACK" || e.nn.selectedProvider == "CPU") {
		numThreads = 4
	}

	simsPerThread := cfg.MCTSSimulations / numThreads
	if simsPerThread < 1 {
		simsPerThread = 1
	}

	var wg sync.WaitGroup
	for t := 0; t < numThreads; t++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localRep := repBase.clone()
			for i := 0; i < simsPerThread; i++ {
				if cfg.TimeLimit > 0 && time.Since(start) > cfg.TimeLimit {
					break
				}
				e.mctsPlayout(root, pos, cfg, localRep, allowTransposition)
			}
		}()
	}
	wg.Wait()

	root.mu.RLock()
	defer root.mu.RUnlock()

	bestMove := xionghan.Move{}
	maxVisits := int64(-1)
	for mv := range root.Children {
		v := root.EdgeVisits[mv]
		if v > maxVisits {
			maxVisits = v
			bestMove = mv
		}
	}

	redWinProb := (root.UtilityAvg + 1.0) / 2.0
	return SearchResult{
		BestMove: bestMove,
		Score:    int((redWinProb*2.0 - 1.0) * 10000),
		WinProb:  float32(redWinProb),
		Nodes:    root.Visits,
		TimeUsed: time.Since(start),
		PV:       []xionghan.Move{bestMove},
	}
}

func applyMCTSContempt(utility float64, sideToMove xionghan.Side) float64 {
	if utility > -0.05 && utility < 0.05 {
		if sideToMove == xionghan.Red {
			return utility - mctsContempt
		}
		return utility + mctsContempt
	}
	return utility
}

func (e *Engine) mctsPlayout(root *MCTSNode, pos *xionghan.Position, cfg SearchConfig, rep *repetitionState, allowTransposition bool) {
	node := root
	currPos := pos
	var path []*MCTSNode
	var edgePath []xionghan.Move
	path = append(path, node)

	for {
		if atomic.LoadInt32(&node.State) != StateExpanded || atomic.LoadInt32(&node.IsTerminal) != 0 {
			break
		}

		mv, nextNode := e.selectMCTSChild(node, rep, node == root)
		if nextNode == nil {
			break
		}

		atomic.AddInt32(&nextNode.VirtualLosses, 1)
		node = nextNode
		path = append(path, node)
		edgePath = append(edgePath, mv)

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
	if atomic.LoadInt32(&node.IsTerminal) != 0 {
		if currPos.SideToMove == xionghan.Red {
			utility = -1.0
		} else {
			utility = 1.0
		}
	} else {
		for {
			if atomic.LoadInt32(&node.IsTerminal) != 0 {
				if currPos.SideToMove == xionghan.Red {
					utility = -1.0
				} else {
					utility = 1.0
				}
				break
			}

			state := atomic.LoadInt32(&node.State)
			if state == StateExpanded {
				node.mu.RLock()
				utility = node.NNValue
				node.mu.RUnlock()
				utility = applyMCTSContempt(utility, currPos.SideToMove)
				break
			}

			if state == StateUnevaluated && atomic.CompareAndSwapInt32(&node.State, StateUnevaluated, StateEvaluating) {
				res, err := e.nn.Evaluate(currPos)
				if err != nil {
					node.mu.RLock()
					utility = node.UtilityAvg
					node.mu.RUnlock()
					// Allow retry by another playout instead of leaving node permanently evaluating.
					atomic.StoreInt32(&node.State, StateUnevaluated)
				} else {
					// 2. 内部节点展开：禁用沉重的 Blunder/VCF 过滤，恢复速度
					e.expandMCTSNodeFromEvaluating(node, currPos, res, false, allowTransposition)
					utility = float64(res.LossProb*2.0 - 1.0)
				}
				utility = applyMCTSContempt(utility, currPos.SideToMove)
				break
			}

			// Another thread is evaluating this node, wait and reuse that result.
			runtime.Gosched()
		}
	}

	// Backprop
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		n.mu.Lock()
		n.Visits++
		n.WeightSum += 1.0
		n.UtilityAvg += (utility - n.UtilityAvg) / float64(n.Visits)
		n.UtilitySqAvg += (utility*utility - n.UtilitySqAvg) / float64(n.Visits)
		n.mu.Unlock()

		if i > 0 {
			parent := path[i-1]
			mv := edgePath[i-1]
			parent.mu.Lock()
			parent.EdgeVisits[mv]++
			parent.mu.Unlock()

			atomic.AddInt32(&n.VirtualLosses, -1)
			if rep.enabled {
				rep.pop(n.Hash)
			}
		}
	}
}

func (e *Engine) selectMCTSChild(node *MCTSNode, rep *repetitionState, isRoot bool) (xionghan.Move, *MCTSNode) {
	node.mu.RLock()
	defer node.mu.RUnlock()

	var bestMove xionghan.Move
	var bestChild *MCTSNode
	maxPUCT := -1e20

	vis := node.Visits

	stdev := math.Sqrt(math.Max(0, node.UtilitySqAvg-node.UtilityAvg*node.UtilityAvg))
	stdevFactor := 1.0 + 0.5*(stdev/0.4-1.0)
	if stdevFactor < 0.5 {
		stdevFactor = 0.5
	}
	if stdevFactor > 2.0 {
		stdevFactor = 2.0
	}

	cpuct := (mctsCpuctExploration + mctsCpuctExplorationLog*math.Log((float64(vis)+mctsCpuctExplorationBase)/mctsCpuctExplorationBase)) * stdevFactor
	totalVisitsSqrt := math.Sqrt(float64(vis) + 0.01)

	fpuReduction := mctsFpuReductionMax * math.Sqrt(math.Max(0, math.Min(1, float64(vis)/100.0)))
	fpuBase := node.NNValue
	if node.NextPla == xionghan.Black {
		fpuBase = -fpuBase
	}
	fpuValue := fpuBase - fpuReduction

	// For root LCB gating, compute max edge visits among selectable children.
	maxEdgeVisits := 0.0
	if isRoot && mctsRootLCBEnabled {
		for mv, child := range node.Children {
			if rep.enabled && !rep.canEnter(child.Hash) {
				continue
			}
			v := float64(node.EdgeVisits[mv])
			if v > maxEdgeVisits {
				maxEdgeVisits = v
			}
		}
	}

	for mv, child := range node.Children {
		if rep.enabled && !rep.canEnter(child.Hash) {
			continue
		}

		edgeVisits := float64(node.EdgeVisits[mv])
		vLoss := float64(atomic.LoadInt32(&child.VirtualLosses))
		childWeight := edgeVisits + vLoss

		var q float64
		child.mu.RLock()
		childVisits := float64(child.Visits)
		childUtilityAvg := child.UtilityAvg
		childUtilitySqAvg := child.UtilitySqAvg
		child.mu.RUnlock()

		if edgeVisits > 0 && childVisits > 0 {
			q = childUtilityAvg
			if node.NextPla == xionghan.Black {
				q = -q
			}

			if isRoot && mctsRootLCBEnabled && maxEdgeVisits > 0 &&
				edgeVisits >= mctsRootLCBMinVisitProp*maxEdgeVisits {
				variance := childUtilitySqAvg - childUtilityAvg*childUtilityAvg
				if variance < 0 {
					variance = 0
				}
				// Lower confidence bound on utility from current player perspective.
				lcbRadius := mctsRootLCBStdevs * math.Sqrt(variance/math.Max(edgeVisits, 1.0))
				q -= lcbRadius
			}

			if vLoss > 0 {
				q = (q*edgeVisits + (-1.0)*vLoss) / childWeight
			}
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

// expandMCTSNode 兼容接口
func (e *Engine) expandMCTSNode(node *MCTSNode, pos *xionghan.Position, res *NNResult) {
	e.expandMCTSNodeInternal(node, pos, res, false, true)
}

func (e *Engine) expandMCTSNodeInternal(node *MCTSNode, pos *xionghan.Position, res *NNResult, useFullFilter bool, allowTransposition bool) {
	if !atomic.CompareAndSwapInt32(&node.State, StateUnevaluated, StateEvaluating) {
		return
	}
	e.expandMCTSNodeFromEvaluating(node, pos, res, useFullFilter, allowTransposition)
}

func (e *Engine) expandMCTSNodeFromEvaluating(node *MCTSNode, pos *xionghan.Position, res *NNResult, useFullFilter bool, allowTransposition bool) {
	moves := pos.GenerateLegalMoves(true)
	if useFullFilter {
		// 只有根节点才跑这些重的过滤
		moves = e.FilterLeiLockedMoves(pos, moves)
		moves = e.FilterUrgentPawnThreatMoves(pos, moves)
		moves = e.FilterBlunderMoves(pos, moves)
		moves = e.FilterVCFMoves(pos, moves)
	} else {
		// 内部节点只跑最轻量的
		moves = e.FilterLeiLockedMoves(pos, moves)
	}

	if len(moves) == 0 {
		atomic.StoreInt32(&node.IsTerminal, 1)
		atomic.StoreInt32(&node.State, StateExpanded)
		return
	}

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

	priorMap := make(map[xionghan.Move]float32)
	totalP := float32(0)
	type childInfo struct {
		mv   xionghan.Move
		hash uint64
		p    float32
	}
	var childrenInfo []childInfo

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

			// 虽然 ApplyMove 有点慢，但为了转置表（Transposition）这是必须的
			// 我们已经移除了内部节点的 VCF/Blunder，速度应该能接受了
			nextPos, ok := pos.ApplyMove(mv)
			if !ok {
				continue
			}

			childrenInfo = append(childrenInfo, childInfo{mv, nextPos.EnsureHash(), p})
			priorMap[mv] = p
			totalP += p
		}
	}

	if totalP > 0 {
		inv := 1.0 / totalP
		for mv := range priorMap {
			priorMap[mv] *= inv
		}
	}

	node.mu.Lock()
	node.NNValue = float64(res.LossProb*2.0 - 1.0)
	node.PriorMap = priorMap

	nextPla := xionghan.Black
	if node.NextPla == xionghan.Black {
		nextPla = xionghan.Red
	}

	for _, ci := range childrenInfo {
		var childNode *MCTSNode
		if allowTransposition {
			e.poolMu.Lock()
			var ok bool
			childNode, ok = e.mctsPool[ci.hash]
			if !ok {
				childNode = NewMCTSNode(ci.mv, nextPla, ci.hash)
				e.mctsPool[ci.hash] = childNode
			}
			e.poolMu.Unlock()
		} else {
			childNode = NewMCTSNode(ci.mv, nextPla, ci.hash)
		}
		node.Children[ci.mv] = childNode
		if _, ok := node.EdgeVisits[ci.mv]; !ok {
			node.EdgeVisits[ci.mv] = 0
		}
	}
	node.mu.Unlock()

	atomic.StoreInt32(&node.State, StateExpanded)
}
