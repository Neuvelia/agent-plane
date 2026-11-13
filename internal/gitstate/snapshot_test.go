package gitstate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("/usr/bin/git", args...)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	runGit(t, d, "init", "-q")
	write(t, d, "a.txt", "base")
	write(t, d, "dir/b.txt", "base")
	write(t, d, ".gitignore", "ignored.log\n")
	runGit(t, d, "add", ".")
	runGit(t, d, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	return d
}

func TestChangedSortedPureComparison(t *testing.T) {
	before := Snapshot{"b": {Index: "1"}, "a": {Index: "1"}, "gone": {Working: "x"}, "same": {Working: "y"}}
	after := Snapshot{"b": {Index: "2"}, "a": {Index: "1"}, "new": {Working: "z"}, "same": {Working: "y"}}
	want := []string{"b", "gone", "new"}
	if got := changed(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := changed(before, before); len(got) != 0 {
		t.Fatalf("identical snapshots changed: %v", got)
	}
}

func TestCaptureAndChangedObserveEveryChangeClass(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	b, err := Capture(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Head) != 40 || b.Root == "" {
		t.Fatalf("baseline: %+v", b)
	}
	if got, err := Changed(ctx, b); err != nil || len(got) != 0 {
		t.Fatalf("clean tree: %v %v", got, err)
	}
	write(t, dir, "a.txt", "modified")           // tracked modification
	write(t, dir, "new/untracked.txt", "new")    // untracked
	write(t, dir, "ignored.log", "ignored data") // newly created ignored file
	if err := os.Remove(filepath.Join(dir, "dir/b.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt") // staged
	got, err := Changed(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.txt", "dir/b.txt", "ignored.log", "new/untracked.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestChangedExcludesPreexistingDirtButSeesFurtherEdits(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	write(t, dir, "a.txt", "dirty before")
	b, err := Capture(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Changed(ctx, b); len(got) != 0 {
		t.Fatalf("pre-existing dirt attributed: %v", got)
	}
	write(t, dir, "a.txt", "dirty before and after")
	if got, _ := Changed(ctx, b); !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("further edit missed: %v", got)
	}
}

func TestChangedRenameReportsBothPaths(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	b, err := Capture(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "a.txt"), filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	if got, _ := Changed(ctx, b); !reflect.DeepEqual(got, []string{"a.txt", "renamed.txt"}) {
		t.Fatalf("got %v", got)
	}
}

func TestCaptureAndChangedFailuresAreExplicit(t *testing.T) {
	ctx := context.Background()
	if _, err := Capture(ctx, t.TempDir()); err == nil {
		t.Fatal("non-Git workspace accepted")
	}
	if _, err := Capture(ctx, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing workspace accepted")
	}
	// A repository without any commit has no HEAD to record.
	empty := t.TempDir()
	runGit(t, empty, "init", "-q")
	if _, err := Capture(ctx, empty); err == nil {
		t.Fatal("repository without HEAD accepted")
	}
	if _, err := Changed(ctx, Baseline{}); err == nil {
		t.Fatal("zero baseline accepted")
	}
	if _, err := Capture(nil, t.TempDir()); err == nil { //nolint:staticcheck
		t.Fatal("nil context accepted by Capture")
	}
	dir := repo(t)
	b, err := Capture(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Changed(cancelled, b); err == nil {
		t.Fatal("cancelled context did not fail inspection")
	}
}

func TestConfinedPathRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", "/abs", "../x", "a/../b", "a//b", "./a", ".git/config", "a/.GIT/x", "a\x00b", "\xff"} {
		if _, err := ConfinedPath(root, bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	got, err := ConfinedPath(root, "a/b.txt")
	if err != nil || got != filepath.Join(root, "a", "b.txt") {
		t.Fatalf("got %q %v", got, err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if _, err := ConfinedPath(root, "link/file"); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
}
