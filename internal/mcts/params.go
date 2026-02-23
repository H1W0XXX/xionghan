package mcts

import (
	"math"
	"time"
)

// SearchParams 对应 C++ 的 SearchParams
type SearchParams struct {
	Simulations    int
	MaxTime        time.Duration
	NumThreads     int
	
	CpuctExploration     float64
	CpuctExplorationBase float64
	CpuctExplorationLog  float64
	
	FpuReductionMax     float64
	RootFpuReductionMax float64
	FpuLossProp         float64
	
	NumVirtualLossesPerThread float64
	
	// 匈汉象棋特有的
	WinLossUtilityFactor float64
}

func DefaultParams() SearchParams {
	return SearchParams{
		Simulations:               800,
		MaxTime:                   5 * time.Second,
		NumThreads:                8,
		CpuctExploration:          1.1,
		CpuctExplorationBase:      10000.0,
		CpuctExplorationLog:       0.4,
		FpuReductionMax:           0.2,
		RootFpuReductionMax:       0.2,
		FpuLossProp:               0.0,
		NumVirtualLossesPerThread: 1.0,
		WinLossUtilityFactor:      1.0,
	}
}

func (p *SearchParams) GetCpuct(totalChildWeight float64) float64 {
	return p.CpuctExploration + p.CpuctExplorationLog*math.Log((totalChildWeight+p.CpuctExplorationBase)/p.CpuctExplorationBase)
}
