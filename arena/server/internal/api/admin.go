package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jackc/pgx/v5"

	"filler-arena/internal/queue"
)

type adminOverview struct {
	QueueBuilds    int64          `json:"queueBuilds"`
	QueueMatches   int64          `json:"queueMatches"`
	Bots           map[string]int `json:"bots"`
	Matches        map[string]int `json:"matches"`
	Finished24h    int            `json:"finished24h"`
	AvgDurationSec *float64       `json:"avgDurationSec"` // finished, last 24h
	Players        int            `json:"players"`
}

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	ctx := r.Context()
	ov := adminOverview{Bots: map[string]int{}, Matches: map[string]int{}}

	var err error
	ov.QueueBuilds, ov.QueueMatches, err = queue.Depths(ctx, s.RDB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "queue depths: "+err.Error())
		return
	}

	rows, err := s.Pool.Query(ctx, `SELECT status, count(*)::int FROM bots GROUP BY status`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var st string
		var n int
		if rows.Scan(&st, &n) == nil {
			ov.Bots[st] = n
		}
	}
	rows.Close()

	rows, err = s.Pool.Query(ctx, `SELECT status, count(*)::int FROM matches GROUP BY status`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var st string
		var n int
		if rows.Scan(&st, &n) == nil {
			ov.Matches[st] = n
		}
	}
	rows.Close()

	err = s.Pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       AVG(EXTRACT(EPOCH FROM finished_at - started_at))
		FROM matches
		WHERE status='finished' AND finished_at > now() - interval '24 hours'`).
		Scan(&ov.Finished24h, &ov.AvgDurationSec)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*)::int FROM users`).Scan(&ov.Players); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// adminRequeueMatch resets an errored match and puts it back on the queue.
func (s *Server) adminRequeueMatch(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ct, err := s.Pool.Exec(r.Context(), `
		UPDATE matches SET status='queued', error=NULL, started_at=NULL, finished_at=NULL
		WHERE id=$1 AND status='error'`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusBadRequest, "match is not in error state")
		return
	}
	if err := queue.Enqueue(r.Context(), s.RDB, queue.Job{Type: queue.JobMatch, MatchID: id}); err != nil {
		writeErr(w, http.StatusInternalServerError, "enqueue: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "queued"})
}

// adminSetBotStatus toggles a bot between active and inactive.
func (s *Server) adminSetBotStatus(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Status != "active" && req.Status != "inactive" {
		writeErr(w, http.StatusBadRequest, "status must be active or inactive")
		return
	}
	ct, err := s.Pool.Exec(r.Context(), `
		UPDATE bots SET status=$2
		WHERE id=$1 AND status IN ('active','inactive')`, id, req.Status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusBadRequest, "bot not found or not toggleable (builds in progress / failed bots can't be activated)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": req.Status})
}

// adminDeleteBot removes a bot and everything it touched: its matches (turns
// cascade), its ranking (cascades from bots), and its files on disk.
func (s *Server) adminDeleteBot(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()

	var lang string
	err = s.Pool.QueryRow(ctx, `SELECT language FROM bots WHERE id=$1`, id).Scan(&lang)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "bot not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if lang == "builtin" {
		writeErr(w, http.StatusBadRequest, "reference robots cannot be deleted")
		return
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`DELETE FROM matches WHERE bot_a_id=$1 OR bot_b_id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete matches: "+err.Error())
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM bots WHERE id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete bot: "+err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Best-effort cleanup outside the transaction.
	_ = os.RemoveAll(filepath.Join(s.Cfg.DataDir, "bots", fmt.Sprint(id)))
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}
