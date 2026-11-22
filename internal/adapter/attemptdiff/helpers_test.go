package attemptdiff

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME=" + dir,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// newRepo returns a committed repository whose files are given as path -> content.
func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	if len(files) == 0 {
		files = map[string]string{"README": "x\n"}
	}
	for p, c := range files {
		write(t, dir, p, c)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func write(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRecorder(t *testing.T) *Recorder {
	t.Helper()
	r, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func snap(t *testing.T, r *Recorder, dir string) Snapshot {
	t.Helper()
	s, err := r.Snapshot(context.Background(), dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return s
}

// compare snapshots dir, runs edit and returns the diff between the two.
func compare(t *testing.T, r *Recorder, dir string, red Redactor, edit func()) Attempt {
	t.Helper()
	before := snap(t, r, dir)
	edit()
	after := snap(t, r, dir)
	got, err := r.Diff(context.Background(), before, after, red)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	return got
}

func byPath(a Attempt) map[string]events.AttemptDiffPayload {
	m := map[string]events.AttemptDiffPayload{}
	for _, f := range a.Files {
		m[f.Path] = f
	}
	return m
}

func paths(a Attempt) []string {
	var out []string
	for _, f := range a.Files {
		out = append(out, f.Path)
	}
	return out
}

type markerRedactor struct{ secret string }

func (m markerRedactor) Redact(text []byte) ([]byte, bool) {
	out := bytes.ReplaceAll(text, []byte(m.secret), []byte("[REDACTED]"))
	return out, !bytes.Equal(out, text)
}

func lines(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(prefix)
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n")
	}
	return b.String()
}
