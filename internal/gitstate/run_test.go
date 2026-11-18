package gitstate

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func runTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@e.invalid", "commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("/usr/bin/git", args...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestRunReturnsStdoutAndExitCodeOnNonZero(t *testing.T) {
	dir := runTestRepo(t)
	res, err := Run(context.Background(), dir, RunOptions{}, "rev-parse", "--verify", "HEAD")
	if err != nil || res.ExitCode != 0 || len(strings.TrimSpace(string(res.Stdout))) < 40 {
		t.Fatalf("success: %+v %v", res, err)
	}
	res, err = Run(context.Background(), dir, RunOptions{}, "rev-parse", "--verify", "--quiet", "refs/does/not/exist")
	if err == nil || res.ExitCode != 1 {
		t.Fatalf("a failing command must return its exit code and an error: %+v %v", res, err)
	}
	var re *RunError
	if !errors.As(err, &re) || re.ExitCode != 1 {
		t.Fatalf("error type: %T %v", err, err)
	}
}

func TestRunUsesControlledEnvironmentAndDisabledHooks(t *testing.T) {
	dir := runTestRepo(t)
	t.Setenv("GIT_DIR", t.TempDir()) // an inherited GIT_DIR would redirect Git away from dir
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", "/tmp")
	res, err := Run(context.Background(), dir, RunOptions{}, "config", "--get", "core.hooksPath")
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "/dev/null" {
		t.Fatalf("hooks must be disabled regardless of the caller environment: %q %v", res.Stdout, err)
	}
	if _, err = Run(context.Background(), dir, RunOptions{}, "rev-parse", "--show-toplevel"); err != nil {
		t.Fatalf("caller GIT_DIR leaked into the runner: %v", err)
	}
}

func TestRunEnforcesTimeoutAndStdoutLimitAndHidesStderr(t *testing.T) {
	dir := runTestRepo(t)
	res, err := Run(context.Background(), dir, RunOptions{StdoutLimit: 10}, "--version")
	var re *RunError
	if err == nil || !errors.As(err, &re) || re.Class != RunOutputLimit || len(res.Stdout) > 10 {
		t.Fatalf("output limit: %+v %v", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Run(ctx, dir, RunOptions{}, "--version"); err == nil {
		t.Fatal("a cancelled context must fail the command")
	}
	_, err = Run(context.Background(), dir, RunOptions{Timeout: time.Nanosecond}, "--version")
	if err == nil || !errors.As(err, &re) || re.Class != RunTimeout {
		t.Fatalf("timeout class: %v", err)
	}
	_, err = Run(context.Background(), dir, RunOptions{}, "checkout", "no-such-branch-xyz")
	if err == nil || strings.Contains(err.Error(), "no-such-branch-xyz") && strings.Contains(err.Error(), "pathspec") {
		t.Fatalf("raw Git stderr must never appear in the error: %v", err)
	}
}

func TestEnvironmentDisablesTerminalPrompts(t *testing.T) {
	for _, kv := range Environment("/usr/bin/git") {
		if kv == "GIT_TERMINAL_PROMPT=0" {
			return
		}
	}
	t.Fatal("Git must never prompt on a terminal")
}
