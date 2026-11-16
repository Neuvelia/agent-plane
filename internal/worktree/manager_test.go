package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/gitstate"
)

const (
	runID = "0b6f1a2e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	nodeA = "11111111-1111-4111-8111-111111111111"
	nodeB = "22222222-2222-4222-8222-222222222222"
	nodeC = "33333333-3333-4333-8333-333333333333"
)

func TestNewManagerRootPermissionsAndKind(t *testing.T) {
	dir := canonical(t, t.TempDir())
	fresh := filepath.Join(dir, "a", "b", "root")
	if _, err := NewManager(fresh); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(fresh)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("a new root must be 0700: %v %v", info, err)
	}
	open := filepath.Join(dir, "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	wantCode(t, func() error { _, err := NewManager(open); return err }(), CodeWorktreeRoot)
	if err := os.Chmod(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(open); err != nil {
		t.Fatalf("an existing 0700 directory is accepted: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(open, link); err != nil {
		t.Fatal(err)
	}
	wantCode(t, func() error { _, err := NewManager(link); return err }(), CodeWorktreeRoot)
	file := filepath.Join(dir, "file")
	writeFile(t, file, "x")
	wantCode(t, func() error { _, err := NewManager(file); return err }(), CodeWorktreeRoot)
	wantCode(t, func() error { _, err := NewManager("relative"); return err }(), CodeWorktreeRoot)
}

func TestCreateMakesDetachedLinkedWorktreeAtBase(t *testing.T) {
	f := newFixture(t)
	h, err := f.mgr.Create(context.Background(), f.pre, runID, nodeA)
	if err != nil {
		t.Fatal(err)
	}
	if h.Path() != filepath.Join(f.mgr.Root(), "agentplane-"+runID+"-"+nodeA) {
		t.Fatalf("path derives only from validated ids: %s", h.Path())
	}
	if info, err := os.Lstat(filepath.Join(h.Path(), ".git")); err != nil || info.IsDir() {
		t.Fatalf(".git of a linked worktree is a file: %v %v", info, err)
	}
	if got := gitIn(t, h.Path(), "rev-parse", "HEAD"); got != f.pre.BaseSHA {
		t.Fatalf("worktree HEAD %s != base %s", got, f.pre.BaseSHA)
	}
	if out, err := exec.Command("/usr/bin/git", "-C", h.Path(), "symbolic-ref", "-q", "HEAD").CombinedOutput(); err == nil {
		t.Fatalf("HEAD must be detached, got %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(h.Path(), "a.txt")); string(b) != "a\n" {
		t.Fatalf("tracked content missing: %q", b)
	}
}

func TestCreateDoesNotRunPostCheckoutHook(t *testing.T) {
	f := newFixture(t)
	canary := filepath.Join(filepath.Dir(f.src), "hook-canary")
	hook := filepath.Join(f.src, ".git", "hooks", "post-checkout")
	writeFile(t, hook, "#!/bin/sh\ntouch '"+canary+"'\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	// Control: plain Git does run the hook, so the assertion below is not vacuous.
	gitIn(t, f.src, "worktree", "add", "-q", "--detach", filepath.Join(filepath.Dir(f.src), "control"), f.pre.BaseSHA)
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("control worktree did not run the hook; the test setup is invalid: %v", err)
	}
	if err := os.Remove(canary); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mgr.Create(context.Background(), f.pre, runID, nodeA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatal("Create must never run repository hooks")
	}
}

func TestCreateRefusesExistingAndSymlinkedTargets(t *testing.T) {
	f := newFixture(t)
	target := filepath.Join(f.mgr.Root(), "agentplane-"+runID+"-"+nodeA)
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := f.mgr.Create(context.Background(), f.pre, runID, nodeA)
	wantCode(t, err, CodeCreate)

	outside := t.TempDir()
	link := filepath.Join(f.mgr.Root(), "agentplane-"+runID+"-"+nodeB)
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	_, err = f.mgr.Create(context.Background(), f.pre, runID, nodeB)
	wantCode(t, err, CodeCreate)
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("a symlinked target must never be written through")
	}
}

func TestCreateRejectsHostileIdentifiersAndForeignSources(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"", "../x", "a/b", "a b", "x\x00", strings.Repeat("a", 65), "..", "."} {
		_, err := f.mgr.Create(context.Background(), f.pre, runID, id)
		wantCode(t, err, CodeInvalidInput)
		_, err = f.mgr.Create(context.Background(), f.pre, id, nodeA)
		wantCode(t, err, CodeInvalidInput)
	}
	_, err := f.mgr.Create(context.Background(), Source{Root: f.src, BaseSHA: "HEAD"}, runID, nodeA)
	wantCode(t, err, CodeInvalidInput)
	_, err = f.mgr.Create(context.Background(), Source{Root: "relative", BaseSHA: f.pre.BaseSHA}, runID, nodeA)
	wantCode(t, err, CodeInvalidInput)
	entries, _ := os.ReadDir(f.mgr.Root())
	if len(entries) != 0 {
		t.Fatalf("rejected input must not leave directories behind: %v", entries)
	}
}

func TestCreateRejectsManagerRootInsideSource(t *testing.T) {
	src := newSource(t)
	pre, err := Preflight(context.Background(), src, filepath.Join(filepath.Dir(src), "ok"))
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(filepath.Join(src, ".agentplane-worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Create(context.Background(), pre, runID, nodeA)
	wantCode(t, err, CodeWorktreeRoot)
}

func TestGitstateWorksOnLinkedWorktree(t *testing.T) {
	f := newFixture(t)
	h, err := f.mgr.Create(context.Background(), f.pre, runID, nodeA)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := gitstate.Capture(context.Background(), h.Path())
	if err != nil {
		t.Fatalf("F1.8 baseline must work in a linked worktree: %v", err)
	}
	writeFile(t, filepath.Join(h.Path(), "a.txt"), "changed\n")
	writeFile(t, filepath.Join(h.Path(), "new.txt"), "n\n")
	changed, err := gitstate.Changed(context.Background(), baseline)
	if err != nil || strings.Join(changed, ",") != "a.txt,new.txt" {
		t.Fatalf("changed files inside the worktree: %v %v", changed, err)
	}
}

func TestSnapshotDoesNotMoveBranchIndexOrWorkingTree(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.src, ".gitignore"), "*.log\n")
	gitIn(t, f.src, "add", ".gitignore")
	gitIn(t, f.src, "commit", "-q", "-m", "ignore logs")
	pre, err := Preflight(context.Background(), f.src, f.root)
	if err != nil {
		t.Fatal(err)
	}
	h, err := f.mgr.Create(context.Background(), pre, runID, nodeA)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.Path(), "a.txt"), "edited\n")
	writeFile(t, filepath.Join(h.Path(), "added.txt"), "added\n")
	writeFile(t, filepath.Join(h.Path(), "debug.log"), "ignored\n")
	if err := os.Remove(filepath.Join(h.Path(), "b.txt")); err != nil {
		t.Fatal(err)
	}

	srcHead := gitIn(t, f.src, "rev-parse", "HEAD")
	srcBranch := gitIn(t, f.src, "symbolic-ref", "HEAD")
	srcStatus := gitIn(t, f.src, "status", "--porcelain=v1", "--untracked-files=all")
	srcIndex := gitIn(t, f.src, "ls-files", "--stage")

	ref, _ := UnitRef(runID, "worker")
	snap, err := f.mgr.Snapshot(context.Background(), h, ref)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ChangedPaths != 3 {
		t.Fatalf("a.txt edited, added.txt added, b.txt deleted = 3 changed paths, got %d", snap.ChangedPaths)
	}
	if gitIn(t, f.src, "rev-parse", ref) != snap.Commit || gitIn(t, f.src, "rev-parse", ref+"^") != pre.BaseSHA {
		t.Fatal("unit ref must point at a commit whose only parent is the base")
	}
	if gitIn(t, f.src, "show", ref+":a.txt") != "edited" || gitIn(t, f.src, "show", ref+":added.txt") != "added" {
		t.Fatal("snapshot lacks the unit's content")
	}
	if names := gitIn(t, f.src, "ls-tree", "-r", "--name-only", ref); strings.Contains(names, "debug.log") || strings.Contains(names, "b.txt") {
		t.Fatalf("ignored and deleted files must not be in the snapshot: %s", names)
	}
	if gitIn(t, f.src, "rev-parse", "HEAD") != srcHead || gitIn(t, f.src, "symbolic-ref", "HEAD") != srcBranch ||
		gitIn(t, f.src, "status", "--porcelain=v1", "--untracked-files=all") != srcStatus || gitIn(t, f.src, "ls-files", "--stage") != srcIndex {
		t.Fatal("Snapshot changed the source branch, index or working tree")
	}
}

