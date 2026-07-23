package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"

	"filler-arena/internal/runner"
)

// auditGame is one entry in bot_audits.games: a single played game from the
// student's perspective.
type auditGame struct {
	Gate          string `json:"gate"`
	Opponent      string `json:"opponent"`
	Map           string `json:"map"`
	StudentSide   int    `json:"studentSide"`
	ScoreStudent  int    `json:"scoreStudent"`
	ScoreOpponent int    `json:"scoreOpponent"`
	Won           bool   `json:"won"`
}

// auditGate is one fixed rubric entry: N games of student vs a builtin bot on
// a given map. Required gates count toward automated_passed; the bonus gate
// is informational only.
type auditGate struct {
	name     string // "map00" | "map01" | "map02" | "bonus" — also the games[].gate value
	opponent string // builtin bot name
	mapName  string
	required int // minimum wins needed to pass this gate (0 for the bonus gate)
}

// This is a fixed grading rubric, not tunable: 5 games per gate, alternating
// which side the student plays, needing >=4 wins on each required gate.
var auditGates = []auditGate{
	{name: "map00", opponent: "wall_e", mapName: "map00", required: 4},
	{name: "map01", opponent: "h2_d2", mapName: "map01", required: 4},
	{name: "map02", opponent: "bender", mapName: "map02", required: 4},
	{name: "bonus", opponent: "terminator", mapName: "map02", required: 0},
}

const auditGamesPerGate = 5

