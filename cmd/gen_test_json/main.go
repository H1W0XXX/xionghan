package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"
	"xionghan/internal/xionghan"
)

const (
	CppStride      = 14
	CppMaxArrSize  = 211
	CppP_BLACK     = 1 // 对应 Go 的 Red
	CppP_WHITE     = 2 // 对应 Go 的 Black
)

type TestCase struct {
	Board   []int8 `json:"board"`
	Pla     int    `json:"pla"`
	Stage   int    `json:"stage"`
	MidLoc0 int    `json:"midLoc0"`
	Mask    []int8 `json:"mask"`
}

func getCppLoc(sq int) int {
	r := sq / 13
	c := sq % 13
	return (c + 1) + (r + 1)*CppStride
}

func encodeBoard(pos *xionghan.Position) []int8 {
	board := make([]int8, CppMaxArrSize)
	for i := range board {
		board[i] = 3 // C_WALL
	}
	for r := 0; r < 13; r++ {
		for c := 0; c < 13; c++ {
			board[(c+1)+(r+1)*CppStride] = 0 // C_EMPTY
		}
	}

	for sq := 0; sq < xionghan.NumSquares; sq++ {
		pc := pos.Board.Squares[sq]
		if pc == 0 {
			continue
		}
		pt := int8(pc.Type())
		var cppPla int8
		if pc.Side() == xionghan.Red {
			cppPla = CppP_BLACK
		} else {
			cppPla = CppP_WHITE
		}
		board[getCppLoc(sq)] = (cppPla << 4) | pt
	}
	return board
}

func main() {
	rand.Seed(time.Now().UnixNano())
	var testCases []TestCase

	numGames := 10
	for g := 0; g < numGames; g++ {
		pos := xionghan.NewInitialPosition()
		maxMoves := 500 // 一局通常没这么多步，主要是防死循环
		for moveCount := 0; moveCount < maxMoves; moveCount++ {
			legalMoves := pos.GenerateLegalMoves(false)
			if len(legalMoves) == 0 {
				break
			}

			cppBoard := encodeBoard(pos)
			var cppPla int
			if pos.SideToMove == xionghan.Red {
				cppPla = CppP_BLACK
			} else {
				cppPla = CppP_WHITE
			}

			// --- Stage 0: 生成选择棋子的 Mask ---
			mask0 := make([]int8, CppMaxArrSize)
			froms := make(map[int]bool)
			for _, mv := range legalMoves {
				froms[getCppLoc(mv.From)] = true
			}
			for loc := range froms {
				mask0[loc] = 1
			}

			testCases = append(testCases, TestCase{
				Board:   cppBoard,
				Pla:     cppPla,
				Stage:   0,
				MidLoc0: 0,
				Mask:    mask0,
			})

			// 随机选一步
			chosenMove := legalMoves[rand.Intn(len(legalMoves))]

			// --- Stage 1: 生成选中棋子后落点的 Mask ---
			mask1 := make([]int8, CppMaxArrSize)
			for _, mv := range legalMoves {
				if mv.From == chosenMove.From {
					mask1[getCppLoc(mv.To)] = 1
				}
			}

			testCases = append(testCases, TestCase{
				Board:   cppBoard,
				Pla:     cppPla,
				Stage:   1,
				MidLoc0: getCppLoc(chosenMove.From),
				Mask:    mask1,
			})

			// 应用移动
			nextPos, ok := pos.ApplyMove(chosenMove)
			if !ok {
				break
			}
			pos = nextPos
		}
	}

	file, _ := json.MarshalIndent(testCases, "", "  ")
	_ = os.WriteFile("move_gen_test_data.json", file, 0644)
	fmt.Printf("Generated %d test cases from %d random games to move_gen_test_data.json\n", len(testCases), numGames)
}
