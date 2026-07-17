package tournament

import (
	"reflect"
	"testing"
)

func TestSeedSlots(t *testing.T) {
	cases := map[int][]int{
		1: {1},
		2: {1, 2},
		4: {1, 4, 2, 3},
		8: {1, 8, 4, 5, 2, 7, 3, 6},
	}
	for size, want := range cases {
		if got := seedSlots(size); !reflect.DeepEqual(got, want) {
			t.Errorf("seedSlots(%d) = %v, want %v", size, got, want)
		}
	}
	// Top two seeds must land in opposite halves for every bracket size.
	for _, size := range []int{2, 4, 8, 16, 32} {
		slots := seedSlots(size)
		var pos1, pos2 int
		for i, s := range slots {
			if s == 1 {
				pos1 = i
			}
			if s == 2 {
				pos2 = i
			}
		}
		if (pos1 < size/2) == (pos2 < size/2) {
			t.Errorf("size %d: seeds 1 and 2 in the same half (%d, %d)", size, pos1, pos2)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestBracketWinner(t *testing.T) {
	seeds := map[int64]int{10: 1, 20: 2}
	if got := bracketWinner(MatchInfo{Status: "finished", WinnerID: ptr(int64(20))}, 10, 20, seeds); got != 20 {
		t.Errorf("finished match: got %d, want 20", got)
	}
	// Draw: better seed advances.
	if got := bracketWinner(MatchInfo{Status: "finished"}, 20, 10, seeds); got != 10 {
		t.Errorf("draw: got %d, want 10 (better seed)", got)
	}
	// Errored match: better seed advances.
	if got := bracketWinner(MatchInfo{Status: "error"}, 10, 20, seeds); got != 10 {
		t.Errorf("error: got %d, want 10 (better seed)", got)
	}
}

func TestStandings(t *testing.T) {
	seeds := map[int64]int{1: 1, 2: 2, 3: 3}
	matches := []MatchInfo{
		// 1 beats 2, 1 beats 3, 2 draws 3.
		{BotA: 1, BotB: 2, Status: "finished", WinnerID: ptr(int64(1)), ScoreA: ptr(50), ScoreB: ptr(30)},
		{BotA: 3, BotB: 1, Status: "finished", WinnerID: ptr(int64(1)), ScoreA: ptr(20), ScoreB: ptr(60)},
		{BotA: 2, BotB: 3, Status: "finished", ScoreA: ptr(40), ScoreB: ptr(40)},
		// Unfinished and errored matches must not count.
		{BotA: 2, BotB: 3, Status: "queued"},
		{BotA: 2, BotB: 3, Status: "error"},
	}
	st := Standings(seeds, matches)
	if len(st) != 3 {
		t.Fatalf("got %d standings, want 3", len(st))
	}
	if st[0].BotID != 1 || st[0].Points != 2 || st[0].Wins != 2 {
		t.Errorf("first place = %+v, want bot 1 with 2 points", st[0])
	}
	// 2 and 3 both have 0.5 points; 2 has the better score diff (-20+0 vs -40+0).
	if st[1].BotID != 2 || st[2].BotID != 3 {
		t.Errorf("tiebreak order = %d, %d; want 2, 3", st[1].BotID, st[2].BotID)
	}
	if st[1].Played != 2 || st[1].Draws != 1 {
		t.Errorf("bot 2 = %+v, want played 2, draws 1", st[1])
	}
}
