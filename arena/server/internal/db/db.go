package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Seed inserts the built-in maps and reference robots shipped inside the
// match image, plus a "system" user to own them. Idempotent.
func Seed(ctx context.Context, pool *pgxpool.Pool) error {
	var systemID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO users (name) VALUES ('system')
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&systemID)
	if err != nil {
		return fmt.Errorf("seed system user: %w", err)
	}

	maps := []struct {
		name string
		path string
		w, h int
	}{
		{"map00", "maps/map00", 20, 15},
		{"map01", "maps/map01", 40, 30},
		{"map02", "maps/map02", 100, 100},
	}
	for _, m := range maps {
		_, err := pool.Exec(ctx, `
			INSERT INTO maps (name, path, width, height) VALUES ($1, $2, $3, $4)
			ON CONFLICT (name) DO NOTHING`, m.name, m.path, m.w, m.h)
		if err != nil {
			return fmt.Errorf("seed map %s: %w", m.name, err)
		}
	}

	for _, r := range []string{"bender", "h2_d2", "wall_e", "terminator"} {
		var botID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO bots (owner_id, name, language, binary_path, status)
			VALUES ($1, $2, 'builtin', $3, 'active')
			ON CONFLICT (name) DO UPDATE SET binary_path = EXCLUDED.binary_path
			RETURNING id`, systemID, r, "robots/"+r).Scan(&botID)
		if err != nil {
			return fmt.Errorf("seed robot %s: %w", r, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO rankings (bot_id) VALUES ($1)
			ON CONFLICT (bot_id) DO NOTHING`, botID); err != nil {
			return fmt.Errorf("seed ranking %s: %w", r, err)
		}
	}
	return nil
}
