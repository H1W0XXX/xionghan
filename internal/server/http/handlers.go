package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"xionghan/internal/engine"
	"xionghan/internal/xionghan"
)

// 一个简单的内存对局管理：exe 本地跑，人类玩足够了
type Game struct {
	Pos       *xionghan.Position
	HashCount map[uint64]int
	Engine    *engine.Engine
	LastMove  time.Time
}

var (
	games   = make(map[string]*Game)
	gamesMu sync.RWMutex

	gameSeq  uint64
	aiEngine = engine.NewEngine()
)

const (
	gameIdleTTL       = 30 * time.Minute
	gameCleanupPeriod = 1 * time.Minute
)

func init() {
	rand.Seed(time.Now().UnixNano())
	startIdleGameJanitor()
}

func newGameID() string {
	seq := atomic.AddUint64(&gameSeq, 1)
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), seq)
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
	now := time.Now()

	game := &Game{
		Pos:       pos,
		HashCount: map[uint64]int{pos.EnsureHash(): 1},
		Engine:    aiEngine.CloneForGame(),
		LastMove:  now,
	}
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

	gamesMu.Lock()
	game, ok := games[req.GameID]
	if !ok {
		gamesMu.Unlock()
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	if game == nil || game.Pos == nil {
		gamesMu.Unlock()
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}

	pos := game.Pos
	ensureGameHashCount(game)
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
		gamesMu.Unlock()
		http.Error(w, "illegal move", http.StatusBadRequest)
		return
	}

	newPos, ok2 := pos.ApplyMove(*found)
	if !ok2 {
		gamesMu.Unlock()
		http.Error(w, "apply move failed", http.StatusInternalServerError)
		return
	}
	if shouldEnableRepetitionRule(pos) && isRepetitionForbidden(game.HashCount, newPos) {
		gamesMu.Unlock()
		http.Error(w, "repetition_forbidden", http.StatusBadRequest)
		return
	}

	// 更新对局
	game.Pos = newPos
	game.HashCount[newPos.EnsureHash()]++
	touchGameLocked(game)
	gamesMu.Unlock()

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
	if !ok {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	if game == nil || game.Pos == nil {
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
	if req.GameID == "" {
		http.Error(w, "missing game_id", http.StatusBadRequest)
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
	legalNow := movesToDTO(pos.GenerateLegalMoves(false))
	gameEngine, historyCount, err := snapshotGameAIContext(req.GameID)
	if err != nil {
		http.Error(w, "game not found", http.StatusNotFound)
		return
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
		MaxDepth:               depth,
		TimeLimit:              limit,
		EnableRepetitionFilter: shouldEnableRepetitionRule(pos),
		RepetitionCount:        historyCount,
		RepetitionBanCount:     3,
	}

	// ===== 3. 调用搜索，只思考不落子 =====
	res := gameEngine.Search(pos, cfg)

	// NN 推理失败：本次请求直接失败，不落子不换边。
	if res.NNFailed {
		moves := pos.GenerateLegalMoves(true)
		moves = gameEngine.FilterLeiLockedMoves(pos, moves)
		moves = gameEngine.FilterUrgentPawnThreatMoves(pos, moves)
		moves = gameEngine.FilterBlunderMoves(pos, moves)
		moves = gameEngine.FilterVCFMoves(pos, moves)
		if shouldEnableRepetitionRule(pos) {
			filtered := make([]xionghan.Move, 0, len(moves))
			for _, mv := range moves {
				nextPos, ok := pos.ApplyMove(mv)
				if !ok {
					continue
				}
				if isRepetitionForbidden(historyCount, nextPos) {
					continue
				}
				filtered = append(filtered, mv)
			}
			moves = filtered
		}
		if len(moves) > 0 {
			fallback := moves[rand.Intn(len(moves))]
			resp := AiMoveResponse{
				BestMove: MoveDTO{
					From: fallback.From,
					To:   fallback.To,
				},
				Score:      0,
				WinProb:    0.5,
				Depth:      res.Depth,
				Nodes:      res.Nodes,
				TimeMs:     res.TimeUsed.Milliseconds(),
				Position:   pos.Encode(),
				ToMove:     sideToInt(pos.SideToMove),
				LegalMoves: legalNow,
				Status:     "ok",
			}
			writeJSON(w, resp)
			return
		}
		resp := AiMoveResponse{
			BestMove:   MoveDTO{From: -1, To: -1},
			Score:      res.Score,
			Depth:      res.Depth,
			Nodes:      res.Nodes,
			TimeMs:     res.TimeUsed.Milliseconds(),
			Position:   pos.Encode(),
			ToMove:     sideToInt(pos.SideToMove),
			LegalMoves: legalNow,
			Status:     "no_moves",
		}
		writeJSON(w, resp)
		return
	}

	// 没有走法
	if res.BestMove.From == 0 && res.BestMove.To == 0 {
		resp := AiMoveResponse{
			BestMove:   MoveDTO{From: -1, To: -1},
			Score:      res.Score,
			Depth:      res.Depth,
			Nodes:      res.Nodes,
			TimeMs:     res.TimeUsed.Milliseconds(),
			Position:   pos.Encode(),              // 原局面
			ToMove:     sideToInt(pos.SideToMove), // 当前轮到谁（理论上和 req.ToMove 一样）
			LegalMoves: legalNow,
			Status:     "no_moves",
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
		Score:      res.Score,
		WinProb:    res.WinProb,
		Depth:      res.Depth,
		Nodes:      res.Nodes,
		TimeMs:     res.TimeUsed.Milliseconds(),
		Position:   pos.Encode(),              // 仍是原局面
		ToMove:     sideToInt(pos.SideToMove), // 当前轮到谁
		LegalMoves: legalNow,
		Status:     "ok",
	}
	writeJSON(w, resp)
}

const repetitionRulePieceThreshold = 40

func ensureGameHashCount(game *Game) {
	if game == nil || game.Pos == nil {
		return
	}
	if game.HashCount == nil {
		game.HashCount = make(map[uint64]int)
	}
	if len(game.HashCount) == 0 {
		game.HashCount[game.Pos.EnsureHash()] = 1
	}
}

func touchGameLocked(game *Game) {
	if game == nil {
		return
	}
	game.LastMove = time.Now()
}

func shouldEnableRepetitionRule(pos *xionghan.Position) bool {
	if pos == nil {
		return false
	}
	return pos.TotalPieces() < repetitionRulePieceThreshold
}

func isRepetitionForbidden(hashCount map[uint64]int, nextPos *xionghan.Position) bool {
	if nextPos == nil {
		return true
	}
	nextHash := nextPos.EnsureHash()
	return hashCount[nextHash]+1 >= 3
}

func copyHashCountLocked(game *Game) map[uint64]int {
	out := make(map[uint64]int)
	if game == nil || game.Pos == nil {
		return out
	}
	if game.HashCount != nil && len(game.HashCount) > 0 {
		out = make(map[uint64]int, len(game.HashCount))
		for k, v := range game.HashCount {
			out[k] = v
		}
		return out
	}
	hash := game.Pos.Hash
	if hash == 0 {
		hash = game.Pos.CalculateHash()
	}
	out[hash] = 1
	return out
}

func snapshotGameAIContext(gameID string) (*engine.Engine, map[uint64]int, error) {
	gamesMu.Lock()
	game, ok := games[gameID]
	if !ok || game == nil || game.Pos == nil {
		gamesMu.Unlock()
		return nil, nil, errors.New("game not found")
	}
	ensureGameHashCount(game)
	if game.Engine == nil {
		game.Engine = aiEngine.CloneForGame()
	}
	touchGameLocked(game)
	history := copyHashCountLocked(game)
	gameEngine := game.Engine
	gamesMu.Unlock()
	return gameEngine, history, nil
}

func startIdleGameJanitor() {
	go func() {
		ticker := time.NewTicker(gameCleanupPeriod)
		defer ticker.Stop()
		for now := range ticker.C {
			cleaned := cleanupIdleGames(now)
			if cleaned > 0 {
				log.Printf("cleaned %d idle games (ttl=%s)", cleaned, gameIdleTTL)
			}
		}
	}()
}

func cleanupIdleGames(now time.Time) int {
	gamesMu.Lock()
	defer gamesMu.Unlock()

	removed := 0
	for id, game := range games {
		if game == nil {
			delete(games, id)
			removed++
			continue
		}
		last := game.LastMove
		if last.IsZero() {
			last = now
		}
		if now.Sub(last) >= gameIdleTTL {
			delete(games, id)
			removed++
		}
	}
	return removed
}
