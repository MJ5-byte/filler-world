package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"filler-arena/internal/builder"
	"filler-arena/internal/config"
	"filler-arena/internal/elo"
	"filler-arena/internal/queue"
	"filler-arena/internal/runner"
)

type Worker struct {
	Cfg  config.Config
	Pool *pgxpool.Pool
	RDB  *redis.Client
}

// Run consumes jobs until ctx is cancelled. Job failures are recorded on the
// bot/match row, never fatal to the loop: broken user code is an expected state.
func (w *Worker) Run(ctx context.Context, id int) {
	log.Printf("worker %d started", id)
	for ctx.Err() == nil {
		job, ok, err := queue.Dequeue(ctx, w.RDB, 5*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("worker %d: dequeue: %v", id, err)
			time.Sleep(2 * time.Second)
			continue
		}
		if !ok {
			continue
		}
		w.process(ctx, id, job)
	}
}

// process dispatches one job, containing any panic so a single bad job can
// never take a worker goroutine down with it.
func (w *Worker) process(ctx context.Context, id int, job queue.Job) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("worker %d: PANIC on %+v: %v", id, job, r)
		}
	}()
	switch job.Type {
	case queue.JobBuild:
		w.handleBuild(ctx, job.BotID)
	case queue.JobMatch:
		w.handleMatch(ctx, job.MatchID)
	default:
		log.Printf("worker %d: unknown job type %q", id, job.Type)
	}
}

// Recover re-arms work that a previous worker process left behind: jobs stuck
// in 'building'/'running' (process died mid-job) and 'pending'/'queued' rows
// whose queue entries were popped but never completed. Duplicate queue
// entries are harmless because handleBuild/handleMatch claim atomically.
func (w *Worker) Recover(ctx context.Context) {
	if _, err := w.Pool.Exec(ctx,
		`UPDATE bots SET status='pending' WHERE status='building'`); err != nil {
		log.Printf("recover: reset building bots: %v", err)
	}
	if _, err := w.Pool.Exec(ctx,
		`UPDATE matches SET status='queued', started_at=NULL WHERE status='running'`); err != nil {
		log.Printf("recover: reset running matches: %v", err)
	}

	n := 0
	rows, err := w.Pool.Query(ctx, `SELECT id FROM bots WHERE status='pending'`)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			if queue.Enqueue(ctx, w.RDB, queue.Job{Type: queue.JobBuild, BotID: id}) == nil {
				n++
			}
		}
	}
	rows, err = w.Pool.Query(ctx, `SELECT id FROM matches WHERE status='queued'`)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			if queue.Enqueue(ctx, w.RDB, queue.Job{Type: queue.JobMatch, MatchID: id}) == nil {
				n++
			}
		}
	}
	if n > 0 {
		log.Printf("recover: re-enqueued %d unfinished jobs", n)
	}
}

func (w *Worker) handleBuild(ctx context.Context, botID int64) {
	// Atomic claim: only one worker may transition pending -> building, so a
	// duplicate queue entry is a no-op instead of a concurrent double build.
	var lang string
	err := w.Pool.QueryRow(ctx, `
		UPDATE bots SET status='building'
		WHERE id=$1 AND status='pending' RETURNING language`, botID).Scan(&lang)
	if errors.Is(err, pgx.ErrNoRows) {
		return // already claimed or no longer pending
	}
	if err != nil {
		log.Printf("build %d: claim: %v", botID, err)
		return
	}

	botDir := filepath.Join(w.Cfg.DataDir, "bots", fmt.Sprint(botID))
	buildLog, err := builder.Build(ctx, w.Cfg, botID, lang, botDir)
	if err != nil {
		full := fmt.Sprintf("%v\n%s", err, buildLog)
		log.Printf("build %d failed: %v", botID, err)
		if _, dberr := w.Pool.Exec(ctx,
			`UPDATE bots SET status='failed', build_log=$2 WHERE id=$1`, botID, full); dberr != nil {
			log.Printf("build %d: record failure: %v", botID, dberr)
		}
		return
	}

	_, err = w.Pool.Exec(ctx,
		`UPDATE bots SET status='active', build_log=$2 WHERE id=$1`, botID, buildLog)
	if err != nil {
		log.Printf("build %d: activate: %v", botID, err)
		return
	}
	if _, err := w.Pool.Exec(ctx,
		`INSERT INTO rankings (bot_id) VALUES ($1) ON CONFLICT (bot_id) DO NOTHING`, botID); err != nil {
		log.Printf("build %d: init ranking: %v", botID, err)
	}
	log.Printf("build %d: bot active", botID)

	if err := w.ScheduleRoundRobin(ctx, botID); err != nil {
		log.Printf("build %d: schedule matches: %v", botID, err)
	}
}

