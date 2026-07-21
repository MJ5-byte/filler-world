package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"filler-arena/internal/queue"
)

// logAudit records an admin action or other notable platform event.
// Best-effort: a logging failure must never fail the action it describes.
func (s *Server) logAudit(ctx context.Context, actor, action, detail string) {
	if _, err := s.Pool.Exec(ctx,
		`INSERT INTO audit_log (actor_name, action, detail) VALUES ($1, $2, $3)`,
		actor, action, detail); err != nil {
		log.Printf("audit log write failed: %v", err)
	}
}

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
func (s *Server) adminRequeueMatch(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
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
	s.logAudit(r.Context(), actor.Login, "requeue_match", fmt.Sprintf("match #%d", id))
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "queued"})
}

// adminSetBotStatus toggles a bot between active and inactive.
func (s *Server) adminSetBotStatus(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
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
	s.logAudit(r.Context(), actor.Login, "set_bot_status", fmt.Sprintf("bot #%d -> %s", id, req.Status))
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": req.Status})
}

// adminDeleteBot removes a bot and everything it touched: its matches (turns
// cascade), its ranking (cascades from bots), and its files on disk.
func (s *Server) adminDeleteBot(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()

	var lang, name string
	err = s.Pool.QueryRow(ctx, `SELECT language, name FROM bots WHERE id=$1`, id).Scan(&lang, &name)
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
	s.logAudit(ctx, actor.Login, "delete_bot", fmt.Sprintf("bot #%d (%s)", id, name))
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ---- user management ----

type adminUserRow struct {
	ID        int64     `json:"id"`
	Login     string    `json:"login"`
	Email     *string   `json:"email"`
	IsAdmin   bool      `json:"isAdmin"`
	IsBlocked bool      `json:"isBlocked"`
	Bots      int       `json:"bots"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT u.id, u.name, u.email, u.is_admin, u.is_blocked, u.created_at,
		       count(b.id)::int
		FROM users u
		LEFT JOIN bots b ON b.owner_id = u.id
		GROUP BY u.id
		ORDER BY u.created_at DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	users := []adminUserRow{}
	for rows.Next() {
		var u adminUserRow
		if err := rows.Scan(&u.ID, &u.Login, &u.Email, &u.IsAdmin, &u.IsBlocked, &u.CreatedAt, &u.Bots); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, users)
}

// adminSetUserBlocked blocks or unblocks a user. Blocking kills every active
// session immediately, so it takes effect even if the user is mid-visit.
func (s *Server) adminSetUserBlocked(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Blocked bool `json:"blocked"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if id == actor.ID && req.Blocked {
		writeErr(w, http.StatusBadRequest, "you cannot block yourself")
		return
	}
	ctx := r.Context()
	var login string
	err = s.Pool.QueryRow(ctx, `UPDATE users SET is_blocked=$2 WHERE id=$1 RETURNING name`, id, req.Blocked).Scan(&login)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Blocked {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			writeErr(w, http.StatusInternalServerError, "revoke sessions: "+err.Error())
			return
		}
	}
	action := "unblock_user"
	if req.Blocked {
		action = "block_user"
	}
	s.logAudit(ctx, actor.Login, action, login)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "blocked": req.Blocked})
}

// adminSetUserAdmin grants or revokes admin access. An admin can't demote
// themselves — that would risk locking every admin out at once.
func (s *Server) adminSetUserAdmin(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		IsAdmin bool `json:"isAdmin"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if id == actor.ID && !req.IsAdmin {
		writeErr(w, http.StatusBadRequest, "you cannot revoke your own admin access")
		return
	}
	ctx := r.Context()
	var login string
	err = s.Pool.QueryRow(ctx, `UPDATE users SET is_admin=$2 WHERE id=$1 RETURNING name`, id, req.IsAdmin).Scan(&login)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "revoke_admin"
	if req.IsAdmin {
		action = "grant_admin"
	}
	s.logAudit(ctx, actor.Login, action, login)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "isAdmin": req.IsAdmin})
}

// ---- audit log ----

type auditEntryRow struct {
	ID        int64     `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Detail    *string   `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Server) adminAuditLog(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, actor_name, action, detail, created_at
		FROM audit_log ORDER BY id DESC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	entries := []auditEntryRow{}
	for rows.Next() {
		var e auditEntryRow
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Detail, &e.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		entries = append(entries, e)
	}
	writeJSON(w, http.StatusOK, entries)
}
