package main

import (
	"flag"
	"fmt"
	"log"
	"xionghan/internal/engine"
	"xionghan/internal/xionghan"
)

type PlayerConfig struct {
	Name string
	Cfg  engine.SearchConfig
}

func main() {
	modelPath := flag.String("model", "xionghan.onnx", "path to ONNX model file")
	libPath := flag.String("lib", "onnxruntime.dll", "path to onnxruntime.dll")
	totalGames := flag.Int("games", 10, "number of games to play")
	abDepth := flag.Int("ab-depth", 2, "Alpha-Beta search depth")
	mctsSims := flag.Int("mcts-sims", 2000, "MCTS simulation count")
	flag.Parse()

	e := engine.NewEngine()
	if err := e.InitNN(*modelPath, *libPath); err != nil {
		log.Fatalf("Failed to initialize NN: %v", err)
	}

	playerAB := PlayerConfig{
		Name: fmt.Sprintf("Alpha-Beta (Depth %d)", *abDepth),
		Cfg: engine.SearchConfig{
			MaxDepth: *abDepth,
			UseMCTS:  false,
		},
	}

	playerMCTS := PlayerConfig{
		Name: fmt.Sprintf("MCTS (%d Sims)", *mctsSims),
		Cfg: engine.SearchConfig{
			UseMCTS:         true,
			MCTSSimulations: *mctsSims,
		},
	}

	abWins := 0
	mctsWins := 0
	draws := 0

	for g := 0; g < *totalGames; g++ {
		var red, black PlayerConfig
		if g%2 == 0 {
			red, black = playerAB, playerMCTS
		} else {
			red, black = playerMCTS, playerAB
		}

		fmt.Printf("\n=== Game %d: Red [%s] vs Black [%s] ===\n", g+1, red.Name, black.Name)
		winner := playGame(e, red, black)
		
		switch winner {
		case "Red":
			if g%2 == 0 {
				abWins++
				fmt.Printf("Result: %s Wins!\n", playerAB.Name)
			} else {
				mctsWins++
				fmt.Printf("Result: %s Wins!\n", playerMCTS.Name)
			}
		case "Black":
			if g%2 == 0 {
				mctsWins++
				fmt.Printf("Result: %s Wins!\n", playerMCTS.Name)
			} else {
				abWins++
				fmt.Printf("Result: %s Wins!\n", playerAB.Name)
			}
		default:
			draws++
			fmt.Println("Result: Draw")
		}
	}

	fmt.Printf("\n=== Final Score ===\n")
	fmt.Printf("%s: %d\n", playerAB.Name, abWins)
	fmt.Printf("%s: %d\n", playerMCTS.Name, mctsWins)
	fmt.Printf("Draws: %d\n", draws)
}

func playGame(e *engine.Engine, red, black PlayerConfig) string {
	pos := xionghan.NewInitialPosition()
	maxMoves := 400 // 防止死循环
	
	for i := 0; i < maxMoves; i++ {
		var currentCfg engine.SearchConfig
		if pos.SideToMove == xionghan.Red {
			currentCfg = red.Cfg
		} else {
			currentCfg = black.Cfg
		}

		res := e.Search(pos, currentCfg)
		if res.BestMove.From == 0 && res.BestMove.To == 0 {
			// 无子可动，当前方输
			if pos.SideToMove == xionghan.Red {
				return "Black"
			}
			return "Red"
		}

		nextPos, ok := pos.ApplyMove(res.BestMove)
		if !ok {
			fmt.Printf("Error: invalid move %v\n", res.BestMove)
			return "Error"
		}
		pos = nextPos

		// 检查吃王
		if !pos.KingExists(xionghan.Red) {
			return "Black"
		}
		if !pos.KingExists(xionghan.Black) {
			return "Red"
		}
	}
	return "Draw"
}
