package engine

import (
	"testing"
	"xionghan/internal/xionghan"
)

func TestVCF_RedWin_BlackDefense(t *testing.T) {
	engine := NewEngine()

	// 同一局面，分别测试红先/黑先。
	// 你给的关键形状是中路炮线，红方有一手炮将军的 VCF 杀；
	// 黑方回合时重点是“识别红方下一步可能 VCF 绝杀”的风险。
	baseFEN := "i.a.h...h...i/...bcdedcb.../..........a../.....f.....f./..g.g.F.g.g../jF..........j/............./J...........J/..G.G.G.G.G../............./............./...BCDEDCB.../I.A.H...H.A.I"
	fenRedToMove := baseFEN + " w"
	fenBlackToMove := baseFEN + " b"

	// --- 测试点 1：轮到红方走棋，AI 必须识别出 VCF 连将赢，并选择炮那一步 ---
	t.Run("RedToMove_ShouldFindVCF", func(t *testing.T) {
		pos, _ := xionghan.DecodePosition(fenRedToMove)
		res := engine.VCFSearch(pos, 4)
		if !res.CanWin {
			t.Errorf("Red should have found a VCF win, but didn't")
		} else {
			// 验证走的是不是炮 (F)
			piece := pos.Board.Squares[res.Move.From]
			if piece.Type() != xionghan.PieceCannon {
				t.Errorf("Red VCF move should be Cannon (F), but got %v", piece.Type())
			}
			t.Logf("Red correctly found VCF move: From %d To %d (Piece: %v)", res.Move.From, res.Move.To, piece.Type())
		}
	})

	// --- 测试点 2：轮到黑方走棋，AI 必须检测到红方下一步能 VCF 绝杀自己 ---
	t.Run("BlackToMove_ShouldDetectThreat", func(t *testing.T) {
		pos, _ := xionghan.DecodePosition(fenBlackToMove)

		// 获取黑方所有合法走法
		moves := pos.GenerateLegalMoves(true)
		safeMoves := engine.FilterVCFMoves(pos, moves)

		// 重点断言：黑方必须能识别“红方下一步存在 VCF 绝杀风险”。
		// 不要求黑方必死，只要能识别到危险分支即可。
		threatCount := 0
		for _, mv := range moves {
			nextPos, _ := pos.ApplyMove(mv)
			vcf := engine.VCFSearch(nextPos, 4)
			if vcf.CanWin {
				threatCount++
			}
		}

		if threatCount == 0 {
			t.Fatalf("Black should detect at least one move that allows Red to VCF win next.")
		}

		// 如果存在安全步，FilterVCFMoves 应该能过滤掉部分危险步。
		if threatCount < len(moves) && len(safeMoves) == len(moves) {
			t.Fatalf("Threat exists but FilterVCFMoves did not filter any risky move.")
		}

		t.Logf("Detected threat lines: %d/%d, safe moves: %d", threatCount, len(moves), len(safeMoves))
	})
}
