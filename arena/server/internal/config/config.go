package config

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string
	RedisAddr   string
	ListenAddr  string

	// DataDir holds uploaded bot files: <DataDir>/bots/<id>/{run,...}
	DataDir string
	// WebDist, if it exists, is served as the SPA frontend.
	WebDist string

	MatchImage string
	// Per-move timeout passed to game_engine -t.
	EngineTimeout int
	// Hard wall-clock cap per match container, enforced by the worker.
	MatchWallClock time.Duration
	// Hard wall-clock cap per build container.
	BuildWallClock time.Duration

	MemoryLimit string
	CPULimit    string
	PidsLimit   int

	// Resource caps for build containers (compilers need more headroom than
	// a match run, so these default higher than MemoryLimit/CPULimit/PidsLimit).
	BuildMemoryLimit string
	BuildCPULimit    string
	BuildPidsLimit   int

	WorkerConcurrency int
	// Interval for periodic round-robin re-matching (0 disables).
	RematchInterval time.Duration

	// Auth provider (reboot01 / 01-edu platform).
	AuthSigninURL  string
	AuthGraphQLURL string
	// AuthDev allows password-less login with any username, for local
	// development only. Never enable in a deployed instance.
	AuthDev bool
	// AdminLogins are usernames granted admin on login (comma-separated env).
	AdminLogins []string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// defaultConcurrency sizes the worker pool from the host: each match container
// is capped at 1 CPU, so half the cores (2..8) parallelizes well without
// starving the API, Docker, or the OS.
func defaultConcurrency() int {
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
	}
	if n > 8 {
		n = 8
	}
	return n
}

func Load() Config {
	return Config{
		DatabaseURL:       env("ARENA_DATABASE_URL", "postgres://arena:arena@localhost:55432/arena"),
		RedisAddr:         env("ARENA_REDIS_ADDR", "localhost:56379"),
		ListenAddr:        env("ARENA_LISTEN_ADDR", ":8080"),
		DataDir:           env("ARENA_DATA_DIR", "./data"),
		WebDist:           env("ARENA_WEB_DIST", "./web/dist"),
		MatchImage:        env("ARENA_MATCH_IMAGE", "filler-arena-match"),
		EngineTimeout:     envInt("ARENA_ENGINE_TIMEOUT", 5),
		MatchWallClock:    envDur("ARENA_MATCH_WALLCLOCK", 5*time.Minute),
		BuildWallClock:    envDur("ARENA_BUILD_WALLCLOCK", 3*time.Minute),
		MemoryLimit:       env("ARENA_MEMORY_LIMIT", "256m"),
		CPULimit:          env("ARENA_CPU_LIMIT", "1.0"),
		PidsLimit:         envInt("ARENA_PIDS_LIMIT", 128),
		BuildMemoryLimit:  env("ARENA_BUILD_MEMORY_LIMIT", "1g"),
		BuildCPULimit:     env("ARENA_BUILD_CPU_LIMIT", "2"),
		BuildPidsLimit:    envInt("ARENA_BUILD_PIDS_LIMIT", 256),
		WorkerConcurrency: envInt("ARENA_WORKER_CONCURRENCY", defaultConcurrency()),
		RematchInterval:   envDur("ARENA_REMATCH_INTERVAL", 24*time.Hour),
		AuthSigninURL:     env("ARENA_AUTH_SIGNIN_URL", "https://learn.reboot01.com/api/auth/signin"),
		AuthGraphQLURL:    env("ARENA_AUTH_GRAPHQL_URL", "https://learn.reboot01.com/api/graphql-engine/v1/graphql"),
		AuthDev:           env("ARENA_AUTH_DEV", "") == "1",
		AdminLogins:       splitList(env("ARENA_ADMIN_LOGINS", "")),
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
