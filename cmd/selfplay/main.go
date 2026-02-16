package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"
	"xionghan/internal/engine"
	"xionghan/internal/xionghan"
)

func main() {
	modelPath := flag.String("model", "xionghan.onnx", "path to ONNX model file")
	libPath := flag.String("lib", "onnxruntime.dll", "path to onnxruntime.dll")
	depth := flag.Int("depth", 4, "search depth")
	maxMoves := flag.Int("maxmoves", 20, "max moves to play")
	flag.Parse()

	// Start pprof for profiling
	go func() {
		log.Println("pprof listening on :6060")
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			log.Printf("pprof failed: %v", err)
		}
	}()

	e := engine.NewEngine()
	log.Printf("Initializing NN with model %s and lib %s", *modelPath, *libPath)
	if err := e.InitNN(*modelPath, *libPath); err != nil {
		log.Fatalf("Failed to initialize NN: %v", err)
	}

	pos := xionghan.NewInitialPosition()
	
	for i := 0; i < *maxMoves; i++ {
		log.Printf("--- Move %d, Side: %v ---", i+1, pos.SideToMove)
		
		start := time.Now()
		res := e.Search(pos, engine.SearchConfig{
			MaxDepth: *depth,
		})
		duration := time.Since(start)

		if res.BestMove.From == 0 && res.BestMove.To == 0 {
			log.Printf("Game over: no moves.")
			break
		}

		fmt.Printf("BestMove: %v, Score: %d, Nodes: %d, Time: %v, NPS: %d\n", 
			res.BestMove, res.Score, res.Nodes, duration, int64(float64(res.Nodes)/duration.Seconds()))

		newPos, ok := pos.ApplyMove(res.BestMove)
		if !ok {
			log.Fatalf("Failed to apply move %v", res.BestMove)
		}
		pos = newPos
		
		if !pos.KingExists(xionghan.Red) || !pos.KingExists(xionghan.Black) {
			log.Printf("Game over: king captured.")
			break
		}
	}
	
	log.Println("Selfplay finished.")
	os.Exit(0)
}
