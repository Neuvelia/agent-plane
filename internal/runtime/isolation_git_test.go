package runtime_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
	"agentplane/internal/testenv"
	"agentplane/internal/worktree"
)

// gitWorkspaces composes internal/worktree behind the runtime port, as the CLI
// does. Tests live in runtime_test so production runtime never imports it.
type gitWorkspaces struct {
	root string
	mgr  *worktree.Manager
}

type gitHandle struct{ h *worktree.Handle }

func (g gitHandle) Path() string { return g.h.Path() }

func (g *gitWorkspaces) Preflight(ctx context.Context, source string) (runtime.WorkspaceSource, error) {
	src, err := worktree.Preflight(ctx, source, g.root)
	if err != nil {
		return runtime.WorkspaceSource{}, err
	}
	if g.mgr == nil {
		if g.mgr, err = worktree.NewManager(g.root); err != nil {
			return runtime.WorkspaceSource{}, err
		}
	}
	return runtime.WorkspaceSource{Root: src.Root, BaseSHA: src.BaseSHA}, nil
}

func (g *gitWorkspaces) Create(ctx context.Context, s runtime.WorkspaceSource, runID, nodeRunID string) (runtime.Workspace, error) {
	h, err := g.mgr.Create(ctx, worktree.Source{Root: s.Root, BaseSHA: s.BaseSHA}, runID, nodeRunID)
	if err != nil {
		return nil, err
	}
	return gitHandle{h}, nil
}

func (g *gitWorkspaces) Snapshot(ctx context.Context, w runtime.Workspace, ref string) (runtime.WorkspaceSnapshot, error) {
	res, err := g.mgr.Snapshot(ctx, w.(gitHandle).h, ref)
	return runtime.WorkspaceSnapshot{Commit: res.Commit, ChangedPaths: res.ChangedPaths}, err
}

func (g *gitWorkspaces) Integrate(ctx context.Context, s runtime.WorkspaceSource, tipRef, unit string) (runtime.WorkspaceIntegration, error) {
	res, err := g.mgr.Integrate(ctx, worktree.Source{Root: s.Root, BaseSHA: s.BaseSHA}, tipRef, unit)
	return runtime.WorkspaceIntegration{Conflict: res.Conflict, ConflictPaths: res.ConflictPaths, Omitted: res.Omitted, TipCommit: res.TipCommit, ChangedPaths: res.ChangedPaths}, err
}

func (g *gitWorkspaces) Remove(ctx context.Context, w runtime.Workspace) error {
	return g.mgr.Remove(ctx, w.(gitHandle).h)
}

func (*gitWorkspaces) UnitRef(runID, nodeID string) (string, error) {
	return worktree.UnitRef(runID, nodeID)
}

func (*gitWorkspaces) IntegratedRef(runID string) (string, error) {
	return worktree.IntegratedRef(runID)
}

func isoGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	testenv.RequireExecutable(t, "/usr/bin/git")
	c := exec.Command("/usr/bin/git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// isoWrite and isoRead report with Errorf: adapters call them from worker
// goroutines, where a Fatal would strand the run.
func isoWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Errorf("writing %s: %v", name, err)
	}
}

func isoRead(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Errorf("reading %s: %v", name, err)
	}
	return string(b)
}

