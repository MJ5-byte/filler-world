package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	sessionCookie = "arena_session"
	sessionTTL    = 7 * 24 * time.Hour
)

var errBadCreds = errors.New("invalid credentials")

// AuthedUser is the arena's view of a logged-in player.
type AuthedUser struct {
	ID         int64    `json:"id"`
	Login      string   `json:"login"`
	FirstName  *string  `json:"firstName"`
	LastName   *string  `json:"lastName"`
	Email      *string  `json:"email"`
	AuditRatio *float64 `json:"auditRatio"`
	IsAdmin    bool     `json:"isAdmin"`
	IsBlocked  bool     `json:"-"`
}

// remoteUser mirrors the fields we ask the auth provider's GraphQL for.
type remoteUser struct {
	Login      string   `json:"login"`
	FirstName  *string  `json:"firstName"`
	LastName   *string  `json:"lastName"`
	Email      *string  `json:"email"`
	AuditRatio *float64 `json:"auditRatio"`
}

// signinRemote exchanges identifier:password for a JWT at the provider's
// Basic-auth signin endpoint. The password is forwarded, never stored.
func signinRemote(ctx context.Context, signinURL, identifier, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, signinURL, nil)
	if err != nil {
		return "", err
	}
	creds := base64.StdEncoding.EncodeToString([]byte(identifier + ":" + password))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("auth provider unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusOK:
		// The endpoint returns the JWT as a (sometimes quoted) string.
		token := strings.Trim(strings.TrimSpace(string(body)), `"`)
		if token == "" {
			return "", errors.New("auth provider returned an empty token")
		}
		return token, nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNotFound:
		return "", errBadCreds
	default:
		return "", fmt.Errorf("auth provider error (%d)", resp.StatusCode)
	}
}

// fetchRemoteUser pulls the player's identity from the provider's GraphQL.
func fetchRemoteUser(ctx context.Context, gqlURL, jwt string) (remoteUser, error) {
	payload, _ := json.Marshal(map[string]string{
		"query": `{ user { login firstName lastName email auditRatio } }`,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gqlURL, bytes.NewReader(payload))
	if err != nil {
		return remoteUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return remoteUser{}, fmt.Errorf("graphql unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return remoteUser{}, fmt.Errorf("graphql error (%d)", resp.StatusCode)
	}

	var out struct {
		Data struct {
			User json.RawMessage `json:"user"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return remoteUser{}, fmt.Errorf("bad graphql response: %w", err)
	}
	if len(out.Errors) > 0 {
		return remoteUser{}, fmt.Errorf("graphql: %s", out.Errors[0].Message)
	}

	// Hasura returns `user` as a one-element list; tolerate an object too.
	var list []remoteUser
	if err := json.Unmarshal(out.Data.User, &list); err == nil && len(list) > 0 {
		return list[0], nil
	}
	var single remoteUser
	if err := json.Unmarshal(out.Data.User, &single); err == nil && single.Login != "" {
		return single, nil
	}
	return remoteUser{}, errors.New("graphql returned no user")
}

// upsertUser stores/refreshes the provider identity and returns our user row.
// Logins listed in ARENA_ADMIN_LOGINS are promoted to admin here; promotion
// is one-way so removing a name from the env doesn't silently demote.
func (s *Server) upsertUser(ctx context.Context, ru remoteUser) (AuthedUser, error) {
	admin := false
	for _, a := range s.Cfg.AdminLogins {
		if a == ru.Login {
			admin = true
			break
		}
	}
	var u AuthedUser
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO users (name, email, first_name, last_name, audit_ratio, is_admin)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (name) DO UPDATE SET
			email = EXCLUDED.email,
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			audit_ratio = EXCLUDED.audit_ratio,
			is_admin = users.is_admin OR EXCLUDED.is_admin
		RETURNING id, name, first_name, last_name, email, audit_ratio, is_admin, is_blocked`,
		ru.Login, ru.Email, ru.FirstName, ru.LastName, ru.AuditRatio, admin).
		Scan(&u.ID, &u.Login, &u.FirstName, &u.LastName, &u.Email, &u.AuditRatio, &u.IsAdmin, &u.IsBlocked)
	return u, err
}

func (s *Server) createSession(ctx context.Context, w http.ResponseWriter, userID int64) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := hex.EncodeToString(raw)
	if _, err := s.Pool.Exec(ctx, `
		INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		token, userID, time.Now().Add(sessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

// currentUser resolves the session cookie to a user, or nil when logged out.
// A blocked user is treated as logged out — their session row is deleted too,
// so this doubles as cleanup for anyone blocked while their token is still live.
func (s *Server) currentUser(r *http.Request) (*AuthedUser, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	var u AuthedUser
	err = s.Pool.QueryRow(r.Context(), `
		SELECT u.id, u.name, u.first_name, u.last_name, u.email, u.audit_ratio, u.is_admin, u.is_blocked
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > now()`, c.Value).
		Scan(&u.ID, &u.Login, &u.FirstName, &u.LastName, &u.Email, &u.AuditRatio, &u.IsAdmin, &u.IsBlocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if u.IsBlocked {
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE token = $1`, c.Value)
		return nil, nil
	}
	return &u, nil
}

// ---- handlers ----

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	// Opportunistic cleanup keeps the sessions table from growing forever;
	// login is infrequent enough that this costs nothing.
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE expires_at < now()`)

	var req struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	req.Identifier = strings.TrimSpace(req.Identifier)
	if req.Identifier == "" {
		writeErr(w, http.StatusBadRequest, "missing username or email")
		return
	}
	ctx := r.Context()

	var ru remoteUser
	if s.Cfg.AuthDev && req.Password == "" {
		// Local development shortcut: trust the identifier as the login.
		ru = remoteUser{Login: req.Identifier}
	} else {
		jwt, err := signinRemote(ctx, s.Cfg.AuthSigninURL, req.Identifier, req.Password)
		if errors.Is(err, errBadCreds) {
			writeErr(w, http.StatusUnauthorized, "wrong credentials, try again")
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		ru, err = fetchRemoteUser(ctx, s.Cfg.AuthGraphQLURL, jwt)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "signed in, but fetching your profile failed: "+err.Error())
			return
		}
	}

	u, err := s.upsertUser(ctx, ru)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store user: "+err.Error())
		return
	}
	if u.IsBlocked {
		writeErr(w, http.StatusForbidden, "this account has been blocked")
		return
	}
	if err := s.createSession(ctx, w, u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "create session: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE token = $1`, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

// requireAdmin wraps handlers that need an admin session.
func (s *Server) requireAdmin(next func(http.ResponseWriter, *http.Request, *AuthedUser)) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, u *AuthedUser) {
		if !u.IsAdmin {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r, u)
	})
}

// requireUser wraps handlers that need a session.
func (s *Server) requireUser(next func(http.ResponseWriter, *http.Request, *AuthedUser)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.currentUser(r)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "log in first")
			return
		}
		next(w, r, u)
	}
}
