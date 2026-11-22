package attemptdiff

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/gitstate"
)

const secret = "s3cr3t-token-VALUE"

func TestDiffRedactsSecretAcrossLinesPathAndCutBoundary(t *testing.T) {
	dir := newRepo(t, nil)
	filler := strings.Repeat("filler line of text\n", 820) // ~16.4 KiB, so the cut falls near the secret
	got := compare(t, newRecorder(t), dir, markerRedactor{secret}, func() {
		write(t, dir, "cfg.txt", filler+"token="+secret+"\n"+filler+secret+"\n")
		write(t, dir, "name-"+secret+".txt", "inside "+secret+"\n")
	})
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("secret leaked: %s", raw)
	}
	for _, f := range got.Files {
		if !f.Redacted {
			t.Errorf("%s must be marked redacted", f.Path)
		}
	}
	if len(got.Files) != 2 {
		t.Fatalf("files = %v", paths(got))
	}
}

func TestDiffRedactedTextStillValidUTF8(t *testing.T) {
	dir := newRepo(t, nil)
	got := compare(t, newRecorder(t), dir, markerRedactor{secret}, func() { write(t, dir, "u.txt", "żółć "+secret+" ✓\n") })
	f := byPath(got)["u.txt"]
	if !f.Redacted || strings.Contains(f.Patch, secret) || f.Patch == "" {
		t.Fatalf("u.txt = %+v", f)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDiffSecretToSecretEditShowsNoVisibleChangeButHashesDiffer(t *testing.T) {
	dir := newRepo(t, map[string]string{"k.txt": "key=" + secret + "\n"})
	got := compare(t, newRecorder(t), dir, markerRedactor{secret}, func() { write(t, dir, "k.txt", "key="+secret+"\n\n") })
	f := byPath(got)["k.txt"]
	if f.BeforeSHA256 == f.AfterSHA256 {
		t.Fatal("hashes must differ")
	}
}

type recordingRunner struct {
	mu   sync.Mutex
	seen []string
}

func (rr *recordingRunner) run(ctx context.Context, dir string, opts gitstate.RunOptions, args ...string) (gitstate.RunResult, error) {
	rr.mu.Lock()
	rr.seen = append(rr.seen, strings.Join(args, " "))
	rr.mu.Unlock()
	return gitstate.Run(ctx, dir, opts, args...)
}

func TestOnlyAllowlistedGitSubcommands(t *testing.T) {
	rr := &recordingRunner{}
	r, err := open(rr.run)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	dir := newRepo(t, map[string]string{"a.txt": "a\n"})
	got := compare(t, r, dir, nil, func() { write(t, dir, "a.txt", "b\n"); write(t, dir, "n.txt", "n\n") })
	if len(got.Files) != 2 || len(rr.seen) == 0 {
		t.Fatalf("files=%v seen=%v", paths(got), rr.seen)
	}
	for _, call := range rr.seen {
		sub := strings.Fields(call)[0]
		if sub != "ls-files" && sub != "cat-file" && sub != "rev-parse" {
			t.Errorf("forbidden git call %q", call)
		}
	}
	if _, err := r.run(context.Background(), dir, 1<<10, "diff", "HEAD"); err == nil {
		t.Error("diff must be refused by the allowlist")
	}
	if _, err := r.run(context.Background(), dir, 1<<10, "cat-file", "-p", "HEAD"); err == nil {
		t.Error("cat-file other than blob must be refused")
	}
}

func TestNoHookFilterTextconvDiffDriverOrFsmonitorRuns(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "a\n"})
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho x >> '"+marker+"'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".gitattributes", "*.txt filter=x diff=x\n")
	for _, kv := range [][2]string{{"filter.x.clean", script}, {"filter.x.smudge", script}, {"filter.x.process", script},
		{"diff.x.textconv", script}, {"diff.external", script}, {"core.fsmonitor", script}, {"core.hooksPath", filepath.Dir(script)}, {"core.attributesFile", script}} {
		gitIn(t, dir, "config", kv[0], kv[1])
	}
	r := newRecorder(t)
	before := snap(t, r, dir)
	write(t, dir, "a.txt", "changed\n")
	write(t, dir, "new.txt", "n\n")
	after := snap(t, r, dir)
	if _, err := r.Diff(context.Background(), before, after, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a repository-configured program ran (marker present: %v)", err)
	}
}

