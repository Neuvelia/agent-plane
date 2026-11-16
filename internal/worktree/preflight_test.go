package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckGitVersion(t *testing.T) {
	for _, ok := range []string{"git version 2.38.0", "git version 2.43.0\n", "git version 2.50.1 (Apple Git-155)", "git version 3.0.0", "git version 2.100.0"} {
		if err := checkGitVersion(ok); err != nil {
			t.Errorf("%q must be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"git version 2.37.9", "git version 1.9.0", "git version 2.3.0", "", "not git", "git version two"} {
		wantCode(t, checkGitVersion(bad), CodeGitVersion)
	}
}

func TestPreflightAcceptsCleanRepoAndIgnoresUntracked(t *testing.T) {
	src := newSource(t)
	writeFile(t, filepath.Join(src, "untracked.txt"), "u\n") // not carried into worktrees; documented limitation
	root := filepath.Join(filepath.Dir(src), "wt")
	got, err := Preflight(context.Background(), src, root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != src || got.BaseSHA != gitIn(t, src, "rev-parse", "HEAD") {
		t.Fatalf("unexpected source: %+v", got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("preflight must not create the worktree root")
	}
}

func TestPreflightRejectsDirtyTrackedFiles(t *testing.T) {
	src := newSource(t)
	writeFile(t, filepath.Join(src, "a.txt"), "changed\n")
	err := func() error {
		_, err := Preflight(context.Background(), src, filepath.Join(filepath.Dir(src), "wt"))
		return err
	}()
	e := wantCode(t, err, CodeDirtyTracked)
	if !strings.Contains(e.Message, `"a.txt"`) || !strings.Contains(e.Message, "commit or stash") {
		t.Fatalf("message must name the file and the remedy: %s", e.Message)
	}
	// A staged change is just as invisible to a worktree.
	gitIn(t, src, "checkout", "--", "a.txt")
	writeFile(t, filepath.Join(src, "b.txt"), "staged\n")
	gitIn(t, src, "add", "b.txt")
	_, err = Preflight(context.Background(), src, filepath.Join(filepath.Dir(src), "wt"))
	wantCode(t, err, CodeDirtyTracked)
}

func TestPreflightRejectsFilterDriverWithoutRunningIt(t *testing.T) {
	src := newSource(t)
	canary := filepath.Join(filepath.Dir(src), "canary")
	gitIn(t, src, "config", "filter.secretdriver.smudge", "touch "+canary+"; cat")
	_, err := Preflight(context.Background(), src, filepath.Join(filepath.Dir(src), "wt"))
	e := wantCode(t, err, CodeFilterDriver)
	if !strings.Contains(e.Message, `"secretdriver"`) {
		t.Fatalf("message must name the driver: %s", e.Message)
	}
	if strings.Contains(e.Message, "touch") || strings.Contains(e.Message, canary) {
		t.Fatalf("the driver command must not be echoed: %s", e.Message)
	}
	if _, statErr := os.Stat(canary); !os.IsNotExist(statErr) {
		t.Fatal("preflight must never execute a filter driver")
	}
}

func TestPreflightRejectsFilterDriverDefinedInIncludedConfig(t *testing.T) {
	src := newSource(t)
	extra := filepath.Join(filepath.Dir(src), "extra.gitconfig")
	writeFile(t, extra, "[filter \"viainclude\"]\n\tprocess = git-lfs filter-process\n")
	gitIn(t, src, "config", "include.path", extra)
	_, err := Preflight(context.Background(), src, filepath.Join(filepath.Dir(src), "wt"))
	e := wantCode(t, err, CodeFilterDriver)
	if !strings.Contains(e.Message, `"viainclude"`) {
		t.Fatalf("included driver not reported: %s", e.Message)
	}
}

func TestPreflightRejectsSubmodules(t *testing.T) {
	src := newSource(t)
	head := gitIn(t, src, "rev-parse", "HEAD")
	gitIn(t, src, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/lib")
	gitIn(t, src, "commit", "-q", "-m", "gitlink")
	_, err := Preflight(context.Background(), src, filepath.Join(filepath.Dir(src), "wt"))
	wantCode(t, err, CodeSubmodule)
}

func TestPreflightRejectsBareMissingHeadAndNonRootWorkspaces(t *testing.T) {
	dir := canonical(t, t.TempDir())
	root := filepath.Join(dir, "wt")

	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, empty, "init", "-q")
	_, err := Preflight(context.Background(), empty, root)
	wantCode(t, err, CodeWorkspace) // no commit yet

	bare := filepath.Join(dir, "bare.git")
	gitIn(t, dir, "init", "-q", "--bare", bare)
	_, err = Preflight(context.Background(), bare, root)
	wantCode(t, err, CodeWorkspace)

	src := newSource(t)
	_, err = Preflight(context.Background(), filepath.Join(src, "sub"), root) // does not exist
	wantCode(t, err, CodeWorkspace)
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = Preflight(context.Background(), filepath.Join(src, "sub"), root) // inside, not the root
	wantCode(t, err, CodeWorkspace)
	_, err = Preflight(context.Background(), "relative/path", root)
	wantCode(t, err, CodeWorkspace)
}

func TestPreflightRejectsUnsafeWorktreeRoots(t *testing.T) {
	src := newSource(t)
	_, err := Preflight(context.Background(), src, "relative/root")
	wantCode(t, err, CodeWorktreeRoot)
	_, err = Preflight(context.Background(), src, filepath.Join(src, ".agentplane", "worktrees"))
	wantCode(t, err, CodeWorktreeRoot)
	_, err = Preflight(context.Background(), src, src)
	wantCode(t, err, CodeWorktreeRoot)

	// A root outside the source that is a symlink into it resolves inside.
	if err := os.Mkdir(filepath.Join(src, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(src), "link")
	if err := os.Symlink(filepath.Join(src, "inner"), link); err != nil {
		t.Fatal(err)
	}
	_, err = Preflight(context.Background(), src, filepath.Join(link, "wt"))
	wantCode(t, err, CodeWorktreeRoot)
}

func TestPreflightIsReadOnly(t *testing.T) {
	src := newSource(t)
	before := gitIn(t, src, "for-each-ref")
	status := gitIn(t, src, "status", "--porcelain=v1", "--untracked-files=all")
	if _, err := Preflight(context.Background(), src, filepath.Join(filepath.Dir(src), "wt")); err != nil {
		t.Fatal(err)
	}
	if gitIn(t, src, "for-each-ref") != before || gitIn(t, src, "status", "--porcelain=v1", "--untracked-files=all") != status {
		t.Fatal("preflight changed the source repository")
	}
}
