package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ---- read-only database browser ----
//
// Lets an admin browse tables and run ad-hoc read-only queries from the web
// UI instead of needing shell/psql access. Table/column identifiers are
// never interpolated into SQL without first being validated against a live
// list fetched from Postgres itself, and adminDBQuery runs inside a
// read-only transaction with a short statement timeout as defense in depth.

const tokenMask = 8 // characters of a session token left unmasked

func maskToken(v string) string {
	if len(v) <= tokenMask {
		return v
	}
	return v[:tokenMask] + "…"
}

// adminDBTables lists user tables with cheap, approximate row counts.
func (s *Server) adminDBTables(w http.ResponseWriter, r *http.Request, _ *AuthedUser) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT relname, n_live_tup FROM pg_stat_user_tables
		WHERE schemaname='public' ORDER BY relname`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type tableInfo struct {
		Name string `json:"name"`
		Rows int64  `json:"rows"`
	}
	out := []tableInfo{}
	for rows.Next() {
		var t tableInfo
		if err := rows.Scan(&t.Name, &t.Rows); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

// publicTableNames returns the live set of table names in the public schema,
// used to validate a path-supplied table name before it's ever interpolated
// into a query string.
func (s *Server) publicTableNames(w http.ResponseWriter, r *http.Request) (map[string]bool, bool) {
	rows, err := s.Pool.Query(r.Context(), `SELECT tablename FROM pg_tables WHERE schemaname='public'`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return nil, false
		}
		names[name] = true
	}
	return names, true
}

// rowsToMaps converts the current result set into a generic slice of
// column-name -> value maps, along with the ordered column names. If
// maskTokenCol is true, any column literally named "token" has its values
// masked before being returned.
func rowsToMaps(rows pgx.Rows, maskTokenCol bool) ([]string, []map[string]any, error) {
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	tokenIdx := -1
	for i, f := range fields {
		cols[i] = f.Name
		if maskTokenCol && f.Name == "token" {
			tokenIdx = i
		}
	}
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, nil, err
		}
		if tokenIdx >= 0 {
			if sv, ok := vals[tokenIdx].(string); ok {
				vals[tokenIdx] = maskToken(sv)
			}
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return cols, out, nil
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// adminDBTableRows returns a page of rows from one table, identified by an
// exact-match path param validated against the live table list.
func (s *Server) adminDBTableRows(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
	table := r.PathValue("table")
	names, ok := s.publicTableNames(w, r)
	if !ok {
		return
	}
	if !names[table] {
		writeErr(w, http.StatusBadRequest, "unknown table")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	limit = clampInt(limit, 1, 200)

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	if offset < 0 {
		offset = 0
	}

	query := "SELECT * FROM " + pgx.Identifier{table}.Sanitize() + " ORDER BY 1 LIMIT $1 OFFSET $2"
	rows, err := s.Pool.Query(r.Context(), query, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	cols, data, err := rowsToMaps(rows, table == "sessions")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logAudit(r.Context(), actor.Login, "db_browse", table)
	writeJSON(w, http.StatusOK, map[string]any{
		"table":   table,
		"columns": cols,
		"rows":    data,
		"limit":   limit,
		"offset":  offset,
	})
}

const dbQueryRowCap = 500

// adminDBQuery runs an ad-hoc, admin-supplied read-only query. Multiple
// layers of defense: a syntactic prefix/statement check first, then (the
// real safety net) execution inside a read-only transaction with a short
// statement timeout, so even a query that slips past the syntactic check
// can't write or hang the connection pool.
func (s *Server) adminDBQuery(w http.ResponseWriter, r *http.Request, actor *AuthedUser) {
	var req struct {
		SQL string `json:"sql"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sql := strings.TrimSpace(req.SQL)
	if sql == "" {
		writeErr(w, http.StatusBadRequest, "sql is required")
		return
	}
	lower := strings.ToLower(sql)
	if !strings.HasPrefix(lower, "select") && !strings.HasPrefix(lower, "with") {
		writeErr(w, http.StatusBadRequest, "only SELECT/WITH queries are allowed")
		return
	}
	// Strip at most one trailing semicolon (and any trailing whitespace after it).
	trimmed := strings.TrimRight(sql, " \t\n\r")
	if strings.HasSuffix(trimmed, ";") {
		trimmed = strings.TrimRight(strings.TrimSuffix(trimmed, ";"), " \t\n\r")
	}
	if strings.Contains(trimmed, ";") {
		writeErr(w, http.StatusBadRequest, "only a single statement is allowed")
		return
	}
	sql = trimmed

	ctx := r.Context()
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '5000'`); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows, err := tx.Query(ctx, sql)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	tokenIdx := -1
	for i, f := range fields {
		cols[i] = f.Name
		if f.Name == "token" {
			tokenIdx = i
		}
	}

	data := []map[string]any{}
	truncated := false
	for rows.Next() {
		if len(data) >= dbQueryRowCap {
			truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tokenIdx >= 0 {
			if sv, ok := vals[tokenIdx].(string); ok {
				vals[tokenIdx] = maskToken(sv)
			}
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = vals[i]
		}
		data = append(data, m)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	logged := req.SQL
	if len(logged) > 300 {
		logged = logged[:300]
	}
	s.logAudit(ctx, actor.Login, "db_query", logged)

	writeJSON(w, http.StatusOK, map[string]any{
		"columns":   cols,
		"rows":      data,
		"truncated": truncated,
		"rowCount":  len(data),
	})
}
