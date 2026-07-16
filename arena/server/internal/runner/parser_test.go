package runner

import (
	"os"
	"strings"
	"testing"
)

// Set ARENA_TEST_ENGINE_OUTPUT to a file with real engine output to run.
func TestParseRealOutput(t *testing.T) {
	path := os.Getenv("ARENA_TEST_ENGINE_OUTPUT")
	if path == "" {
		t.Skip("no ARENA_TEST_ENGINE_OUTPUT set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res := Parse(string(data))
	if !res.HasScores {
		t.Fatal("no scores parsed")
	}
	if len(res.Turns) == 0 {
		t.Fatal("no turns parsed")
	}
	first := res.Turns[0]
	rows := strings.Split(first.Anfield, "\n")
	t.Logf("turns=%d scoreA=%d scoreB=%d winner=%d grid=%dx%d firstPiece=%q",
		len(res.Turns), res.ScoreA, res.ScoreB, res.Winner, len(rows[0]), len(rows), first.Piece)
	for i, r := range rows {
		if len(r) != len(rows[0]) {
			t.Fatalf("ragged grid row %d: %d vs %d", i, len(r), len(rows[0]))
		}
	}
	for _, tn := range res.Turns {
		if tn.Player != 1 && tn.Player != 2 {
			t.Fatalf("bad player %d", tn.Player)
		}
	}
}