// isoRepo is a committed repository: a.txt, a three-line b.txt and shared.txt.
func isoRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isoGit(t, dir, "init", "-q")
	isoWrite(t, dir, "a.txt", "a\n")
	isoWrite(t, dir, "b.txt", "l1\nl2\nl3\n")
	isoWrite(t, dir, "shared.txt", "one\ntwo\nthree\n")
	isoGit(t, dir, "add", ".")
	isoGit(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func newGitWorkspaces(t *testing.T) *gitWorkspaces {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &gitWorkspaces{root: filepath.Join(root, "worktrees")}
}

// sourceFingerprint captures everything the runtime must leave alone: HEAD, the
// index bytes, git status including untracked files, and every working file.
func sourceFingerprint(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintln(&b, isoGit(t, dir, "rev-parse", "HEAD"), isoGit(t, dir, "symbolic-ref", "-q", "HEAD"))
	fmt.Fprintln(&b, isoGit(t, dir, "status", "--porcelain=v1", "--untracked-files=all"))
	index, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "index %x\n", sha256.Sum256(index))
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			fmt.Fprintf(&b, "%s %x\n", path, sha256.Sum256(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func gitOptions(w runtime.WorkspaceManager, policy runtime.RetainPolicy) runtime.Options {
	options := isolationOptions(w)
	options.MaxConcurrency = 4
	options.RetainWorktrees = policy
	return options
}

// editAdapter applies edits (relative path -> content) per task and attempt.
func editAdapter(t *testing.T, edit func(t *testing.T, in agent.AgentExecutionInput, attempt int) bool) (*loopAdapter, func(string) int) {
	t.Helper()
	a := newLoopAdapter()
	var mu sync.Mutex
	counts := map[string]int{}
	a.produce = func(_ context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		mu.Lock()
		counts[in.Task]++
		attempt := counts[in.Task]
		mu.Unlock()
		ok := edit(t, in, attempt)
		return agent.AgentExecutionOutput{RawMetadata: []byte(fmt.Sprintf(`{"ok":%t}`, ok))}, nil
	}
	return a, func(task string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[task]
	}
}

func TestIsolationParallelSameFileHasNoRace(t *testing.T) {
	src := isoRepo(t)
	before := sourceFingerprint(t, src)
	var mu sync.Mutex
	arrived, release := 0, make(chan struct{})
	var overlapped, foreign []string
	paths := map[string]string{}
	a, _ := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, _ int) bool {
		own := in.Task + " owns this file\n"
		isoWrite(t, in.Workspace, "shared.txt", own)
		mu.Lock()
		paths[in.Task] = in.Workspace
		arrived++
		if arrived == 2 {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
			mu.Lock()
			overlapped = append(overlapped, in.Task)
			mu.Unlock()
		case <-time.After(10 * time.Second):
		}
		if got := isoRead(t, in.Workspace, "shared.txt"); got != own {
			mu.Lock()
			foreign = append(foreign, in.Task)
			mu.Unlock()
		}
		return true
	})
	result, err := runtime.Run(context.Background(), unitGraph(src, []string{"a", "b"}, "a", "b"), nil, compose(a), gitOptions(newGitWorkspaces(t), runtime.RetainAlways))
	if err != nil {
		t.Fatal(err)
	}
	if len(overlapped) != 2 || len(foreign) != 0 {
		t.Fatalf("attempts overlapped=%v saw a foreign write=%v", overlapped, foreign)
	}
	if paths["a"] == paths["b"] || paths["a"] == src || paths["b"] == src {
		t.Fatalf("units must run in distinct worktrees outside the source: %v", paths)
	}
	for task, path := range paths {
		if got := isoRead(t, path, "shared.txt"); got != task+" owns this file\n" {
			t.Fatalf("worktree of %s holds %q", task, got)
		}
	}
	if after := sourceFingerprint(t, src); after != before {
		t.Fatalf("the source checkout changed:\n%s\n---\n%s", before, after)
	}
	if result.Failure == nil || result.Failure.Kind != run.FailureNeedsIntervention {
		t.Fatalf("both units rewrote the same lines, so the second must conflict: %+v", result.Failure)
	}
}

func TestIsolationSourceCheckoutIsUntouchedAndRefsHoldTheResult(t *testing.T) {
	src := isoRepo(t)
	isoWrite(t, src, "scratch.log", "untracked user file\n")
	before := sourceFingerprint(t, src)
	a, _ := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, _ int) bool {
		isoWrite(t, in.Workspace, in.Task+".txt", "edited by "+in.Task+"\n")
		return true
	})
	options := gitOptions(newGitWorkspaces(t), "")
	result, err := runtime.Run(context.Background(), unitGraph(src, []string{"a", "b"}, "a", "b"), nil, compose(a), options)
	if err != nil || result.State != run.RunSucceeded || result.Isolation == nil {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	if after := sourceFingerprint(t, src); after != before {
		t.Fatalf("the source checkout changed:\n%s\n---\n%s", before, after)
	}
	iso := result.Isolation
	if got := isoGit(t, src, "rev-parse", iso.IntegratedRef); got != iso.IntegratedCommit {
		t.Fatalf("ref %s is %s, result says %s", iso.IntegratedRef, got, iso.IntegratedCommit)
	}
	if iso.BaseSHA != isoGit(t, src, "rev-parse", "HEAD") {
		t.Fatalf("base %s", iso.BaseSHA)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if got := isoGit(t, src, "show", iso.IntegratedCommit+":"+name); got != "edited by "+strings.TrimSuffix(name, ".txt") {
			t.Fatalf("integrated %s = %q", name, got)
		}
	}
	if len(iso.Units) != 2 || len(iso.Retained) != 0 {
		t.Fatalf("units=%+v retained=%+v", iso.Units, iso.Retained)
	}
	released := releases(result)
	if released["a"] != (releaseFact{"removed", "integrated"}) || released["b"] != (releaseFact{"removed", "integrated"}) {
		t.Fatalf("releases %v", released)
	}
	for _, e := range eventsOf(result, events.WorkspacePrepared) {
		if _, statErr := os.Stat(e.Payload.(events.WorkspacePreparedPayload).Path); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("an integrated worktree is still on disk: %v", statErr)
		}
	}
}

