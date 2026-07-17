package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DockerOut runs a docker CLI command and returns combined output.
func DockerOut(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// RemoveContainer force-removes a container, ignoring errors (cleanup path).
func RemoveContainer(name string) {
	_ = exec.Command("docker", "rm", "-f", name).Run()
}

// tarDir packs the files of hostDir (non-recursive) into a tar archive under
// prefix/, forcing mode 0755 so executables survive the Windows filesystem's
// lack of unix permission bits.
func tarDir(hostDir, prefix string) ([]byte, error) {
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     prefix + "/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}); err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(hostDir, e.Name()))
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: prefix + "/" + e.Name(),
			Mode: 0o755,
			Size: int64(len(data)),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CopyDirIntoContainer streams hostDir's files into containerDest/prefix via
// `docker cp -`, avoiding host bind mounts entirely (works with any host
// path encoding and read-only container rootfs, since the container is not
// started yet).
func CopyDirIntoContainer(ctx context.Context, container, hostDir, prefix, containerDest string) error {
	archive, err := tarDir(hostDir, prefix)
	if err != nil {
		return fmt.Errorf("pack %s: %w", hostDir, err)
	}
	cmd := exec.CommandContext(ctx, "docker", "cp", "-", container+":"+containerDest)
	cmd.Stdin = bytes.NewReader(archive)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker cp into %s: %w: %s", container, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CopyFileFromContainer extracts a single file from a container to hostPath
// via `docker cp ... -` (tar over stdout).
func CopyFileFromContainer(ctx context.Context, container, containerPath, hostPath string) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", container+":"+containerPath, "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	tr := tar.NewReader(stdout)
	var found bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar from %s: %w", container, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if err := os.WriteFile(hostPath, data, 0o755); err != nil {
			return err
		}
		found = true
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker cp from %s: %w: %s", container, err, strings.TrimSpace(stderr.String()))
	}
	if !found {
		return fmt.Errorf("no file at %s in container", containerPath)
	}
	return nil
}
