package mcts

import (
	"sync"
	"sync/atomic"
	"xionghan/internal/engine"
	"xionghan/internal/xionghan"
)

const (
	StateUnevaluated = iota
	StateEvaluating
	StateExpanded
)

type NodeStats struct {
	Visits     int64
	WeightSum  float64
	UtilityAvg float64
}

type MCTSNode struct {
	mu sync.Mutex

	Move      xionghan.Move
	NextPla   int // Side to move
	Parent    *MCTSNode
	Children  map[xionghan.Move]*MCTSNode
	State     int32 // Atomic
	
	// NN Output related
	PriorMap map[xionghan.Move]float32
	WinProb  float32 // From fixed color (Red) perspective, or next pla perspective?
	// KataGo usually uses a win-loss utility where 1.0 is next pla win, -1.0 is loss.
	// We'll use 1.0 for Red win, -1.0 for Black win, 0.0 for draw.
	
	Stats         NodeStats
	VirtualLosses int32 // Atomic
	
	IsTerminal bool
	Winner     int // 1: Draw, 2: Red, 3: Black
}

func NewNode(mv xionghan.Move, parent *MCTSNode, pla int) *MCTSNode {
	return &MCTSNode{
		Move:     mv,
		Parent:   parent,
		NextPla:  pla,
		Children: make(map[xionghan.Move]*MCTSNode),
		State:    StateUnevaluated,
	}
}

func (n *MCTSNode) GetChildWeight(visits int64) float64 {
	// Simple implementation, KataGo has more complex weight logic.
	return float64(visits)
}

func (n *MCTSNode) RecordPlayout(utility float64, weight float64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	
	oldVisits := n.Stats.Visits
	n.Stats.Visits++
	n.Stats.WeightSum += weight
	
	// Welford's algorithm or simple incremental update
	delta := utility - n.Stats.UtilityAvg
	n.Stats.UtilityAvg += delta * weight / n.Stats.WeightSum
	
	_ = oldVisits
}

func (n *MCTSNode) GetUtilityForSelection(pla int) float64 {
	// Return utility from the perspective of 'pla'
	avg := n.Stats.UtilityAvg
	if pla == xionghan.Red {
		return avg
	}
	return -avg
}
