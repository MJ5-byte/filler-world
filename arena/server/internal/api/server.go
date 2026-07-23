package api

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"filler-arena/internal/config"
	"filler-arena/internal/queue"
)

const maxUploadBytes = 32 << 20 // 32 MiB

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]{2,40}$`)

var validLangs = map[string]bool{"binary": true, "rust": true}

type Server struct {
	Cfg  config.Config
	Pool *pgxpool.Pool
	RDB  *redis.Client

	uploadLimit  *limiter
	matchLimit   *limiter
	loginLimit   *limiter
	tourneyLimit *limiter
}

func New(cfg config.Config, pool *pgxpool.Pool, rdb *redis.Client) *Server {
	return &Server{
		Cfg:         cfg,
		Pool:        pool,
		RDB:         rdb,
		uploadLimit: newLimiter(10, 10*time.Minute),
		matchLimit:  newLimiter(60, 10*time.Minute),
		loginLimit:  newLimiter(15, 10*time.Minute),
		// A tournament fans out into up to 120 matches, so keep creation rare.
		tourneyLimit: newLimiter(5, 10*time.Minute),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.loginLimit.middleware(s.login))
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("POST /api/bots", s.uploadLimit.middleware(s.requireUser(s.createBot)))
	mux.HandleFunc("GET /api/bots", s.listBots)
	mux.HandleFunc("GET /api/bots/{id}", s.getBot)
	mux.HandleFunc("POST /api/matches", s.matchLimit.middleware(s.requireUser(s.createMatch)))
	mux.HandleFunc("GET /api/matches", s.listMatches)
	mux.HandleFunc("GET /api/matches/{id}", s.getMatch)
	mux.HandleFunc("GET /api/matches/{id}/replay", s.getReplay)
	mux.HandleFunc("POST /api/tournaments", s.tourneyLimit.middleware(s.requireUser(s.createTournament)))
	mux.HandleFunc("GET /api/tournaments", s.listTournaments)
	mux.HandleFunc("GET /api/tournaments/{id}", s.getTournament)
	mux.HandleFunc("GET /api/leaderboard", s.leaderboard)
	mux.HandleFunc("GET /api/players", s.listPlayers)
	mux.HandleFunc("GET /api/players/{name}", s.getPlayer)
	mux.HandleFunc("GET /api/players/{name}/stats", s.playerStatsHandler)
	mux.HandleFunc("GET /api/maps", s.listMaps)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/admin/overview", s.requireAdmin(s.adminOverview))
	mux.HandleFunc("POST /api/admin/matches/{id}/requeue", s.requireAdmin(s.adminRequeueMatch))
	mux.HandleFunc("POST /api/admin/bots/{id}/status", s.requireAdmin(s.adminSetBotStatus))
	mux.HandleFunc("DELETE /api/admin/bots/{id}", s.requireAdmin(s.adminDeleteBot))
	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.adminListUsers))
	mux.HandleFunc("POST /api/admin/users/{id}/block", s.requireAdmin(s.adminSetUserBlocked))
	mux.HandleFunc("POST /api/admin/users/{id}/admin", s.requireAdmin(s.adminSetUserAdmin))
	mux.HandleFunc("GET /api/admin/audit-log", s.requireAdmin(s.adminAuditLog))
	mux.HandleFunc("GET /api/admin/audits", s.requireAdmin(s.adminListAudits))
	mux.HandleFunc("GET /api/admin/audits/{id}", s.requireAdmin(s.adminGetAudit))
	mux.HandleFunc("POST /api/admin/audits/{id}/checklist", s.requireAdmin(s.adminSaveAuditChecklist))
	mux.HandleFunc("POST /api/admin/audits/{id}/decide", s.requireAdmin(s.adminDecideAudit))
	mux.HandleFunc("GET /api/admin/db/tables", s.requireAdmin(s.adminDBTables))
	mux.HandleFunc("GET /api/admin/db/tables/{table}", s.requireAdmin(s.adminDBTableRows))
	mux.HandleFunc("POST /api/admin/db/query", s.requireAdmin(s.adminDBQuery))
	mux.HandleFunc("/", s.static)
	return gzipAPI(mux)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	return nil
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

// gzipAPI compresses API responses when the client accepts it. Replays for
// the 100×100 map are ~20 MB of highly repetitive JSON — they gzip ~30:1.
// Static files are left alone so ServeFile's range/etag handling keeps working.
func gzipAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ---- bots ----

func (s *Server) createBot(w http.ResponseWriter, r *http.Request, u *AuthedUser) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid multipart form (max 32MB): "+err.Error())
		return
	}
	name := r.FormValue("name")
	lang := r.FormValue("language")
	if !nameRe.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "bot name must be 2-40 chars of [a-zA-Z0-9_-]")
		return
	}
	if !validLangs[lang] {
		writeErr(w, http.StatusBadRequest, "language must be one of: binary, rust")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "empty or unreadable file")
		return
	}
	hash := sha256.Sum256(data)

	ctx := r.Context()
	ownerID := u.ID

	var botID int64
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO bots (owner_id, name, language, binary_path, binary_hash, status)
		VALUES ($1, $2, $3, '', $4, 'pending') RETURNING id`,
		ownerID, name, lang, hex.EncodeToString(hash[:])).Scan(&botID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, fmt.Sprintf("a bot named %q already exists — pick another name", name))
			return
		}
		writeErr(w, http.StatusInternalServerError, "create bot: "+err.Error())
		return
	}

	// From here on, failure must not leave a half-created bot behind.
	fail := func(status int, msg string) {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM bots WHERE id=$1 AND status='pending'`, botID)
		writeErr(w, status, msg)
	}

	botDir := filepath.Join(s.Cfg.DataDir, "bots", fmt.Sprint(botID))
	srcDir := filepath.Join(botDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		fail(http.StatusInternalServerError, "store upload: "+err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(srcDir, "upload"), data, 0o644); err != nil {
		fail(http.StatusInternalServerError, "store upload: "+err.Error())
		return
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE bots SET binary_path=$2 WHERE id=$1`, botID, botDir); err != nil {
		fail(http.StatusInternalServerError, "update bot: "+err.Error())
		return
	}
	if err := queue.Enqueue(ctx, s.RDB, queue.Job{Type: queue.JobBuild, BotID: botID}); err != nil {
		fail(http.StatusInternalServerError, "enqueue build: "+err.Error())
		return
	}
	s.logAudit(ctx, u.Login, "upload_bot", fmt.Sprintf("bot #%d %q (%s)", botID, name, lang))
	writeJSON(w, http.StatusCreated, map[string]any{"id": botID, "status": "pending"})
}

