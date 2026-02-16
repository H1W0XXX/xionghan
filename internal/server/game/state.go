package game

import (
	"time"

	"xionghan/internal/xionghan"
)

type GameState struct {
	ID        string
	Pos       *xionghan.Position
	CreatedAt time.Time
	UpdatedAt time.Time
}
