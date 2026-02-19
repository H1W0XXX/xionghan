package xionghan

import (
	"strings"
	"testing"
)

func TestHashInitializedFromInitialAndFEN(t *testing.T) {
	pos := NewInitialPosition()
	if pos.Hash != pos.CalculateHash() {
		t.Fatalf("initial hash mismatch: got=%d want=%d", pos.Hash, pos.CalculateHash())
	}

	fen := strings.ReplaceAll(initialBoardString, "\n", "/") + " w"
	decoded, err := DecodePosition(fen)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Hash != decoded.CalculateHash() {
		t.Fatalf("decoded hash mismatch: got=%d want=%d", decoded.Hash, decoded.CalculateHash())
	}
}

func TestApplyMoveHashIncrementalMatchesFullRecompute(t *testing.T) {
	pos := NewInitialPosition()
	for ply := 0; ply < 24; ply++ {
		moves := pos.GenerateLegalMoves(true)
		if len(moves) == 0 {
			return
		}
		mv := moves[len(moves)/2]
		next, ok := pos.ApplyMove(mv)
		if !ok {
			t.Fatalf("apply move failed at ply %d: %+v", ply, mv)
		}
		got := next.Hash
		want := next.CalculateHash()
		if got != want {
			t.Fatalf("hash mismatch at ply %d: got=%d want=%d move=%+v", ply, got, want, mv)
		}
		pos = next
	}
}
