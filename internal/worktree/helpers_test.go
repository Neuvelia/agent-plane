package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs plain Git in dir with a clean environment and a fixed identity.
// It is the test's independent oracle; production code goes through gitstate.Run.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("/usr/bin/git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// newSource creates a committed repository with a.txt, b.txt and shared.txt.
func newSource(t *testing.T) string {
	t.Helper()
	dir := canonical(t, t.TempDir())
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, "init", "-q")
	writeFile(t, filepath.Join(src, "a.txt"), "a\n")
	writeFile(t, filepath.Join(src, "b.txt"), "b\n")
	writeFile(t, filepath.Join(src, "shared.txt"), "one\ntwo\nthree\n")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-q", "-m", "base")
	return src
}

// fixture is a source repository, a fresh worktree root and a ready manager.
type fixture struct {
	src  string
	root string
	pre  Source
	mgr  *Manager
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	src := newSource(t)
	root := filepath.Join(filepath.Dir(src), "worktrees")
	pre, err := Preflight(context.Background(), src, root)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	mgr, err := NewManager(root)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	return fixture{src: src, root: root, pre: pre, mgr: mgr}
}

func wantCode(t *testing.T, err error, code Code) *Error {
	t.Helper()
	var werr *Error
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", code)
	}
	if !asWorktreeError(err, &werr) || werr.Code != code {
		t.Fatalf("expected code %s, got %v", code, err)
	}
	return werr
}