func TestIsolationScopedRetryProtectsAcceptedSiblingFiles(t *testing.T) {
	src := isoRepo(t)
	d := unitGraph(src, []string{"a", "b", "c", "d"}, "a", "b", "c", "d")
	d.Nodes["c"].Loop.MaxAttempts = 3
	a, count := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, attempt int) bool {
		switch in.Task {
		case "a":
			isoWrite(t, in.Workspace, "a.txt", "A\n")
		case "b":
			isoWrite(t, in.Workspace, "b.txt", "B\nl2\nl3\n")
		case "d":
			isoWrite(t, in.Workspace, "d.txt", "D\n")
		case "c":
			if attempt == 2 {
				if got := isoRead(t, in.Workspace, "c.txt"); got != "C1\n" {
					t.Errorf("the correction attempt must see the rejected attempt's files, got %q", got)
				}
			}
			isoWrite(t, in.Workspace, "c.txt", fmt.Sprintf("C%d\n", attempt))
			isoWrite(t, in.Workspace, "b.txt", "l1\nl2\nC\n")
			return attempt == 2
		}
		return true
	})
	result, err := runtime.Run(context.Background(), d, nil, compose(a), gitOptions(newGitWorkspaces(t), ""))
	if err != nil || result.State != run.RunSucceeded || result.Isolation == nil {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	for task, want := range map[string]int{"a": 1, "b": 1, "c": 2, "d": 1} {
		if got := count(task); got != want {
			t.Fatalf("%s ran %d times, want %d", task, got, want)
		}
	}
	iso := result.Isolation
	unitCommit := map[string]string{}
	for _, u := range iso.Units {
		unitCommit[u.NodeID] = u.Commit
	}
	if got := isoGit(t, src, "show", unitCommit["b"]+":b.txt"); got != "B\nl2\nl3" {
		t.Fatalf("B's unit commit holds %q", got)
	}
	wantTree := map[string]string{"a.txt": "A", "b.txt": "B\nl2\nC", "c.txt": "C2", "d.txt": "D"}
	for name, want := range wantTree {
		if got := isoGit(t, src, "show", iso.IntegratedCommit+":"+name); got != want {
			t.Fatalf("integrated %s = %q, want %q", name, got, want)
		}
	}
}

func TestIsolationIntegrationDeterministic(t *testing.T) {
	var trees []string
	for i := 0; i < 20; i++ {
		src := isoRepo(t)
		a, _ := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, _ int) bool {
			isoWrite(t, in.Workspace, in.Task+".txt", "edited by "+in.Task+"\n")
			return true
		})
		result, err := runtime.Run(context.Background(), unitGraph(src, []string{"a", "b"}, "a", "b"), nil, compose(a), gitOptions(newGitWorkspaces(t), runtime.RetainNever))
		if err != nil || result.State != run.RunSucceeded {
			t.Fatalf("run %d: err=%v failure=%+v", i, err, result.Failure)
		}
		trees = append(trees, isoGit(t, src, "rev-parse", result.Isolation.IntegratedCommit+"^{tree}"))
	}
	for i, tree := range trees {
		if tree != trees[0] {
			t.Fatalf("run %d produced tree %s, run 0 produced %s", i, tree, trees[0])
		}
	}
}

