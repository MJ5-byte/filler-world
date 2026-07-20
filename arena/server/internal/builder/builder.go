package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"filler-arena/internal/config"
	"filler-arena/internal/runner"
)

const (
	goImage     = "golang:1.22-bookworm"
	gccImage    = "gcc:13-bookworm"
	pythonImage = "python:3.12-slim-bookworm"
	rustImage   = "rust:1.79-slim-bookworm"
)

// Images returns every image builds depend on, so they can be pre-pulled
// (build containers run with --network=none and cannot pull anything).
func Images() []string { return []string{goImage, gccImage, pythonImage, rustImage} }

// Build validates/compiles the uploaded source in botDir/src and produces
// botDir/run (plus support files) that the match container will execute.
// Returns the build log; err != nil means the bot must be marked failed.
// All user code runs inside network-disabled, resource-capped containers.
func Build(ctx context.Context, cfg config.Config, botID int64, lang, botDir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.BuildWallClock)
	defer cancel()

	srcDir := filepath.Join(botDir, "src")
	switch lang {
	case "binary":
		// Cheap sanity check: a non-Linux-executable upload would otherwise
		// pass "building" and then fail every single match it plays.
		head := make([]byte, 4)
		f, err := os.Open(filepath.Join(srcDir, "upload"))
		if err != nil {
			return "", err
		}
		n, _ := f.Read(head)
		f.Close()
		if n < 4 || head[0] != 0x7f || head[1] != 'E' || head[2] != 'L' || head[3] != 'F' {
			return "the uploaded file is not a Linux executable (missing ELF header)",
				fmt.Errorf("not an ELF binary — compile for Linux x86-64 (e.g. GOOS=linux go build, or cargo build --target x86_64-unknown-linux-musl)")
		}
		return "", os.Rename(filepath.Join(srcDir, "upload"), filepath.Join(botDir, "run"))

	case "python":
		log, err := runBuildContainer(ctx, cfg, botID, pythonImage, srcDir, false, botDir,
			"python3", "-m", "py_compile", "/work/src/upload")
		if err != nil {
			return log, fmt.Errorf("python syntax check failed: %w", err)
		}
		src, err := os.ReadFile(filepath.Join(srcDir, "upload"))
		if err != nil {
			return log, err
		}
		if err := os.WriteFile(filepath.Join(botDir, "bot.py"), src, 0o644); err != nil {
			return log, err
		}
		launcher := "#!/bin/sh\nexec python3 \"$(dirname \"$0\")/bot.py\"\n"
		return log, os.WriteFile(filepath.Join(botDir, "run"), []byte(launcher), 0o755)

	case "go":
		log, err := runBuildContainer(ctx, cfg, botID, goImage, srcDir, true, botDir,
			"sh", "-c", "mkdir -p /tmp/out && cp /work/src/upload /tmp/main.go && cd /tmp && CGO_ENABLED=0 GOCACHE=/tmp/gocache GOPATH=/tmp/gopath go build -o /tmp/out/bot main.go")
		if err != nil {
			return log, fmt.Errorf("go build failed: %w", err)
		}
		return log, nil

	case "rust":
		// Single-file rustc build: no cargo, no crates — same offline rule as
		// Go/C. Static-ish glibc binary runs fine in the match image (same
		// bookworm base).
		log, err := runBuildContainer(ctx, cfg, botID, rustImage, srcDir, true, botDir,
			"sh", "-c", "mkdir -p /tmp/out && cp /work/src/upload /tmp/main.rs && rustc -O -o /tmp/out/bot /tmp/main.rs")
		if err != nil {
			return log, fmt.Errorf("rust build failed: %w", err)
		}
		return log, nil

	case "c":
		log, err := runBuildContainer(ctx, cfg, botID, gccImage, srcDir, true, botDir,
			"sh", "-c", "mkdir -p /tmp/out && cp /work/src/upload /tmp/main.c && cd /tmp && gcc -O2 -static -o /tmp/out/bot main.c")
		if err != nil {
			return log, fmt.Errorf("c build failed: %w", err)
		}
		return log, nil

	default:
		return "", fmt.Errorf("unsupported language %q", lang)
	}
}

// runBuildContainer creates a network-disabled build container, copies the
// source into /work/src, runs the command, and (if extract is set) pulls
// /tmp/out/bot back to botDir/run. Always removes the container.
func runBuildContainer(ctx context.Context, cfg config.Config, botID int64, image, srcDir string, extract bool, botDir string, cmd ...string) (string, error) {
	name := fmt.Sprintf("arena-build-%d", botID)
	runner.RemoveContainer(name)
	defer runner.RemoveContainer(name)

	args := []string{
		"create", "--name", name,
		"--network=none",
		"--memory", cfg.BuildMemoryLimit,
		"--memory-swap", cfg.BuildMemoryLimit,
		"--cpus", cfg.BuildCPULimit,
		"--pids-limit", fmt.Sprint(cfg.BuildPidsLimit),
		image,
	}
	args = append(args, cmd...)
	if out, err := runner.DockerOut(ctx, args...); err != nil {
		return out, err
	}
	// Copy to / with a nested prefix: docker cp requires the destination
	// directory to already exist, and /work does not in stock images.
	if err := runner.CopyDirIntoContainer(ctx, name, srcDir, "work/src", "/"); err != nil {
		return "", err
	}
	log, err := runner.DockerOut(ctx, "start", "-a", name)
	if err != nil {
		return log, err
	}
	if extract {
		if err := runner.CopyFileFromContainer(ctx, name, "/tmp/out/bot", filepath.Join(botDir, "run")); err != nil {
			return log, fmt.Errorf("extract binary: %w", err)
		}
	}
	return log, nil
}
