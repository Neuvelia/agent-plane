//go:build darwin || linux

package gitstate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/process"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-process-supervisor" {
		os.Exit(process.Supervise(os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func TestSelectedGitIsUsedForWorkspaceAndScope(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		command := exec.Command("/usr/bin/git", args...)
		command.Dir = root
		command.Env = InspectionEnvironment()
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("initialize repository: %v: %s", err, output)
		}
	}
	selected := filepath.Join(t.TempDir(), "selected-git")
	if err := os.WriteFile(selected, []byte("#!/bin/sh\nexec /usr/bin/git \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, err := WithExecutable(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := CanonicalWorkspace(ctx, root); err != nil || got != root {
		t.Fatalf("selected Git workspace = %q, %v", got, err)
	}
	baseline, err := Capture(ctx, root)
	if err != nil || baseline.Root != root {
		t.Fatalf("selected Git capture = %#v, %v", baseline, err)
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(ctx, root); err == nil {
		t.Fatal("selected executable disappeared but capture fell back to default Git")
	}
	if _, err := Capture(context.Background(), root); err != nil {
		t.Fatalf("Unix CLI default Git changed: %v", err)
	}
}

func TestGitExecutableRejectsUnboundPath(t *testing.T) {
	if _, err := WithExecutable(context.Background(), "git"); err == nil {
		t.Fatal("relative Git executable was accepted")
	}
}

func TestSelectedGitWrapperCannotHoldInspectionPipeOpen(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "pid")
	selected := filepath.Join(root, "git-wrapper")
	content := fmt.Sprintf("#!/bin/sh\nsleep 10 &\necho $! > %q\nexit 0\n", pidFile)
	if err := os.WriteFile(selected, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	ctx, err := WithExecutable(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := git(ctx, root, "version"); err == nil {
		t.Fatal("abandoned Git stdout was treated as complete evidence")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Git inspection did not join within its pipe bound: %s", elapsed)
	}
}

func TestSupervisedGitWrapperJoinsDescendant(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "pid")
	selected := filepath.Join(root, "git-wrapper")
	content := fmt.Sprintf("#!/bin/sh\nsleep 10 &\necho $! > %q\nexit 0\n", pidFile)
	if err := os.WriteFile(selected, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	ctx, err := WithExecutable(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = WithSupervisor(ctx, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := git(ctx, root, "version"); err != nil {
		t.Fatalf("owned Git wrapper failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("owned Git inspection did not join promptly: %s", elapsed)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("owned Git descendant survived supervisor return")
	}
}

func TestSupervisedGitRejectsNonzeroExitEvenWithPlausibleOutput(t *testing.T) {
	root := t.TempDir()
	selected := filepath.Join(root, "git-wrapper")
	if err := os.WriteFile(selected, []byte("#!/bin/sh\nprintf 'plausible output\\n'\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, err := WithExecutable(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = WithSupervisor(ctx, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if output, err := git(ctx, root, "ls-files"); err == nil {
		t.Fatalf("nonzero supervised Git exit accepted: %q", output)
	}
}
