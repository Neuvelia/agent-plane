package gitstate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agentplane/internal/gitlocate"
)

func TestRunStartsGitFoundOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell wrapper")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "used")
	script := "#!/bin/sh\n: > '" + marker + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := runTestRepo(t)
	t.Setenv("PATH", bin)
	if _, err := Run(context.Background(), dir, RunOptions{}, "rev-parse", "--verify", "HEAD"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the git found on PATH was not used: %v", err)
	}
}

func TestRunReportsMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Run(context.Background(), t.TempDir(), RunOptions{}, "--version")
	var re *RunError
	if !errors.As(err, &re) || re.Class != RunStart {
		t.Fatalf("want RunStart error, got %T %v", err, err)
	}
	if !errors.Is(err, gitlocate.ErrGitNotFound) {
		t.Fatalf("want gitlocate.ErrGitNotFound in chain: %v", err)
	}
	if msg := errors.Unwrap(err).Error(); !strings.Contains(msg, "PATH") || !strings.Contains(msg, "git") {
		t.Fatalf("error must name what was tried: %q", msg)
	}
}
