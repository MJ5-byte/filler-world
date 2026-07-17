package runner

import (
	"regexp"
	"strconv"
	"strings"
)

type Turn struct {
	Number  int    `json:"n"`
	Player  int    `json:"player"` // 1 or 2
	Anfield string `json:"anfield"`
	Piece   string `json:"piece"`
	MoveX   int    `json:"x"`
	MoveY   int    `json:"y"`
}

type Result struct {
	Turns   []Turn
	ScoreA  int
	ScoreB  int
	Winner  int // 0 = draw, 1, 2
	HasScores bool
}

var (
	anfieldRe = regexp.MustCompile(`^Anfield (\d+) (\d+):`)
	pieceRe   = regexp.MustCompile(`^Piece (\d+) (\d+):`)
	answerRe  = regexp.MustCompile(`^-> Answer \(([@$])\): (-?\d+) (-?\d+)`)
	scoreRe   = regexp.MustCompile(`^Player([12]) \(.*\): (\d+)\s*$`)
)

// Parse reconstructs per-half-turn replay data plus final scores from the
// game engine's verbose stdout. It is lenient: truncated or noisy output
// (crashed bots, engine errors) yields whatever turns were completed.
func Parse(output string) Result {
	lines := strings.Split(output, "\n")
	var res Result
	var anfield, piece string
	turn := 0

	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")

		if m := anfieldRe.FindStringSubmatch(line); m != nil {
			h, _ := strconv.Atoi(m[2])
			i++ // skip the column ruler line
			var rows []string
			for r := 0; r < h; r++ {
				i++
				if i >= len(lines) {
					break
				}
				row := strings.TrimRight(lines[i], "\r")
				// rows look like "007 $.$$$$..."; strip the numeric prefix
				if len(row) > 4 {
					rows = append(rows, row[4:])
				}
			}
			anfield = strings.Join(rows, "\n")
			continue
		}

		if m := pieceRe.FindStringSubmatch(line); m != nil {
			h, _ := strconv.Atoi(m[2])
			var rows []string
			for r := 0; r < h; r++ {
				i++
				if i >= len(lines) {
					break
				}
				rows = append(rows, strings.TrimRight(lines[i], "\r"))
			}
			piece = strings.Join(rows, "\n")
			continue
		}

		if m := answerRe.FindStringSubmatch(line); m != nil {
			player := 1
			if m[1] == "$" {
				player = 2
			}
			x, _ := strconv.Atoi(m[2])
			y, _ := strconv.Atoi(m[3])
			turn++
			res.Turns = append(res.Turns, Turn{
				Number:  turn,
				Player:  player,
				Anfield: anfield,
				Piece:   piece,
				MoveX:   x,
				MoveY:   y,
			})
			continue
		}

		if m := scoreRe.FindStringSubmatch(line); m != nil {
			score, _ := strconv.Atoi(m[2])
			if m[1] == "1" {
				res.ScoreA = score
			} else {
				res.ScoreB = score
			}
			res.HasScores = true
			continue
		}
	}

	if res.HasScores {
		switch {
		case res.ScoreA > res.ScoreB:
			res.Winner = 1
		case res.ScoreB > res.ScoreA:
			res.Winner = 2
		}
	}
	return res
}
