package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"filler-arena/internal/config"
)

// BotRef describes one participant. Builtin bots already live inside the
// match image; uploaded bots live in a host directory that gets staged into
// a persistent per-bot volume.
type BotRef struct {
	ID      int64
	Builtin bool
	// Builtin: path inside the image (robots/bender).
	// Uploaded: host directory containing an executable entry point "run".
	Path string
}

type MatchOutput struct {
	Result Result
}

// Bots are immutable once built, so each uploaded bot's files are staged into
// a named volume exactly once per worker process and mounted read-only into
// every match after that. This removes a helper container + copy from the
// per-match hot path.
var (
	stagedMu sync.Mutex
	staged   = map[int64]*sync.Once{}
)

func botVolume(id int64) string { return fmt.Sprintf("arena-bot-%d", id) }

func ensureBotVolume(ctx context.Context, cfg config.Config, bot BotRef) (string, error) {
	stagedMu.Lock()
	once, ok := staged[bot.ID]
	if !ok {
		once = &sync.Once{}
		staged[bot.ID] = once
	}
	stagedMu.Unlock()

	var stageErr error
	once.Do(func() {
		vol := botVolume(bot.ID)
		if _, err := DockerOut(ctx, "volume", "create", vol); err != nil {
			stageErr = err
			return
		}
		helper := vol + "-fill"
		RemoveContainer(helper)
		defer RemoveContainer(helper)
		if _, err := DockerOut(ctx, "create", "--name", helper,
			"-v", vol+":/staging", cfg.MatchImage, "true"); err != nil {
			stageErr = err
			return
		}
		// Overwriting identical content on restage (worker restart) is fine.
		stageErr = CopyDirIntoContainer(ctx, helper, bot.Path, ".", "/staging")
	})
	if stageErr != nil {
		// Allow a later match to retry staging.
		stagedMu.Lock()
		delete(staged, bot.ID)
		stagedMu.Unlock()
		return "", fmt.Errorf("stage bot %d: %w", bot.ID, stageErr)
	}
	return botVolume(bot.ID), nil
}

// Run executes one match in a locked-down container:
// no network, capped memory/CPU/pids, read-only rootfs, tmpfs /tmp, and a
// wall-clock kill enforced here regardless of game_engine's own -t timeout.
func Run(ctx context.Context, cfg config.Config, matchID int64, botA, botB BotRef, mapPath string) (MatchOutput, error) {
	name := fmt.Sprintf("arena-match-%d", matchID)
	RemoveContainer(name) // stale container from a crashed prior run
	defer RemoveContainer(name)

	pathA, pathB := botA.Path, botB.Path
	var volArgs []string
	if !botA.Builtin {
		vol, err := ensureBotVolume(ctx, cfg, botA)
		if err != nil {
			return MatchOutput{}, err
		}
		volArgs = append(volArgs, "-v", vol+":/filler/bots/a:ro")
		pathA = "bots/a/run"
	}
	if !botB.Builtin {
		vol, err := ensureBotVolume(ctx, cfg, botB)
		if err != nil {
			return MatchOutput{}, err
		}
		volArgs = append(volArgs, "-v", vol+":/filler/bots/b:ro")
		pathB = "bots/b/run"
	}

	createArgs := []string{
		"create", "--name", name,
		"--network=none",
		"--memory", cfg.MemoryLimit,
		"--memory-swap", cfg.MemoryLimit,
		"--cpus", cfg.CPULimit,
		"--pids-limit", strconv.Itoa(cfg.PidsLimit),
		"--read-only",
		"--tmpfs", "/tmp:rw,size=64m",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
	}
	createArgs = append(createArgs, volArgs...)
	createArgs = append(createArgs,
		cfg.MatchImage,
		"./game_engine", "-f", mapPath, "-p1", pathA, "-p2", pathB,
		"-t", strconv.Itoa(cfg.EngineTimeout),
	)
	if _, err := DockerOut(ctx, createArgs...); err != nil {
		return MatchOutput{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.MatchWallClock)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "docker", "start", "-a", name)
	// Caps keep a bot that floods its streams from OOMing the worker: the
	// biggest legitimate replay (100×100, ~2000 half-turns) is ~30 MB, so
	// 64 MB of stdout is generous; stderr is diagnostics only.
	stdout := newCapWriter(64 << 20)
	stderr := newCapWriter(1 << 20)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()

	if runCtx.Err() == context.DeadlineExceeded {
		// CommandContext only kills the docker CLI; make sure the container dies.
		RemoveContainer(name)
		return MatchOutput{}, fmt.Errorf("match exceeded wall-clock limit %s", cfg.MatchWallClock)
	}

	out := MatchOutput{Result: Parse(stdout.String())}
	if err != nil && !out.Result.HasScores {
		return out, fmt.Errorf("game engine failed: %w: %s", err, firstLines(stderr.String(), 5))
	}
	if !out.Result.HasScores {
		return out, fmt.Errorf("no final scores in engine output: %s", firstLines(stdout.String()+stderr.String(), 5))
	}
	return out, nil
}

// capWriter buffers up to max bytes and silently discards the rest,
// remembering that it truncated.
type capWriter struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func newCapWriter(max int) *capWriter { return &capWriter{max: max} }

func (w *capWriter) Write(p []byte) (int, error) {
	n := len(p)
	if room := w.max - w.buf.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
			w.truncated = true
		}
		w.buf.Write(p)
	} else {
		w.truncated = true
	}
	return n, nil // report full write so the pipe keeps draining
}

func (w *capWriter) String() string {
	if w.truncated {
		return w.buf.String() + "\n[output truncated]"
	}
	return w.buf.String()
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " | ")
}