type botRow struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Owner    string    `json:"owner"`
	Language string    `json:"language"`
	Status   string    `json:"status"`
	Created  time.Time `json:"createdAt"`
	Rating   *float64  `json:"rating"`
	Wins     *int      `json:"wins"`
	Losses   *int      `json:"losses"`
	Draws    *int      `json:"draws"`
	Played   *int      `json:"matchesPlayed"`
}

const botSelect = `
	SELECT b.id, b.name, COALESCE(u.name, ''), b.language, b.status, b.created_at,
	       r.rating, r.wins, r.losses, r.draws, r.matches_played
	FROM bots b
	LEFT JOIN users u ON u.id = b.owner_id
	LEFT JOIN rankings r ON r.bot_id = b.id`

func scanBot(row pgx.Row) (botRow, error) {
	var b botRow
	err := row.Scan(&b.ID, &b.Name, &b.Owner, &b.Language, &b.Status, &b.Created,
		&b.Rating, &b.Wins, &b.Losses, &b.Draws, &b.Played)
	return b, err
}

func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), botSelect+` ORDER BY b.id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	bots := []botRow{}
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		bots = append(bots, b)
	}
	writeJSON(w, http.StatusOK, bots)
}

func (s *Server) getBot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	b, err := scanBot(s.Pool.QueryRow(r.Context(), botSelect+` WHERE b.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "bot not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var buildLog *string
	_ = s.Pool.QueryRow(r.Context(), `SELECT build_log FROM bots WHERE id=$1`, id).Scan(&buildLog)

	matches, err := s.queryMatches(r.Context(),
		`WHERE mt.bot_a_id=$1 OR mt.bot_b_id=$1 ORDER BY mt.id DESC LIMIT 50`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": b, "buildLog": buildLog, "matches": matches})
}

// ---- matches ----

