package main

import (
	"fmt"

	"xionghan/internal/xionghan"
)

func main() {
	pos := xionghan.NewInitialPosition()
	fmt.Println("FEN:", pos.Encode())
	moves := pos.GeneratePseudoLegalMoves()
	fmt.Println("Pseudo legal moves:", len(moves))
}