// ScheduleRoundRobin queues one placement match per other active bot,
// rotating through the maps so each map still gets coverage without the
// full opponents×maps explosion (which floods the queue when several
// people upload at once).
func (w *Worker) ScheduleRoundRobin(ctx context.Context, botID int64) error {
	rows, err := w.Pool.Query(ctx,
		`SELECT id FROM bots WHERE status='active' AND id <> $1 ORDER BY id`, botID)
	if err != nil {
		return err
	}
	var opponents []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		opponents = append(opponents, id)
	}
	rows.Close()

	// Placement runs on small/medium maps only: 100×100 games take 30s-5min
	// each and would stall the queue when several bots are uploaded together.
	// The big map is still exercised by explicit challenges and the periodic
	// rematch.
	var mapIDs []int64
	mrows, err := w.Pool.Query(ctx,
		`SELECT id FROM maps WHERE width * height <= 1200 ORDER BY id`)
	if err != nil {
		return err
	}
	for mrows.Next() {
		var id int64
		if err := mrows.Scan(&id); err == nil {
			mapIDs = append(mapIDs, id)
		}
	}
	mrows.Close()
	if len(mapIDs) == 0 {
		return nil
	}

	for i, opp := range opponents {
		if err := w.EnqueueMatch(ctx, botID, opp, mapIDs[i%len(mapIDs)]); err != nil {
			return err
		}
	}
	log.Printf("scheduled %d placement matches for bot %d", len(opponents), botID)
	return nil
}

func (w *Worker) EnqueueMatch(ctx context.Context, botA, botB, mapID int64) error {
	var matchID int64
	err := w.Pool.QueryRow(ctx, `
		INSERT INTO matches (bot_a_id, bot_b_id, map_id, status)
		VALUES ($1, $2, $3, 'queued') RETURNING id`, botA, botB, mapID).Scan(&matchID)
	if err != nil {
		return err
	}
	return queue.Enqueue(ctx, w.RDB, queue.Job{Type: queue.JobMatch, MatchID: matchID})
}

func (w *Worker) handleMatch(ctx context.Context, matchID int64) {
	// Atomic claim: queued -> running exactly once, even with duplicate
	// queue entries or a second worker process.
	ct, err := w.Pool.Exec(ctx, `
		UPDATE matches SET status='running', started_at=now()
		WHERE id=$1 AND status='queued'`, matchID)
	if err != nil {
		log.Printf("match %d: claim: %v", matchID, err)
		return
	}
	if ct.RowsAffected() == 0 {
		return // already claimed or no longer queued
	}

	var botAID, botBID int64
	var mapPath string
	var langA, langB, pathA, pathB string
	err = w.Pool.QueryRow(ctx, `
		SELECT ba.id, ba.language, ba.binary_path, bb.id, bb.language, bb.binary_path, m.path
		FROM matches mt
		JOIN bots ba ON ba.id = mt.bot_a_id
		JOIN bots bb ON bb.id = mt.bot_b_id
		JOIN maps m ON m.id = mt.map_id
		WHERE mt.id = $1`, matchID).
		Scan(&botAID, &langA, &pathA, &botBID, &langB, &pathB, &mapPath)
	if err != nil {
		log.Printf("match %d: load: %v", matchID, err)
		if _, dberr := w.Pool.Exec(ctx,
			`UPDATE matches SET status='error', error=$2, finished_at=now() WHERE id=$1`,
			matchID, "load match data: "+err.Error()); dberr != nil {
			log.Printf("match %d: record error: %v", matchID, dberr)
		}
		return
	}

	refA := runner.BotRef{ID: botAID, Builtin: langA == "builtin", Path: w.botPath(langA, pathA)}
	refB := runner.BotRef{ID: botBID, Builtin: langB == "builtin", Path: w.botPath(langB, pathB)}

	started := time.Now()
	out, err := runner.Run(ctx, w.Cfg, matchID, refA, refB, mapPath)
	if err != nil {
		log.Printf("match %d failed: %v", matchID, err)
		// Keep whatever turns were played before the failure — a partial
		// replay showing where a bot hung/crashed beats an empty page.
		if len(out.Result.Turns) > 0 {
			if terr := w.storeTurns(ctx, matchID, out.Result.Turns); terr != nil {
				log.Printf("match %d: store partial turns: %v", matchID, terr)
			}
		}
		if _, dberr := w.Pool.Exec(ctx,
			`UPDATE matches SET status='error', error=$2, finished_at=now() WHERE id=$1`,
			matchID, err.Error()); dberr != nil {
			log.Printf("match %d: record error: %v", matchID, dberr)
		}
		return
	}

	if err := w.persistResult(ctx, matchID, botAID, botBID, out.Result); err != nil {
		log.Printf("match %d: persist: %v", matchID, err)
		if _, dberr := w.Pool.Exec(ctx,
			`UPDATE matches SET status='error', error=$2, finished_at=now() WHERE id=$1`,
			matchID, "persist: "+err.Error()); dberr != nil {
			log.Printf("match %d: record error: %v", matchID, dberr)
		}
		return
	}
	log.Printf("match %d finished in %s: %d - %d (winner %d)",
		matchID, time.Since(started).Round(time.Millisecond),
		out.Result.ScoreA, out.Result.ScoreB, out.Result.Winner)
}

