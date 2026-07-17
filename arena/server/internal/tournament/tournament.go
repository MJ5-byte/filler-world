// Package tournament creates and advances tournaments. Round-robin queues
// every pairing up front; single-elim brackets are re-derived from stored
// match rows on every advance, so the logic is idempotent and crash-safe:
// calling Advance at any time either creates the next due matches, finishes
// the tournament, or does nothing.
package tournament

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"filler-arena/internal/queue"
)

const (
	FormatRoundRobin = "round_robin"
	FormatSingleElim = "single_elim"

	// Round-robin is quadratic in matches (16 bots = 120 games); brackets are
	// linear so they can afford more entrants.
	MaxRoundRobin = 16
	MaxSingleElim = 32
)

// unknown marks a bracket entrant whose qualifying match hasn't finished;
// byeSlot marks an empty round-1 slot (top seeds skip round 1 in short fields).
const (
	unknown int64 = -1
	byeSlot int64 = 0
)

type Params struct {
	Name    string
	Format  string
	BotIDs  []int64 // empty = every active bot
	MapID   int64   // 0 = rotate through all maps
	Creator int64
}

// Create inserts the tournament, seeds participants by current rating, and
// queues the initial matches (all of them for round-robin, round 1 for
// single-elim). Returns the tournament id.
func Create(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, p Params) (int64, error) {
	if p.Format != FormatRoundRobin && p.Format != FormatSingleElim {
		return 0, fmt.Errorf("format must be %s or %s", FormatRoundRobin, FormatSingleElim)
	}

	bots, err := loadParticipants(ctx, pool, p.BotIDs)
	if err != nil {
		return 0, err
	}
	limit := MaxRoundRobin
	if p.Format == FormatSingleElim {
		limit = MaxSingleElim
	}
	if len(bots) < 2 {
		return 0, errors.New("a tournament needs at least 2 active bots")
	}
	if len(bots) > limit {
		return 0, fmt.Errorf("too many bots for %s (max %d, got %d)", p.Format, limit, len(bots))
	}

	mapIDs, err := loadMapIDs(ctx, pool, p.MapID)
	if err != nil {
		return 0, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var mapID *int64
	if p.MapID != 0 {
		mapID = &p.MapID
	}
	var tid int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO tournaments (name, format, map_id, created_by)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		p.Name, p.Format, mapID, p.Creator).Scan(&tid); err != nil {
		return 0, fmt.Errorf("insert tournament: %w", err)
	}
	for seed, botID := range bots {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tournament_bots (tournament_id, bot_id, seed)
			VALUES ($1, $2, $3)`, tid, botID, seed+1); err != nil {
			return 0, fmt.Errorf("insert participant: %w", err)
		}
	}

	var created []int64
	if p.Format == FormatRoundRobin {
		n := 0
		for i := 0; i < len(bots); i++ {
			for j := i + 1; j < len(bots); j++ {
				id, err := insertMatch(ctx, tx, tid, bots[i], bots[j], mapIDs[n%len(mapIDs)], nil, nil)
				if err != nil {
					return 0, err
				}
				created = append(created, id)
				n++
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	enqueue(ctx, rdb, created)

	if p.Format == FormatSingleElim {
		if err := Advance(ctx, pool, rdb, tid); err != nil {
			return tid, fmt.Errorf("schedule round 1: %w", err)
		}
	}
	return tid, nil
}

// Advance brings a running tournament up to date with its finished matches:
// creates whatever matches are now due, and finishes the tournament when a
// winner is decided. Safe to call concurrently and repeatedly — a per-
// tournament advisory lock serializes it, and it only acts on missing state.
func Advance(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, tid int64) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Two matches of the same tournament can finish on different workers at
	// once; only one advancer may derive the bracket at a time.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(783402, $1)`, int32(tid)); err != nil {
		return fmt.Errorf("tournament lock: %w", err)
	}

	var format, status string
	var mapID *int64
	err = tx.QueryRow(ctx, `
		SELECT format, status, map_id FROM tournaments WHERE id=$1`, tid).
		Scan(&format, &status, &mapID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // deleted meanwhile
	}
	if err != nil {
		return err
	}
	if status != "running" {
		return nil
	}

	seeds, order, err := loadSeeds(ctx, tx, tid)
	if err != nil {
		return err
	}
	if len(order) < 2 {
		// Participants were deleted from under us; nothing sensible remains.
		_, err := tx.Exec(ctx, `
			UPDATE tournaments SET status='error', error='participants removed', finished_at=now()
			WHERE id=$1`, tid)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	matches, err := loadMatches(ctx, tx, tid)
	if err != nil {
		return err
	}

	mapChoice := int64(0)
	if mapID != nil {
		mapChoice = *mapID
	}
	mapIDs, err := loadMapIDs(ctx, tx, mapChoice)
	if err != nil {
		return err
	}

	var created []int64
	switch format {
	case FormatRoundRobin:
		done := true
		for _, m := range matches {
			if !terminal(m.Status) {
				done = false
				break
			}
		}
		if !done || len(matches) == 0 {
			return nil
		}
		st := Standings(seeds, matches)
		if _, err := tx.Exec(ctx, `
			UPDATE tournaments SET status='finished', winner_id=$2, finished_at=now()
			WHERE id=$1`, tid, st[0].BotID); err != nil {
			return err
		}

	case FormatSingleElim:
		pick := 0
		byRoundSlot := map[[2]int]MatchInfo{}
		for _, m := range matches {
			if m.Round != nil && m.Slot != nil {
				byRoundSlot[[2]int{*m.Round, *m.Slot}] = m
			}
		}

		size := 1
		for size < len(order) {
			size *= 2
		}
		entrants := make([]int64, size)
		for i, seed := range seedSlots(size) {
			if seed <= len(order) {
				entrants[i] = order[seed-1]
			} else {
				entrants[i] = byeSlot
			}
		}

		for round := 1; len(entrants) > 1; round++ {
			next := make([]int64, len(entrants)/2)
			for slot := range next {
				a, b := entrants[2*slot], entrants[2*slot+1]
				switch {
				case a == unknown || b == unknown:
					next[slot] = unknown
				case a == byeSlot:
					next[slot] = b
				case b == byeSlot:
					next[slot] = a
				default:
					m, ok := byRoundSlot[[2]int{round, slot}]
					if !ok {
						r, s := round, slot
						id, err := insertMatch(ctx, tx, tid, a, b, mapIDs[pick%len(mapIDs)], &r, &s)
						if err != nil {
							return err
						}
						pick++
						created = append(created, id)
						next[slot] = unknown
					} else if terminal(m.Status) {
						next[slot] = bracketWinner(m, a, b, seeds)
					} else {
						next[slot] = unknown
					}
				}
			}
			entrants = next
		}

		if champion := entrants[0]; champion > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE tournaments SET status='finished', winner_id=$2, finished_at=now()
				WHERE id=$1`, tid, champion); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	enqueue(ctx, rdb, created)
	return nil
}

// bracketWinner resolves who advances from a terminal match. Filler has no
// tiebreak game, so a draw or an errored match advances the better seed.
func bracketWinner(m MatchInfo, a, b int64, seeds map[int64]int) int64 {
	if m.Status == "finished" && m.WinnerID != nil {
		return *m.WinnerID
	}
	if seeds[a] != 0 && (seeds[b] == 0 || seeds[a] < seeds[b]) {
		return a
	}
	return b
}

func terminal(status string) bool { return status == "finished" || status == "error" }

// seedSlots places 1-based seeds into a power-of-two bracket in standard
// order (1 vs size, 2 vs size-1, ... with re-pairing each round), so the top
// two seeds can only meet in the final.
func seedSlots(size int) []int {
	slots := []int{1}
	for len(slots) < size {
		n := len(slots) * 2
		next := make([]int, 0, n)
		for _, s := range slots {
			next = append(next, s, n+1-s)
		}
		slots = next
	}
	return slots
}

// MatchInfo is the slice of a match row that tournament logic needs.
type MatchInfo struct {
	ID       int64
	BotA     int64
	BotB     int64
	Status   string
	WinnerID *int64
	ScoreA   *int
	ScoreB   *int
	Round    *int
	Slot     *int
}

type Standing struct {
	BotID     int64   `json:"botId"`
	Seed      int     `json:"seed"`
	Wins      int     `json:"wins"`
	Losses    int     `json:"losses"`
	Draws     int     `json:"draws"`
	Played    int     `json:"played"`
	Points    float64 `json:"points"`
	ScoreDiff int     `json:"scoreDiff"`
}

// Standings ranks participants by points (win 1, draw ½), then total score
// difference, then seed. Only finished matches count.
func Standings(seeds map[int64]int, matches []MatchInfo) []Standing {
	rows := map[int64]*Standing{}
	for botID, seed := range seeds {
		rows[botID] = &Standing{BotID: botID, Seed: seed}
	}
	get := func(id int64) *Standing {
		if s, ok := rows[id]; ok {
			return s
		}
		s := &Standing{BotID: id}
		rows[id] = s
		return s
	}
	for _, m := range matches {
		if m.Status != "finished" {
			continue
		}
		a, b := get(m.BotA), get(m.BotB)
		a.Played++
		b.Played++
		if m.ScoreA != nil && m.ScoreB != nil {
			a.ScoreDiff += *m.ScoreA - *m.ScoreB
			b.ScoreDiff += *m.ScoreB - *m.ScoreA
		}
		switch {
		case m.WinnerID == nil:
			a.Draws++
			b.Draws++
			a.Points += 0.5
			b.Points += 0.5
		case *m.WinnerID == m.BotA:
			a.Wins++
			b.Losses++
			a.Points++
		default:
			b.Wins++
			a.Losses++
			b.Points++
		}
	}
	out := make([]Standing, 0, len(rows))
	for _, s := range rows {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Points != out[j].Points {
			return out[i].Points > out[j].Points
		}
		if out[i].ScoreDiff != out[j].ScoreDiff {
			return out[i].ScoreDiff > out[j].ScoreDiff
		}
		return out[i].Seed < out[j].Seed
	})
	return out
}

// ---- helpers ----

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadParticipants returns the chosen active bot ids ordered by rating desc
// (seed order). Empty ids = all active bots.
func loadParticipants(ctx context.Context, pool *pgxpool.Pool, ids []int64) ([]int64, error) {
	var rows pgx.Rows
	var err error
	base := `
		SELECT b.id, b.status FROM bots b
		LEFT JOIN rankings r ON r.bot_id = b.id`
	order := ` ORDER BY r.rating DESC NULLS LAST, b.id`
	if len(ids) == 0 {
		rows, err = pool.Query(ctx, base+` WHERE b.status='active'`+order)
	} else {
		rows, err = pool.Query(ctx, base+` WHERE b.id = ANY($1)`+order, ids)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		if status != "active" {
			return nil, fmt.Errorf("bot %d is not active (status: %s)", id, status)
		}
		out = append(out, id)
	}
	if len(ids) > 0 && len(out) != len(ids) {
		return nil, errors.New("one or more bots not found")
	}
	return out, nil
}

func loadMapIDs(ctx context.Context, q querier, mapID int64) ([]int64, error) {
	var rows pgx.Rows
	var err error
	if mapID != 0 {
		rows, err = q.Query(ctx, `SELECT id FROM maps WHERE id=$1`, mapID)
	} else {
		rows, err = q.Query(ctx, `SELECT id FROM maps ORDER BY id`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, errors.New("map not found (or no maps seeded)")
	}
	return out, nil
}

func loadSeeds(ctx context.Context, tx pgx.Tx, tid int64) (map[int64]int, []int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT bot_id, seed FROM tournament_bots WHERE tournament_id=$1 ORDER BY seed`, tid)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	seeds := map[int64]int{}
	var order []int64
	for rows.Next() {
		var botID int64
		var seed int
		if err := rows.Scan(&botID, &seed); err != nil {
			return nil, nil, err
		}
		seeds[botID] = seed
		order = append(order, botID)
	}
	return seeds, order, nil
}

func loadMatches(ctx context.Context, tx pgx.Tx, tid int64) ([]MatchInfo, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, bot_a_id, bot_b_id, status, winner_id, score_a, score_b,
		       tournament_round, tournament_slot
		FROM matches WHERE tournament_id=$1`, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchInfo
	for rows.Next() {
		var m MatchInfo
		if err := rows.Scan(&m.ID, &m.BotA, &m.BotB, &m.Status, &m.WinnerID,
			&m.ScoreA, &m.ScoreB, &m.Round, &m.Slot); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func insertMatch(ctx context.Context, tx pgx.Tx, tid, botA, botB, mapID int64, round, slot *int) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO matches (bot_a_id, bot_b_id, map_id, status, tournament_id, tournament_round, tournament_slot)
		VALUES ($1, $2, $3, 'queued', $4, $5, $6) RETURNING id`,
		botA, botB, mapID, tid, round, slot).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert tournament match: %w", err)
	}
	return id, nil
}

// enqueue pushes created matches onto the worker queue. A failure here is
// non-fatal: rows are already committed as 'queued', and the worker's startup
// recovery re-enqueues any queued match it finds.
func enqueue(ctx context.Context, rdb *redis.Client, matchIDs []int64) {
	for _, id := range matchIDs {
		if err := queue.Enqueue(ctx, rdb, queue.Job{Type: queue.JobMatch, MatchID: id}); err != nil {
			log.Printf("tournament: enqueue match %d: %v", id, err)
		}
	}
}
