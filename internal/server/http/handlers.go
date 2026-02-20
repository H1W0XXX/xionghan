package httpserver

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"xionghan/internal/engine"
	"xionghan/internal/xionghan"
)

// 一个简单的内存对局管理：exe 本地跑，人类玩足够了
type Game struct {
	Pos *xionghan.Position
}

var (
	games   = make(map[string]*Game)
	gamesMu sync.RWMutex

	aiEngine = engine.NewEngine()
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func newGameID() string {
	return time.Now().Format("20060102T150405") + "-" + randomString(6)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// Handler 实现 http.Handler，用于 /api/* 路由
type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) Engine() *engine.Engine {
	return aiEngine
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/new_game":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleNewGame(w, r)

	case "/api/play":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handlePlay(w, r)

	case "/api/state":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleState(w, r)

	case "/api/ai_move": // 新增
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleAiMove(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleNewGame(w http.ResponseWriter, r *http.Request) {
	pos := xionghan.NewInitialPosition()
	legal := pos.GenerateLegalMoves(false)

	game := &Game{Pos: pos}
	id := newGameID()

	gamesMu.Lock()
	games[id] = game
	gamesMu.Unlock()

	resp := NewGameResponse{
		GameID:     id,
		Position:   pos.Encode(),
		ToMove:     sideToInt(pos.SideToMove),
		LegalMoves: movesToDTO(legal),
	}
	writeJSON(w, resp)
}

func (h *Handler) handlePlay(w http.ResponseWriter, r *http.Request) {
	var req PlayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	gamesMu.RLock()
	game, ok := games[req.GameID]
	gamesMu.RUnlock()
	if !ok {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}

	pos := game.Pos
	legal := pos.GenerateLegalMoves(false)

	// 确认这步是不是合法招之一
	var found *xionghan.Move
	for i := range legal {
		if legal[i].From == req.Move.From && legal[i].To == req.Move.To {
			found = &legal[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "illegal move", http.StatusBadRequest)
		return
	}

	newPos, ok2 := pos.ApplyMove(*found)
	if !ok2 {
		http.Error(w, "apply move failed", http.StatusInternalServerError)
		return
	}

	// 更新对局
	game.Pos = newPos
	legal2 := newPos.GenerateLegalMoves(false)

	status := "ongoing"
	// TODO: 以后加上将死 / 和棋判断

	resp := PlayResponse{
		Position:   newPos.Encode(),
		ToMove:     sideToInt(newPos.SideToMove),
		LegalMoves: movesToDTO(legal2),
		Status:     status,
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("writeJSON error:", err)
	}
}

func (h *Handler) handleState(w http.ResponseWriter, r *http.Request) {
	var req StateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	gamesMu.RLock()
	game, ok := games[req.GameID]
	gamesMu.RUnlock()
	if !ok || game == nil || game.Pos == nil {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}

	pos := game.Pos
	legal := pos.GenerateLegalMoves(false)

	status := "ongoing" // 以后你可以在 Game 里存状态，这里直接返回

	resp := StateResponse{
		Position:   pos.Encode(),
		ToMove:     sideToInt(pos.SideToMove),
		LegalMoves: movesToDTO(legal),
		Status:     status,
	}
	writeJSON(w, resp)
}

func (h *Handler) handleAiMove(w http.ResponseWriter, r *http.Request) {
	var req AiMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	if req.Position == "" {
		http.Error(w, "missing position", http.StatusBadRequest)
		return
	}

	// ===== 1. 从字符串局面还原 Position =====
	// 这里假设你有类似这样的函数：
	//   func DecodePosition(enc string) (*Position, error)
	// 如果你实际名字不同，在这里改一下即可。
	pos, err := xionghan.DecodePosition(req.Position)
	if err != nil {
		http.Error(w, "invalid position", http.StatusBadRequest)
		return
	}

	// 设置轮到谁走（以请求参数为准）；若与 FEN 不同，同步重建 Hash 保持一致性。
	reqSide := intToSide(req.ToMove)
	if pos.SideToMove != reqSide {
		pos.SideToMove = reqSide
		pos.Hash = pos.CalculateHash()
	}

	// ===== 2. 搜索参数 =====
	depth := req.MaxDepth
	if depth <= 0 {
		depth = 3
	}
	var limit time.Duration
	if req.TimeMs > 0 {
		limit = time.Duration(req.TimeMs) * time.Millisecond
	}

	cfg := engine.SearchConfig{
		MaxDepth:  depth,
		TimeLimit: limit,
	}

	// ===== 3. 调用搜索，只思考不落子 =====
	res := aiEngine.Search(pos, cfg)

	// NN 推理失败：本次请求直接失败，不落子不换边。
	if res.NNFailed {
		resp := AiMoveResponse{
			BestMove: MoveDTO{From: -1, To: -1},
			Score:    res.Score,
			Depth:    res.Depth,
			Nodes:    res.Nodes,
			TimeMs:   res.TimeUsed.Milliseconds(),
			Position: pos.Encode(),
			ToMove:   sideToInt(pos.SideToMove),
			Status:   "nn_error",
		}
		writeJSON(w, resp)
		return
	}

	// 没有走法
	if res.BestMove.From == 0 && res.BestMove.To == 0 {
		resp := AiMoveResponse{
			BestMove: MoveDTO{From: -1, To: -1},
			Score:    res.Score,
			Depth:    res.Depth,
			Nodes:    res.Nodes,
			TimeMs:   res.TimeUsed.Milliseconds(),
			Position: pos.Encode(),              // 原局面
			ToMove:   sideToInt(pos.SideToMove), // 当前轮到谁（理论上和 req.ToMove 一样）
			Status:   "no_moves",
		}
		writeJSON(w, resp)
		return
	}

	// 正常返回
	resp := AiMoveResponse{
		BestMove: MoveDTO{
			From: res.BestMove.From,
			To:   res.BestMove.To,
		},
		Score:    res.Score,
		WinProb:  res.WinProb,
		Depth:    res.Depth,
		Nodes:    res.Nodes,
		TimeMs:   res.TimeUsed.Milliseconds(),
		Position: pos.Encode(),              // 仍是原局面
		ToMove:   sideToInt(pos.SideToMove), // 当前轮到谁
		Status:   "ok",
	}
	writeJSON(w, resp)
}