func (w *Worker) botPath(lang, binaryPath string) string {
	if lang == "builtin" {
		return binaryPath // in-image path
	}
	return binaryPath // host dir, copied into the container by the runner
}

// persistResult stores turns, final scores, and Elo/ranking updates in one
// transaction so a crash can't half-apply a match.
func (w *Worker) persistResult(ctx context.Context, matchID, botAID, botBID int64, res runner.Result) error {
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := copyTurns(ctx, tx, matchID, res.Turns); err != nil {
		return err
	}

	var winnerID *int64
	scoreA := 0.5
	switch res.Winner {
	case 1:
		winnerID, scoreA = &botAID, 1.0
	case 2:
		winnerID, scoreA = &botBID, 0.0
	}
	if _, err := tx.Exec(ctx, `
		UPDATE matches SET status='finished', winner_id=$2, score_a=$3, score_b=$4, finished_at=now()
		WHERE id=$1`, matchID, winnerID, res.ScoreA, res.ScoreB); err != nil {
		return fmt.Errorf("update match: %w", err)
	}

	// Serialize all rating updates globally (works across worker machines,
	// not just goroutines): Elo depends on both current ratings, so updates
	// must apply one at a time in a deterministic order.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(783401)`); err != nil {
		return fmt.Errorf("rating lock: %w", err)
	}
	// Lock both ranking rows in a stable order to avoid deadlocks between
	// concurrent workers.
	first, second := botAID, botBID
	if first > second {
		first, second = second, first
	}
	ratings := map[int64]float64{}
	for _, id := range []int64{first, second} {
		var r float64
		if err := tx.QueryRow(ctx,
			`SELECT rating FROM rankings WHERE bot_id=$1 FOR UPDATE`, id).Scan(&r); err != nil {
			return fmt.Errorf("lock ranking %d: %w", id, err)
		}
		ratings[id] = r
	}

	newA, newB := elo.Update(ratings[botAID], ratings[botBID], scoreA)
	winA, lossA, drawA := 0, 0, 0
	winB, lossB, drawB := 0, 0, 0
	switch res.Winner {
	case 1:
		winA, lossB = 1, 1
	case 2:
		winB, lossA = 1, 1
	default:
		drawA, drawB = 1, 1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE rankings SET rating=$2, wins=wins+$3, losses=losses+$4, draws=draws+$5,
		matches_played=matches_played+1 WHERE bot_id=$1`,
		botAID, newA, winA, lossA, drawA); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE rankings SET rating=$2, wins=wins+$3, losses=losses+$4, draws=draws+$5,
		matches_played=matches_played+1 WHERE bot_id=$1`,
		botBID, newB, winB, lossB, drawB); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// copyTurns replaces a match's stored turns. The delete makes retries and
// partial-then-final writes idempotent instead of a primary-key conflict.
func copyTurns(ctx context.Context, tx pgx.Tx, matchID int64, turns []runner.Turn) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM match_turns WHERE match_id=$1`, matchID); err != nil {
		return fmt.Errorf("clear turns: %w", err)
	}
	rows := make([][]any, len(turns))
	for i, t := range turns {
		rows[i] = []any{matchID, t.Number, t.Player, t.Anfield, t.Piece, t.MoveX, t.MoveY}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"match_turns"},
		[]string{"match_id", "turn_number", "player", "anfield", "piece", "move_x", "move_y"},
		pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("insert turns: %w", err)
	}
	return nil
}

// storeTurns writes turns outside the finish transaction (partial replays).
func (w *Worker) storeTurns(ctx context.Context, matchID int64, turns []runner.Turn) error {
	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := copyTurns(ctx, tx, matchID, turns); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Rematch periodically re-runs a round robin across all active bots so
// rankings stay fresh. One match per unordered pair on a rotating map.
func (w *Worker) Rematch(ctx context.Context) {
	if w.Cfg.RematchInterval <= 0 {
		return
	}
	ticker := time.NewTicker(w.Cfg.RematchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rows, err := w.Pool.Query(ctx, `SELECT id FROM bots WHERE status='active' ORDER BY id`)
		if err != nil {
			log.Printf("rematch: %v", err)
			continue
		}
		var bots []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				bots = append(bots, id)
			}
		}
		rows.Close()

		var mapIDs []int64
		mrows, err := w.Pool.Query(ctx, `SELECT id FROM maps ORDER BY id`)
		if err != nil {
			log.Printf("rematch: %v", err)
			continue
		}
		for mrows.Next() {
			var id int64
			if err := mrows.Scan(&id); err == nil {
				mapIDs = append(mapIDs, id)
			}
		}
		mrows.Close()
		if len(mapIDs) == 0 {
			continue
		}

		n := 0
		for i := 0; i < len(bots); i++ {
			for j := i + 1; j < len(bots); j++ {
				mapID := mapIDs[n%len(mapIDs)]
				if err := w.EnqueueMatch(ctx, bots[i], bots[j], mapID); err != nil {
					log.Printf("rematch: enqueue: %v", err)
				}
				n++
			}
		}
		log.Printf("rematch: queued %d matches", n)
	}
}