// handleAudit runs the automated portion of a bot's review: it plays it
// against the fixed set of reference bots and records win/loss tallies plus
// a per-game log. If the automated gates pass, the bot moves to
// needs_review for a human to finish the manual rubric and accept/reject it;
// if they fail, the bot is auto-rejected — no human needs to look at a bot
// that can't clear the required win rate.
func (w *Worker) handleAudit(ctx context.Context, botID int64) {
	var name, language, binaryPath, status string
	err := w.Pool.QueryRow(ctx,
		`SELECT name, language, binary_path, status FROM bots WHERE id=$1`, botID).
		Scan(&name, &language, &binaryPath, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return // bot gone
	}
	if err != nil {
		log.Printf("audit %d: load bot: %v", botID, err)
		return
	}
	if status != "auditing" {
		return // already claimed / decided elsewhere, same as handleMatch's claim check
	}

	student := runner.BotRef{ID: botID, Builtin: false, Path: binaryPath}

	var games []auditGame
	var errMsgs []string
	tally := make(map[string][2]int, len(auditGates)) // gate name -> [wins, losses]

	for gi, gate := range auditGates {
		var mapPath string
		if err := w.Pool.QueryRow(ctx,
			`SELECT path FROM maps WHERE name=$1`, gate.mapName).Scan(&mapPath); err != nil {
			log.Printf("audit %d: load map %s: %v", botID, gate.mapName, err)
			errMsgs = append(errMsgs, fmt.Sprintf("load map %s: %v", gate.mapName, err))
			continue
		}
		var oppID int64
		var oppPath string
		if err := w.Pool.QueryRow(ctx,
			`SELECT id, binary_path FROM bots WHERE language='builtin' AND name=$1`, gate.opponent).
			Scan(&oppID, &oppPath); err != nil {
			log.Printf("audit %d: load opponent %s: %v", botID, gate.opponent, err)
			errMsgs = append(errMsgs, fmt.Sprintf("load opponent %s: %v", gate.opponent, err))
			continue
		}
		opponent := runner.BotRef{ID: oppID, Builtin: true, Path: oppPath}

		wins, losses := 0, 0
		for game := 1; game <= auditGamesPerGate; game++ {
			// Games 1,3,5: student is p1/botA. Games 2,4: student is p2/botB.
			studentSide := 1
			botA, botB := student, opponent
			if game%2 == 0 {
				studentSide = 2
				botA, botB = opponent, student
			}

			// Synthetic negative matchID: never collides with a real (positive
			// serial) matches.id, and is unique per bot/gate/game.
			matchID := -(botID*1000 + int64(gi)*10 + int64(game))

			out, runErr := runner.Run(ctx, w.Cfg, matchID, botA, botB, mapPath)
			scoreStudent, scoreOpponent := 0, 0
			won := false
			if runErr != nil {
				// Infra/timeout error, not the bot simply losing a fair game.
				// Count it as a loss and keep going: one flaky game shouldn't
				// sink an otherwise-working bot, though repeated infra errors
				// will tank the win count same as real losses would.
				log.Printf("audit %d: gate %s game %d: %v", botID, gate.name, game, runErr)
				errMsgs = append(errMsgs, fmt.Sprintf("%s game %d: %v", gate.name, game, runErr))
			} else {
				res := out.Result
				if studentSide == 1 {
					scoreStudent, scoreOpponent = res.ScoreA, res.ScoreB
					won = res.Winner == 1
				} else {
					scoreStudent, scoreOpponent = res.ScoreB, res.ScoreA
					won = res.Winner == 2
				}
			}
			if won {
				wins++
			} else {
				losses++
			}
			games = append(games, auditGame{
				Gate:          gate.name,
				Opponent:      gate.opponent,
				Map:           gate.mapName,
				StudentSide:   studentSide,
				ScoreStudent:  scoreStudent,
				ScoreOpponent: scoreOpponent,
				Won:           won,
			})
		}
		tally[gate.name] = [2]int{wins, losses}
		log.Printf("audit %d: gate %s vs %s: %d-%d", botID, gate.name, gate.opponent, wins, losses)
	}

	map00, map01, map02, bonus := tally["map00"], tally["map01"], tally["map02"], tally["bonus"]
	automatedPassed := map00[0] >= 4 && map01[0] >= 4 && map02[0] >= 4

	var automatedError *string
	if len(errMsgs) > 0 {
		s := strings.Join(errMsgs, "; ")
		automatedError = &s
	}

	gamesJSON, err := json.Marshal(games)
	if err != nil {
		// Shouldn't happen (auditGame is plain data), but don't lose the run
		// over a marshal failure — record NULL games rather than nothing.
		log.Printf("audit %d: marshal games: %v", botID, err)
		gamesJSON = nil
	}

	auditStatus := "rejected"
	if automatedPassed {
		auditStatus = "needs_review"
	}

	_, err = w.Pool.Exec(ctx, `
		INSERT INTO bot_audits (
			bot_id, status, automated_passed, automated_error,
			gate_map00_wins, gate_map00_losses,
			gate_map01_wins, gate_map01_losses,
			gate_map02_wins, gate_map02_losses,
			bonus_wins, bonus_losses, games, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now())
		ON CONFLICT (bot_id) DO UPDATE SET
			status=$2, automated_passed=$3, automated_error=$4,
			gate_map00_wins=$5, gate_map00_losses=$6,
			gate_map01_wins=$7, gate_map01_losses=$8,
			gate_map02_wins=$9, gate_map02_losses=$10,
			bonus_wins=$11, bonus_losses=$12, games=$13, updated_at=now()`,
		botID, auditStatus, automatedPassed, automatedError,
		map00[0], map00[1], map01[0], map01[1], map02[0], map02[1],
		bonus[0], bonus[1], gamesJSON,
	)
	if err != nil {
		log.Printf("audit %d: record result: %v", botID, err)
		return
	}

	if !automatedPassed {
		// Auto-reject: a bot that can't clear the required win-rate gates
		// doesn't need a human to look at it. If it did pass, bots.status
		// stays 'auditing' until a human accepts/rejects it via bot_audits.
		if _, err := w.Pool.Exec(ctx,
			`UPDATE bots SET status='rejected' WHERE id=$1 AND status='auditing'`, botID); err != nil {
			log.Printf("audit %d: reject bot: %v", botID, err)
		}
	}

	log.Printf("audit %d (%s): finished, passed=%v map00=%d-%d map01=%d-%d map02=%d-%d bonus=%d-%d",
		botID, name, automatedPassed,
		map00[0], map00[1], map01[0], map01[1], map02[0], map02[1], bonus[0], bonus[1])
}
