package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- bot audit review ----

const maxAuditSourceBytes = 200 * 1024 // 200KB

type auditGate struct {
	Wins     int    `json:"wins"`
	Losses   int    `json:"losses"`
	Opponent string `json:"opponent"`
}

type auditListRow struct {
	BotID           int64              `json:"botId"`
	BotName         string             `json:"botName"`
	Owner           string             `json:"owner"`
	Language        string             `json:"language"`
	AuditStatus     string             `json:"auditStatus"`
	AutomatedPassed *bool              `json:"automatedPassed"`
	Gates           map[string]auditGate `json:"gates"`
	CreatedAt       *time.Time         `json:"createdAt"`
	UpdatedAt       *time.Time         `json:"updatedAt"`
}

const auditListSelect = `
	SELECT b.id, b.name, COALESCE(u.name, ''), b.language,
	       ba.status, ba.automated_passed,
	       ba.gate_map00_wins, ba.gate_map00_losses,
	       ba.gate_map01_wins, ba.gate_map01_losses,
	       ba.gate_map02_wins, ba.gate_map02_losses,
	       ba.bonus_wins, ba.bonus_losses,
	       ba.created_at, ba.updated_at
	FROM bots b
	LEFT JOIN users u ON u.id = b.owner_id
	LEFT JOIN bot_audits ba ON ba.bot_id = b.id`

// scanAuditListRow scans one row of auditListSelect. bot_audits fields are
// all nullable: the worker may not have finished the first audit pass yet,
// which is a normal transient state, not an error.
func scanAuditListRow(row pgx.Row) (auditListRow, error) {
	var a auditListRow
	var status *string
	var g00w, g00l, g01w, g01l, g02w, g02l, bw, bl *int
	var createdAt, updatedAt *time.Time
	err := row.Scan(&a.BotID, &a.BotName, &a.Owner, &a.Language,
		&status, &a.AutomatedPassed,
		&g00w, &g00l, &g01w, &g01l, &g02w, &g02l, &bw, &bl,
		&createdAt, &updatedAt)
	if err != nil {
		return a, err
	}
	if status != nil {
		a.AuditStatus = *status
	} else {
		a.AuditStatus = "running"
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	a.Gates = map[string]auditGate{
		"map00": {intOr0(g00w), intOr0(g00l), "wall_e"},
		"map01": {intOr0(g01w), intOr0(g01l), "h2_d2"},
		"map02": {intOr0(g02w), intOr0(g02l), "bender"},
		"bonus": {intOr0(bw), intOr0(bl), "terminator"},
	}
	return a, nil
}

func intOr0(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// adminListAudits lists bots the owner has explicitly submitted for review
// (bot_audits.status='needs_review'). A bot that's still running its
// automated gates, or that passed but hasn't been submitted yet, doesn't
// show up here — the admin only sees what owners actually send them.
func (s *Server) adminListAudits(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	rows, err := s.Pool.Query(r.Context(), auditListSelect+`
		WHERE ba.status='needs_review'
		ORDER BY b.id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []auditListRow{}
	for rows.Next() {
		a, err := scanAuditListRow(rows)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, out)
}

// adminGetAudit returns full detail for one bot's audit, including the raw
// games JSON, build log, and (best-effort) the uploaded source. Admins can
// look up any bot's audit directly regardless of status (e.g. mid-run, or
// still awaiting the owner's submission) — only the list view is restricted
// to submitted ones.
func (s *Server) adminGetAudit(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	botID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()

	a, err := scanAuditListRow(s.Pool.QueryRow(ctx, auditListSelect+` WHERE b.id=$1`, botID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "bot not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var games []byte
	var notes, reviewer, automatedError *string
	var decidedAt *time.Time
	err = s.Pool.QueryRow(ctx, `
		SELECT games, notes, reviewer, automated_error, decided_at
		FROM bot_audits WHERE bot_id=$1`, botID).
		Scan(&games, &notes, &reviewer, &automatedError, &decidedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var buildLog *string
	_ = s.Pool.QueryRow(ctx, `SELECT build_log FROM bots WHERE id=$1`, botID).Scan(&buildLog)

	var gamesJSON any
	if len(games) > 0 {
		_ = json.Unmarshal(games, &gamesJSON)
	}

	var source *string
	if a.Language != "binary" && a.Language != "builtin" {
		path := filepath.Join(s.Cfg.DataDir, "bots", fmt.Sprint(botID), "src", "upload")
		if data, err := os.ReadFile(path); err == nil {
			text := string(data)
			if len(data) > maxAuditSourceBytes {
				text = string(data[:maxAuditSourceBytes]) + "\n... (truncated)"
			}
			source = &text
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"botId":           a.BotID,
		"botName":         a.BotName,
		"owner":           a.Owner,
		"language":        a.Language,
		"auditStatus":     a.AuditStatus,
		"automatedPassed": a.AutomatedPassed,
		"gates":           a.Gates,
		"createdAt":       a.CreatedAt,
		"updatedAt":       a.UpdatedAt,
		"games":           gamesJSON,
		"notes":           notes,
		"reviewer":        reviewer,
		"decidedAt":       decidedAt,
		"automatedError":  automatedError,
		"buildLog":        buildLog,
		"source":          source,
	})
}

// adminDecideAudit accepts or rejects a bot that's awaiting human review.
// Accepting activates the bot and schedules placement matches against the
// current active roster; rejecting just closes out the audit.
func (s *Server) adminDecideAudit(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
	botID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Notes    string `json:"notes"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Decision != "accept" && req.Decision != "reject" {
		writeErr(w, http.StatusBadRequest, "decision must be accept or reject")
		return
	}
	ctx := r.Context()

	var auditStatus string
	err = s.Pool.QueryRow(ctx, `SELECT status FROM bot_audits WHERE bot_id=$1`, botID).Scan(&auditStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "audit not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if auditStatus != "needs_review" {
		writeErr(w, http.StatusBadRequest, "audit is not awaiting review")
		return
	}

	var botName string
	if err := s.Pool.QueryRow(ctx, `SELECT name FROM bots WHERE id=$1`, botID).Scan(&botName); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var newBotStatus, newAuditStatus, action string
	if req.Decision == "accept" {
		newBotStatus, newAuditStatus, action = "active", "accepted", "accept_bot_audit"
	} else {
		newBotStatus, newAuditStatus, action = "rejected", "rejected", "reject_bot_audit"
	}

	ct, err := s.Pool.Exec(ctx,
		`UPDATE bots SET status=$2 WHERE id=$1 AND status='auditing'`, botID, newBotStatus)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusBadRequest, "bot is not awaiting audit (already decided or moved on)")
		return
	}

	if req.Decision == "accept" {
		// Accepting only makes the bot eligible: it joins the leaderboard at
		// the default rating with zero games played. No matches are
		// auto-scheduled — the owner has to actively challenge other bots to
		// start building a record, same as any other active bot.
		if _, err := s.Pool.Exec(ctx,
			`INSERT INTO rankings (bot_id) VALUES ($1) ON CONFLICT (bot_id) DO NOTHING`, botID); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if _, err := s.Pool.Exec(ctx, `
		UPDATE bot_audits SET status=$2, reviewer=$3, notes=$4, decided_at=now(), updated_at=now()
		WHERE bot_id=$1`, botID, newAuditStatus, actor.Login, req.Notes); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logAudit(ctx, actor.Login, action, fmt.Sprintf("bot #%d (%s)", botID, botName))
	writeJSON(w, http.StatusOK, map[string]any{
		"botId":     botID,
		"decision":  req.Decision,
		"botStatus": newBotStatus,
	})
}