func TestCaptureWritesNothingIntoGitDir(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "a\n"})
	linked := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-wt")
	gitIn(t, dir, "worktree", "add", "-q", "-b", "wt", linked)
	t.Cleanup(func() { _ = os.RemoveAll(linked) })
	linked, _ = filepath.EvalSymlinks(linked)
	for _, workspace := range []string{dir, linked} {
		digest := gitDirDigest(t, dir)
		compare(t, newRecorder(t), workspace, nil, func() { write(t, workspace, "a.txt", "z\n"); write(t, workspace, "n.txt", "n\n") })
		if gitDirDigest(t, dir) != digest {
			t.Fatalf("capture changed .git for %s", workspace)
		}
	}
}

func gitDirDigest(t *testing.T, repo string) [32]byte {
	t.Helper()
	h := sha256.New()
	_ = filepath.WalkDir(filepath.Join(repo, ".git"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		data, _ := os.ReadFile(path)
		h.Write([]byte(path + info.ModTime().String()))
		h.Write(data)
		return nil
	})
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func TestScratchPermissionsAndRemovedOnClose(t *testing.T) {
	r, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	dir := newRepo(t, nil)
	compare(t, r, dir, nil, func() { write(t, dir, "u.txt", "untracked\n") })
	info, err := os.Stat(r.scratch.dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("scratch dir: %v %v", info, err)
	}
	entries, _ := os.ReadDir(r.scratch.dir)
	if len(entries) == 0 {
		t.Fatal("expected a scratch copy of the untracked file")
	}
	for _, e := range entries {
		fi, _ := e.Info()
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", e.Name(), fi.Mode())
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.scratch.dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("scratch must be removed on Close")
	}
}

func TestSymlinkedAncestorIsNotFollowed(t *testing.T) {
	dir := newRepo(t, map[string]string{"sub/a.txt": "a\n"})
	outside := t.TempDir()
	write(t, outside, "a.txt", "outside\n")
	got := compare(t, newRecorder(t), dir, nil, func() {
		_ = os.RemoveAll(filepath.Join(dir, "sub"))
		if err := os.Symlink(outside, filepath.Join(dir, "sub")); err != nil {
			t.Skip("symlinks unavailable")
		}
	})
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "outside") {
		t.Fatalf("content outside the workspace was read: %s", raw)
	}
}

type failingRunner struct{ class gitstate.RunClass }

func (f failingRunner) run(context.Context, string, gitstate.RunOptions, ...string) (gitstate.RunResult, error) {
	return gitstate.RunResult{}, &gitstate.RunError{Class: f.class, ExitCode: -1}
}

func TestGitFailureClassesMapToDiagnosticCodes(t *testing.T) {
	dir := newRepo(t, nil)
	cases := map[gitstate.RunClass]string{
		gitstate.RunStart: events.DiffGitUnavailable, gitstate.RunTimeout: events.DiffTimeout,
		gitstate.RunExit: events.DiffGitFailed, gitstate.RunOutputLimit: events.DiffGitFailed,
	}
	for class, want := range cases {
		r, err := open(failingRunner{class}.run)
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.Snapshot(context.Background(), dir)
		_ = r.Close()
		if DiagnosticCode(err) != want {
			t.Errorf("%s -> %q, want %q", class, DiagnosticCode(err), want)
		}
		if strings.Contains(err.Error(), dir) {
			t.Errorf("error leaks a path: %v", err)
		}
	}
}

func TestConcurrentSnapshotsShareScratchSafely(t *testing.T) {
	dir := newRepo(t, nil)
	write(t, dir, "u.txt", "shared untracked content\n")
	r := newRecorder(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Snapshot(context.Background(), dir); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