type matchRow struct {
	ID           int64      `json:"id"`
	BotAID       int64      `json:"botAId"`
	BotBID       int64      `json:"botBId"`
	BotAName     string     `json:"botAName"`
	BotBName     string     `json:"botBName"`
	MapName      string     `json:"mapName"`
	Status       string     `json:"status"`
	WinnerID     *int64     `json:"winnerId"`
	ScoreA       *int       `json:"scoreA"`
	ScoreB       *int       `json:"scoreB"`
	Error        *string    `json:"error"`
	Created      time.Time  `json:"createdAt"`
	Finished     *time.Time `json:"finishedAt"`
	TournamentID *int64     `json:"tournamentId"`
	Round        *int       `json:"round"`
	Slot         *int       `json:"slot"`
}

func (s *Server) queryMatches(ctx context.Context, where string, args ...any) ([]matchRow, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT mt.id, mt.bot_a_id, mt.bot_b_id, ba.name, bb.name, m.name,
		       mt.status, mt.winner_id, mt.score_a, mt.score_b, mt.error,
		       mt.created_at, mt.finished_at,
		       mt.tournament_id, mt.tournament_round, mt.tournament_slot
		FROM matches mt
		JOIN bots ba ON ba.id = mt.bot_a_id
		JOIN bots bb ON bb.id = mt.bot_b_id
		JOIN maps m ON m.id = mt.map_id `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []matchRow{}
	for rows.Next() {
		var mr matchRow
		if err := rows.Scan(&mr.ID, &mr.BotAID, &mr.BotBID, &mr.BotAName, &mr.BotBName,
			&mr.MapName, &mr.Status, &mr.WinnerID, &mr.ScoreA, &mr.ScoreB, &mr.Error,
			&mr.Created, &mr.Finished, &mr.TournamentID, &mr.Round, &mr.Slot); err != nil {
			return nil, err
		}
		out = append(out, mr)
	}
	return out, nil
}

func (s *Server) createMatch(w http.ResponseWriter, r *http.Request, u *AuthedUser) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		BotAID int64 `json:"botAId"`
		BotBID int64 `json:"botBId"`
		MapID  int64 `json:"mapId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	if req.BotAID == req.BotBID {
		writeErr(w, http.StatusBadRequest, "a bot cannot play itself")
		return
	}
	ctx := r.Context()

	for _, id := range []int64{req.BotAID, req.BotBID} {
		var status string
		var ownerID *int64
		err := s.Pool.QueryRow(ctx, `SELECT status, owner_id FROM bots WHERE id=$1`, id).Scan(&status, &ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, fmt.Sprintf("bot %d not found", id))
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if status != "active" {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("bot %d is not active (status: %s)", id, status))
			return
		}
		// You may only start matches on behalf of your own bot.
		if id == req.BotAID && (ownerID == nil || *ownerID != u.ID) {
			writeErr(w, http.StatusForbidden, "you can only challenge with a bot you own")
			return
		}
	}

	mapID := req.MapID
	if mapID == 0 {
		var ids []int64
		rows, err := s.Pool.Query(ctx, `SELECT id FROM maps`)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		if len(ids) == 0 {
			writeErr(w, http.StatusInternalServerError, "no maps seeded")
			return
		}
		mapID = ids[rand.Intn(len(ids))]
	}

	var matchID int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO matches (bot_a_id, bot_b_id, map_id, status)
		VALUES ($1, $2, $3, 'queued') RETURNING id`,
		req.BotAID, req.BotBID, mapID).Scan(&matchID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := queue.Enqueue(ctx, s.RDB, queue.Job{Type: queue.JobMatch, MatchID: matchID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "enqueue: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": matchID, "status": "queued"})
}

func (s *Server) listMatches(w http.ResponseWriter, r *http.Request) {
	matches, err := s.queryMatches(r.Context(), `ORDER BY mt.id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, matches)
}

