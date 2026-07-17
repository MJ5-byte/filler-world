package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"filler-arena/internal/tournament"
)

type tournamentRow struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Format       string     `json:"format"`
	Status       string     `json:"status"`
	MapName      *string    `json:"mapName"` // nil = rotating maps
	CreatedBy    *string    `json:"createdBy"`
	WinnerID     *int64     `json:"winnerId"`
	WinnerName   *string    `json:"winnerName"`
	Error        *string    `json:"error"`
	Participants int        `json:"participants"`
	MatchesTotal int        `json:"matchesTotal"`
	MatchesDone  int        `json:"matchesDone"`
	Created      time.Time  `json:"createdAt"`
	Finished     *time.Time `json:"finishedAt"`
}

const tournamentSelect = `
	SELECT t.id, t.name, t.format, t.status, m.name, u.name, t.winner_id, wb.name,
	       t.error,
	       (SELECT count(*)::int FROM tournament_bots tb WHERE tb.tournament_id = t.id),
	       (SELECT count(*)::int FROM matches mt WHERE mt.tournament_id = t.id),
	       (SELECT count(*)::int FROM matches mt WHERE mt.tournament_id = t.id
	           AND mt.status IN ('finished','error')),
	       t.created_at, t.finished_at
	FROM tournaments t
	LEFT JOIN maps m ON m.id = t.map_id
	LEFT JOIN users u ON u.id = t.created_by
	LEFT JOIN bots wb ON wb.id = t.winner_id`

func scanTournament(row pgx.Row) (tournamentRow, error) {
	var t tournamentRow
	err := row.Scan(&t.ID, &t.Name, &t.Format, &t.Status, &t.MapName, &t.CreatedBy,
		&t.WinnerID, &t.WinnerName, &t.Error, &t.Participants, &t.MatchesTotal,
		&t.MatchesDone, &t.Created, &t.Finished)
	return t, err
}

func (s *Server) createTournament(w http.ResponseWriter, r *http.Request, u *AuthedUser) {
	var req struct {
		Name   string  `json:"name"`
		Format string  `json:"format"`
		BotIDs []int64 `json:"botIds"`
		MapID  int64   `json:"mapId"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := len(req.Name); n < 2 || n > 60 {
		writeErr(w, http.StatusBadRequest, "tournament name must be 2-60 characters")
		return
	}

	tid, err := tournament.Create(r.Context(), s.Pool, s.RDB, tournament.Params{
		Name:    req.Name,
		Format:  req.Format,
		BotIDs:  req.BotIDs,
		MapID:   req.MapID,
		Creator: u.ID,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": tid, "status": "running"})
}

func (s *Server) listTournaments(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), tournamentSelect+` ORDER BY t.id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []tournamentRow{}
	for rows.Next() {
		t, err := scanTournament(rows)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getTournament(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()

	t, err := scanTournament(s.Pool.QueryRow(ctx, tournamentSelect+` WHERE t.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "tournament not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	type participant struct {
		BotID    int64    `json:"botId"`
		Name     string   `json:"name"`
		Owner    string   `json:"owner"`
		Language string   `json:"language"`
		Seed     int      `json:"seed"`
		Rating   *float64 `json:"rating"`
	}
	prows, err := s.Pool.Query(ctx, `
		SELECT tb.bot_id, b.name, COALESCE(u.name, ''), b.language, tb.seed, r.rating
		FROM tournament_bots tb
		JOIN bots b ON b.id = tb.bot_id
		LEFT JOIN users u ON u.id = b.owner_id
		LEFT JOIN rankings r ON r.bot_id = b.id
		WHERE tb.tournament_id=$1 ORDER BY tb.seed`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer prows.Close()
	participants := []participant{}
	seeds := map[int64]int{}
	for prows.Next() {
		var p participant
		if err := prows.Scan(&p.BotID, &p.Name, &p.Owner, &p.Language, &p.Seed, &p.Rating); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		participants = append(participants, p)
		seeds[p.BotID] = p.Seed
	}

	matches, err := s.queryMatches(ctx, `
		WHERE mt.tournament_id=$1
		ORDER BY mt.tournament_round NULLS FIRST, mt.tournament_slot, mt.id`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Standings power the round-robin table; they update live as matches land.
	var standings []tournament.Standing
	if t.Format == tournament.FormatRoundRobin {
		infos := make([]tournament.MatchInfo, len(matches))
		for i, m := range matches {
			infos[i] = tournament.MatchInfo{
				ID: m.ID, BotA: m.BotAID, BotB: m.BotBID, Status: m.Status,
				WinnerID: m.WinnerID, ScoreA: m.ScoreA, ScoreB: m.ScoreB,
			}
		}
		standings = tournament.Standings(seeds, infos)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tournament":   t,
		"participants": participants,
		"matches":      matches,
		"standings":    standings,
	})
}