func TestIsolationRealConflictKeepsTipAndRetainsTheWorktree(t *testing.T) {
	src := isoRepo(t)
	a, count := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, _ int) bool {
		isoWrite(t, in.Workspace, "shared.txt", "one\n"+strings.ToUpper(in.Task)+"2\nthree\n")
		return true
	})
	result, err := runtime.Run(context.Background(), unitGraph(src, []string{"a", "b"}, "a", "b"), nil, compose(a), gitOptions(newGitWorkspaces(t), ""))
	if err != nil || result.State != run.RunFailed || result.Failure.Kind != run.FailureNeedsIntervention || result.Failure.NodeID != "b" {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	if count("a") != 1 || count("b") != 1 {
		t.Fatalf("a conflict reran a unit: a=%d b=%d", count("a"), count("b"))
	}
	conflict := eventsOf(result, events.WorkspaceConflict)
	if len(conflict) != 1 {
		t.Fatalf("%d conflict events", len(conflict))
	}
	payload := conflict[0].Payload.(events.WorkspaceConflictPayload)
	if fmt.Sprint(payload.Paths) != "[shared.txt]" {
		t.Fatalf("conflict paths %v", payload.Paths)
	}
	iso := result.Isolation
	if got := isoGit(t, src, "rev-parse", iso.IntegratedRef); got != iso.IntegratedCommit || got != payload.TipCommit {
		t.Fatalf("tip moved: ref=%s result=%s conflict tip=%s", got, iso.IntegratedCommit, payload.TipCommit)
	}
	if got := isoGit(t, src, "show", iso.IntegratedCommit+":shared.txt"); got != "one\nA2\nthree" {
		t.Fatalf("the integrated tree must hold only the first unit: %q", got)
	}
	if len(iso.Retained) != 1 || iso.Retained[0].NodeID != "b" || iso.Retained[0].Reason != "conflict" {
		t.Fatalf("retained %+v", iso.Retained)
	}
	if got := isoRead(t, iso.Retained[0].Path, "shared.txt"); got != "one\nB2\nthree\n" {
		t.Fatalf("the retained worktree must hold the conflicting unit's own content: %q", got)
	}
	if got := nodeResult(result, "a"); got.State != run.NodeSucceeded {
		t.Fatalf("the accepted unit changed: %+v", got)
	}
}

func TestIsolationDirtySourceFailsPreflight(t *testing.T) {
	src := isoRepo(t)
	isoWrite(t, src, "a.txt", "uncommitted edit\n")
	before := sourceFingerprint(t, src)
	workspaces := newGitWorkspaces(t)
	requirePreflightFailure(t, unitGraph(src, []string{"a"}, "a"), gitOptions(workspaces, ""), "uncommitted")
	if sourceFingerprint(t, src) != before {
		t.Fatal("a rejected preflight must not change the source checkout")
	}
	if _, err := os.Stat(workspaces.root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a rejected preflight created the worktree root: %v", err)
	}
}

func TestIsolationChangedFilesScopeChecksOnlyItsOwnWorktree(t *testing.T) {
	src := isoRepo(t)
	d := unitGraph(src, []string{"a", "b"}, "a", "b")
	for _, id := range []string{"a", "b"} {
		n := d.Nodes[id]
		n.Loop.Gate.Config = graph.NodeConfig{"check_id": "scope", "kind": "changed_files_scope", "input": "candidate", "mode": "deny", "workspace": src, "allowed_paths": []string{id + ".txt"}}
		d.Nodes[id] = n
	}
	a, _ := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, _ int) bool {
		isoWrite(t, in.Workspace, in.Task+".txt", "in scope\n")
		if in.Task == "a" {
			isoWrite(t, in.Workspace, "stray_a.txt", "out of scope\n")
		}
		return true
	})
	result, err := runtime.Run(context.Background(), d, nil, compose(a), gitOptions(newGitWorkspaces(t), ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eventsOf(result, events.GateCheck) {
		check := e.Payload.(events.CheckResult)
		var paths []string
		for _, evidence := range check.Evidence {
			if evidence.Path != "" {
				paths = append(paths, evidence.Path)
			}
		}
		switch e.NodeID {
		case "a":
			if check.Verdict != events.CheckRed || fmt.Sprint(paths) != "[stray_a.txt]" {
				t.Fatalf("a: verdict=%s paths=%v", check.Verdict, paths)
			}
		case "b":
			if check.Verdict != events.CheckGreen || len(paths) != 0 {
				t.Fatalf("b must not see a's file: verdict=%s paths=%v", check.Verdict, paths)
			}
		}
	}
	if nodeResult(result, "b").State != run.NodeSucceeded || nodeResult(result, "a").State != run.NodeFailed {
		t.Fatalf("states a=%s b=%s", nodeResult(result, "a").State, nodeResult(result, "b").State)
	}
}