func (s *Server) getMatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	matches, err := s.queryMatches(r.Context(), `WHERE mt.id=$1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(matches) == 0 {
		writeErr(w, http.StatusNotFound, "match not found")
		return
	}
	writeJSON(w, http.StatusOK, matches[0])
}

func (s *Server) getReplay(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	matches, err := s.queryMatches(r.Context(), `WHERE mt.id=$1`, id)
	if err != nil || len(matches) == 0 {
		writeErr(w, http.StatusNotFound, "match not found")
		return
	}

	rows, err := s.Pool.Query(r.Context(), `
		SELECT turn_number, player, anfield, piece, move_x, move_y
		FROM match_turns WHERE match_id=$1 ORDER BY turn_number`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type turn struct {
		N       int    `json:"n"`
		Player  int    `json:"player"`
		Anfield string `json:"anfield"`
		Piece   string `json:"piece"`
		X       int    `json:"x"`
		Y       int    `json:"y"`
	}
	turns := []turn{}
	for rows.Next() {
		var t turn
		if err := rows.Scan(&t.N, &t.Player, &t.Anfield, &t.Piece, &t.X, &t.Y); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		turns = append(turns, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"match": matches[0], "turns": turns})
}

// ---- players ----

type playerRow struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	FirstName  *string  `json:"firstName"`
	LastName   *string  `json:"lastName"`
	Bots       int      `json:"bots"`
	ActiveBots int      `json:"activeBots"`
	BestRating *float64 `json:"bestRating"`
	BestBot    *string  `json:"bestBot"`
	Wins       int      `json:"wins"`
	Losses     int      `json:"losses"`
	Draws      int      `json:"draws"`
	Played     int      `json:"matchesPlayed"`
}

const playerSelect = `
	SELECT u.id, u.name, u.first_name, u.last_name,
	       count(b.id)::int,
	       count(b.id) FILTER (WHERE b.status = 'active')::int,
	       max(r.rating),
	       (array_agg(b.name ORDER BY r.rating DESC NULLS LAST))[1],
	       COALESCE(sum(r.wins), 0)::int,
	       COALESCE(sum(r.losses), 0)::int,
	       COALESCE(sum(r.draws), 0)::int,
	       COALESCE(sum(r.matches_played), 0)::int
	FROM users u
	LEFT JOIN bots b ON b.owner_id = u.id
	LEFT JOIN rankings r ON r.bot_id = b.id`

func scanPlayer(row pgx.Row) (playerRow, error) {
	var p playerRow
	err := row.Scan(&p.ID, &p.Name, &p.FirstName, &p.LastName, &p.Bots, &p.ActiveBots,
		&p.BestRating, &p.BestBot, &p.Wins, &p.Losses, &p.Draws, &p.Played)
	return p, err
}

func (s *Server) listPlayers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(),
		playerSelect+` GROUP BY u.id ORDER BY max(r.rating) DESC NULLS LAST, u.name`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	players := []playerRow{}
	for rows.Next() {
		p, err := scanPlayer(rows)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		players = append(players, p)
	}
	writeJSON(w, http.StatusOK, players)
}

func (s *Server) getPlayer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := scanPlayer(s.Pool.QueryRow(r.Context(),
		playerSelect+` WHERE u.name = $1 GROUP BY u.id`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "player not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows, err := s.Pool.Query(r.Context(),
		botSelect+` WHERE u.name = $1 ORDER BY r.rating DESC NULLS LAST, b.id`, name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	bots := []botRow{}
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		bots = append(bots, b)
	}

	matches, err := s.queryMatches(r.Context(), `
		WHERE mt.bot_a_id IN (SELECT id FROM bots WHERE owner_id = $1)
		   OR mt.bot_b_id IN (SELECT id FROM bots WHERE owner_id = $1)
		ORDER BY mt.id DESC LIMIT 50`, p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": p, "bots": bots, "matches": matches})
}

// ---- leaderboard / maps / static ----

func (s *Server) leaderboard(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), botSelect+`
		WHERE b.status='active' AND r.bot_id IS NOT NULL
		ORDER BY r.rating DESC, r.wins DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	bots := []botRow{}
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		bots = append(bots, b)
	}
	writeJSON(w, http.StatusOK, bots)
}

func (s *Server) listMaps(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT id, name, width, height FROM maps ORDER BY id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type m struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	maps := []m{}
	for rows.Next() {
		var mm m
		if err := rows.Scan(&mm.ID, &mm.Name, &mm.Width, &mm.Height); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		maps = append(maps, mm)
	}
	writeJSON(w, http.StatusOK, maps)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Pool.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "postgres": err.Error()})
		return
	}
	if err := s.RDB.Ping(r.Context()).Err(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "redis": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// static serves the built frontend with an SPA fallback to index.html.
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	dist := s.Cfg.WebDist
	if _, err := os.Stat(dist); err != nil {
		http.Error(w, "frontend not built (run npm build in web/); API is under /api/", http.StatusNotFound)
		return
	}
	path := filepath.Join(dist, filepath.Clean("/"+r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		// Vite content-hashes everything under assets/, so cache hard.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(dist, "index.html"))
}

func (s *Server) LogAndServe() error {
	log.Printf("api listening on %s", s.Cfg.ListenAddr)
	return http.ListenAndServe(s.Cfg.ListenAddr, s.Handler())
}
