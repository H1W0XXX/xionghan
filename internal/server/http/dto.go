package httpserver

import "xionghan/internal/xionghan"

// AiMoveRequest 请求让 AI 为当前局面走一步
type AiMoveRequest struct {
	GameID   string `json:"game_id"`  // 对局 ID，用于读取重复局面历史
	Position string `json:"position"` // 当前局面（前端把 pos.Encode() 传回来）
	ToMove   int    `json:"to_move"`  // 0=红, 1=黑（或绿），和你 sideToInt 对应
	MaxDepth int    `json:"max_depth"`
	TimeMs   int64  `json:"time_ms"`

	// MCTS 相关的参数
	UseMCTS         bool `json:"use_mcts"`
	MCTSSimulations int  `json:"mcts_simulations"`
}

// 前端用的招法结构
type MoveDTO struct {
	From int `json:"from"`
	To   int `json:"to"`
}

func dtoToMove(m MoveDTO) xionghan.Move {
	return xionghan.Move{From: m.From, To: m.To}
}

type AiMoveResponse struct {
	BestMove   MoveDTO   `json:"best_move"`
	Score      int       `json:"score"`
	WinProb    float32   `json:"win_prob"` // 新增：红方胜率
	Depth      int       `json:"depth"`
	Nodes      int64     `json:"nodes"`
	Position   string    `json:"position"`    // AI 落子后局面
	ToMove     int       `json:"to_move"`     // 下一手执棋方（人类）
	LegalMoves []MoveDTO `json:"legal_moves"` // 下一手所有可走棋
	Status     string    `json:"status"`      // "ongoing" / "no_moves" / 以后再扩展赢家
	TimeMs     int64     `json:"time_ms"`
}

// NewGame 返回
type NewGameResponse struct {
	GameID     string    `json:"game_id"`
	Position   string    `json:"position"`    // FEN 字符串
	ToMove     int       `json:"to_move"`     // 0=红(w),1=黑(b)
	LegalMoves []MoveDTO `json:"legal_moves"` // 当前所有可走棋
}

// Play 请求
type PlayRequest struct {
	GameID string  `json:"game_id"`
	Move   MoveDTO `json:"move"`
}

// Play 返回
type PlayResponse struct {
	Position   string    `json:"position"`
	ToMove     int       `json:"to_move"`
	LegalMoves []MoveDTO `json:"legal_moves"`
	Status     string    `json:"status"` // "ongoing" / "checkmate" / "draw" 先留着
}

func sideToInt(s xionghan.Side) int {
	switch s {
	case xionghan.Red:
		return 0
	case xionghan.Black:
		return 1
	default:
		return -1
	}
}

func intToSide(v int) xionghan.Side {
	if v == 1 {
		return xionghan.Black
	}
	return xionghan.Red
}

func moveToDTO(m xionghan.Move) MoveDTO {
	return MoveDTO{From: m.From, To: m.To}
}

func movesToDTO(ms []xionghan.Move) []MoveDTO {
	out := make([]MoveDTO, len(ms))
	for i, m := range ms {
		out[i] = moveToDTO(m)
	}
	return out
}

// State 请求：前端刷新时用 game_id 来要当前盘面
type StateRequest struct {
	GameID string `json:"game_id"`
}

// State 返回：结构基本和 NewGameResponse 一样
type StateResponse struct {
	Position   string    `json:"position"`
	ToMove     int       `json:"to_move"`
	LegalMoves []MoveDTO `json:"legal_moves"`
	Status     string    `json:"status"` // 先统一用 "ongoing"
}