func TestSnapshotRejectsNestedRepositoryAndBadRefs(t *testing.T) {
	f := newFixture(t)
	h, err := f.mgr.Create(context.Background(), f.pre, runID, nodeA)
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := UnitRef(runID, "w")
	for _, bad := range []string{"refs/heads/main", "HEAD", "refs/agentplane/x/../y", ""} {
		_, err := f.mgr.Snapshot(context.Background(), h, bad)
		wantCode(t, err, CodeInvalidInput)
	}
	nested := filepath.Join(h.Path(), "vendor", "lib")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, nested, "init", "-q")
	writeFile(t, filepath.Join(nested, "f.txt"), "x\n")
	gitIn(t, nested, "add", ".")
	gitIn(t, nested, "commit", "-q", "-m", "inner")
	_, err = f.mgr.Snapshot(context.Background(), h, ref)
	wantCode(t, err, CodeNestedRepo)
	if out, err := exec.Command("/usr/bin/git", "-C", f.src, "rev-parse", "--verify", "--quiet", ref).CombinedOutput(); err == nil {
		t.Fatalf("a rejected snapshot must not create a ref: %s", out)
	}
}

func TestRemoveOnlyOwnLiveHandles(t *testing.T) {
	f := newFixture(t)
	h, err := f.mgr.Create(context.Background(), f.pre, runID, nodeA)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.Path(), "untracked.txt"), "u\n")
	ref, _ := UnitRef(runID, "w")
	if _, err := f.mgr.Snapshot(context.Background(), h, ref); err != nil {
		t.Fatal(err)
	}

	other, err := NewManager(filepath.Join(filepath.Dir(f.src), "other-root"))
	if err != nil {
		t.Fatal(err)
	}
	wantCode(t, other.Remove(context.Background(), h), CodeInvalidInput)
	wantCode(t, f.mgr.Remove(context.Background(), nil), CodeInvalidInput)
	if _, err := os.Stat(h.Path()); err != nil {
		t.Fatal("a foreign manager must not remove the worktree")
	}

	if err := f.mgr.Remove(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.Path()); !os.IsNotExist(err) {
		t.Fatal("worktree directory must be gone, including untracked files")
	}
	if n := strings.Count(gitIn(t, f.src, "worktree", "list", "--porcelain"), "worktree "); n != 1 {
		t.Fatalf("only the source worktree should remain registered, got %d", n)
	}
	if gitIn(t, f.src, "rev-parse", ref) == "" {
		t.Fatal("unit refs are the deliverable and must survive removal")
	}
	wantCode(t, f.mgr.Remove(context.Background(), h), CodeInvalidInput) // already removed
	if _, err := os.Stat(f.src); err != nil || gitIn(t, f.src, "rev-parse", "HEAD") != f.pre.BaseSHA {
		t.Fatal("the source must be untouched")
	}
}
