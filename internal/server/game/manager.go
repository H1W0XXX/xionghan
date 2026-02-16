package game

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"xionghan/internal/xionghan"
)

type Manager struct {
	mu    sync.RWMutex
	games map[string]*GameState
}

func NewManager() *Manager {
	return &Manager{games: make(map[string]*GameState)}
}

func (m *Manager) NewGame() *GameState {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.NewString()
	g := &GameState{
		ID:        id,
		Pos:       xionghan.NewInitialPosition(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.games[id] = g
	return g
}

func (m *Manager) Get(id string) (*GameState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.games[id]
	if !ok {
		return nil, errors.New("game not found")
	}
	return g, nil
}

func (m *Manager) Update(id string, pos *xionghan.Position) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.games[id]
	if !ok {
		return errors.New("game not found")
	}
	g.Pos = pos
	g.UpdatedAt = time.Now()
	return nil
}
