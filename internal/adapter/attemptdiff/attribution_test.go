package attemptdiff

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
)

func TestDiffTrackedModifiedStagedUntrackedDeletedAndModeOnly(t *testing.T) {
	dir := newRepo(t, map[string]string{"mod.txt": "a\nb\n", "staged.txt": "1\n", "gone.txt": "bye\n", "run.sh": "echo\n"})
	got := compare(t, newRecorder(t), dir, nil, func() {
		write(t, dir, "mod.txt", "a\nB\n")
		write(t, dir, "staged.txt", "2\n")
		gitIn(t, dir, "add", "staged.txt")
		write(t, dir, "new.txt", "fresh\n")
		_ = os.Remove(filepath.Join(dir, "gone.txt"))
		_ = os.Chmod(filepath.Join(dir, "run.sh"), 0o755)
	})
	files := byPath(got)
	want := map[string]string{"mod.txt": events.DiffModified, "staged.txt": events.DiffModified, "new.txt": events.DiffAdded, "gone.txt": events.DiffDeleted, "run.sh": events.DiffMode}
	if len(files) != len(want) {
		t.Fatalf("files = %v", paths(got))
	}
	for p, change := range want {
		if files[p].Change != change {
			t.Errorf("%s change = %q, want %q", p, files[p].Change, change)
		}
	}
	if !strings.Contains(files["mod.txt"].Patch, "-b\n+B\n") || files["mod.txt"].AddedLines != 1 || files["mod.txt"].DeletedLines != 1 {
		t.Errorf("mod.txt patch = %q", files["mod.txt"].Patch)
	}
	if files["run.sh"].Patch != "" || files["run.sh"].OmittedReason != "" {
		t.Errorf("mode-only change must carry no patch: %+v", files["run.sh"])
	}
	if got.Summary.Files != 5 || got.Summary.AddedLines < 2 {
		t.Errorf("summary = %+v", got.Summary)
	}
}

func TestDiffRenameIsDeletionPlusAddition(t *testing.T) {
	dir := newRepo(t, map[string]string{"old.txt": "same\n"})
	got := compare(t, newRecorder(t), dir, nil, func() { gitIn(t, dir, "mv", "old.txt", "new.txt") })
	files := byPath(got)
	if files["old.txt"].Change != events.DiffDeleted || files["new.txt"].Change != events.DiffAdded || len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
}

func TestDiffExcludesPreExistingDirtUntouchedByTheAttempt(t *testing.T) {
	dir := newRepo(t, map[string]string{"dirty.txt": "a\n", "target.txt": "t\n"})
	write(t, dir, "dirty.txt", "a\ndirt\n")
	write(t, dir, "scratch.txt", "untracked\n")
	got := compare(t, newRecorder(t), dir, nil, func() { write(t, dir, "target.txt", "t2\n") })
	if !reflect.DeepEqual(paths(got), []string{"target.txt"}) {
		t.Fatalf("files = %v, want only target.txt", paths(got))
	}
}

func TestDiffTwoSnapshotsOnlyContainTheLaterChange(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "one\n"})
	r := newRecorder(t)
	s1 := snap(t, r, dir)
	write(t, dir, "a.txt", "one\ntwo\n")
	s2 := snap(t, r, dir)
	write(t, dir, "a.txt", "one\ntwo\nthree\n")
	s3 := snap(t, r, dir)
	got, err := r.Diff(context.Background(), s2, s3, nil)
	if err != nil {
		t.Fatal(err)
	}
	patch := got.Files[0].Patch
	if !strings.Contains(patch, "+three") || strings.Contains(patch, "+two") {
		t.Fatalf("second diff must start from the first result: %q", patch)
	}
	first, _ := r.Diff(context.Background(), s1, s2, nil)
	if !strings.Contains(first.Files[0].Patch, "+two") {
		t.Fatalf("first patch = %q", first.Files[0].Patch)
	}
}

func TestDiffEolAndCleanFilterDivergenceFallsBackToScratchCopy(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "l1\nl2\n", ".gitattributes": "a.txt text eol=crlf\n"})
	write(t, dir, "a.txt", "l1\r\nl2\r\n")
	got := compare(t, newRecorder(t), dir, nil, func() { write(t, dir, "a.txt", "l1\r\nl2\r\nl3\r\n") })
	f := byPath(got)["a.txt"]
	if !strings.Contains(f.Patch, "+l3\r\n") || f.OmittedReason != "" {
		t.Fatalf("diverging working copy must be diffed from scratch: %+v", f)
	}
}

func TestDiffSymlinkComparesTargetTextAndNeverFollows(t *testing.T) {
	dir := newRepo(t, map[string]string{"real.txt": "secret body\n"})
	if err := os.Symlink("real.txt", filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	gitIn(t, dir, "add", "link")
	gitIn(t, dir, "commit", "-q", "-m", "link")
	got := compare(t, newRecorder(t), dir, nil, func() {
		_ = os.Remove(filepath.Join(dir, "link"))
		_ = os.Symlink("other.txt", filepath.Join(dir, "link"))
	})
	f := byPath(got)["link"]
	if f.Change != events.DiffModified || !strings.Contains(f.Patch, "-real.txt") || !strings.Contains(f.Patch, "+other.txt") || strings.Contains(f.Patch, "secret body") {
		t.Fatalf("symlink diff = %+v", f)
	}
}

func TestSnapshotWorksInLinkedWorktree(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "a\n"})
	linked := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-linked")
	gitIn(t, dir, "worktree", "add", "-q", "-b", "side", linked)
	t.Cleanup(func() { _ = os.RemoveAll(linked) })
	linked, _ = filepath.EvalSymlinks(linked)
	got := compare(t, newRecorder(t), linked, nil, func() { write(t, linked, "a.txt", "b\n") })
	if len(got.Files) != 1 || got.Files[0].Path != "a.txt" {
		t.Fatalf("files = %v", paths(got))
	}
}
