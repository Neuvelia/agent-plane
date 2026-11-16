package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unitFrom creates a worktree, applies edit and snapshots it onto a unit ref.
func unitFrom(t *testing.T, f fixture, node, name string, edit func(dir string)) string {
	t.Helper()
	h, err := f.mgr.Create(context.Background(), f.pre, runID, node)
	if err != nil {
		t.Fatal(err)
	}
	edit(h.Path())
	ref, err := UnitRef(runID, name)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := f.mgr.Snapshot(context.Background(), h, ref)
	if err != nil {
		t.Fatal(err)
	}
	return snap.Commit
}

func TestIntegrateDisjointUnitsMergeCleanly(t *testing.T) {
	f := newFixture(t)
	tipRef, _ := IntegratedRef(runID)
	u1 := unitFrom(t, f, nodeA, "one", func(d string) { writeFile(t, filepath.Join(d, "a.txt"), "from one\n") })
	u2 := unitFrom(t, f, nodeB, "two", func(d string) { writeFile(t, filepath.Join(d, "b.txt"), "from two\n") })

	r1, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, u1)
	if err != nil || r1.Conflict || r1.ChangedPaths != 1 {
		t.Fatalf("first integration: %+v %v", r1, err)
	}
	if gitIn(t, f.src, "rev-parse", tipRef) != r1.TipCommit {
		t.Fatal("tip ref must equal the reported tip")
	}
	if parents := gitIn(t, f.src, "rev-list", "--parents", "-n", "1", r1.TipCommit); len(strings.Fields(parents)) != 3 {
		t.Fatalf("an integration commit has the previous tip and the unit as parents: %s", parents)
	}
	r2, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, u2)
	if err != nil || r2.Conflict {
		t.Fatalf("second integration: %+v %v", r2, err)
	}
	if gitIn(t, f.src, "show", tipRef+":a.txt") != "from one" || gitIn(t, f.src, "show", tipRef+":b.txt") != "from two" {
		t.Fatal("integrated tree must contain both units' changes")
	}
	if gitIn(t, f.src, "rev-parse", "HEAD") != f.pre.BaseSHA {
		t.Fatal("the source branch must not move")
	}
}

func TestIntegrateSameLinesConflictsAndKeepsTip(t *testing.T) {
	f := newFixture(t)
	tipRef, _ := IntegratedRef(runID)
	u1 := unitFrom(t, f, nodeA, "one", func(d string) { writeFile(t, filepath.Join(d, "shared.txt"), "one\nTWO-A\nthree\n") })
	u2 := unitFrom(t, f, nodeB, "two", func(d string) { writeFile(t, filepath.Join(d, "shared.txt"), "one\nTWO-B\nthree\n") })

	r1, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, u1)
	if err != nil || r1.Conflict {
		t.Fatalf("first: %+v %v", r1, err)
	}
	tipBefore := gitIn(t, f.src, "rev-parse", tipRef)
	r2, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, u2)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Conflict || strings.Join(r2.ConflictPaths, ",") != "shared.txt" || r2.Omitted != 0 || r2.TipCommit != tipBefore {
		t.Fatalf("expected a shared.txt conflict at the unchanged tip: %+v", r2)
	}
	if gitIn(t, f.src, "rev-parse", tipRef) != tipBefore {
		t.Fatal("a conflict must not advance the tip ref")
	}
	if gitIn(t, f.src, "show", tipRef+":shared.txt") != "one\nTWO-A\nthree" {
		t.Fatal("the tip must still hold the first unit's content")
	}
}

func TestIntegrateConflictPathsAreBounded(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 70; i++ {
		writeFile(t, filepath.Join(f.src, "many", fmt.Sprintf("f%02d.txt", i)), "base\n")
	}
	gitIn(t, f.src, "add", ".")
	gitIn(t, f.src, "commit", "-q", "-m", "many")
	pre, err := Preflight(context.Background(), f.src, f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.pre = pre
	edit := func(label string) func(string) {
		return func(d string) {
			for i := 0; i < 70; i++ {
				writeFile(t, filepath.Join(d, "many", fmt.Sprintf("f%02d.txt", i)), label+"\n")
			}
		}
	}
	tipRef, _ := IntegratedRef(runID)
	u1 := unitFrom(t, f, nodeA, "one", edit("A"))
	u2 := unitFrom(t, f, nodeB, "two", edit("B"))
	if _, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, u1); err != nil {
		t.Fatal(err)
	}
	r, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, u2)
	if err != nil || !r.Conflict {
		t.Fatalf("%+v %v", r, err)
	}
	if len(r.ConflictPaths) != MaxConflictPaths || r.Omitted != 6 {
		t.Fatalf("expected %d paths and 6 omitted, got %d and %d", MaxConflictPaths, len(r.ConflictPaths), r.Omitted)
	}
	if r.ConflictPaths[0] != "many/f00.txt" {
		t.Fatalf("paths must be sorted: %v", r.ConflictPaths[:2])
	}
}

func TestIntegrateIsDeterministicAcrossRepetitions(t *testing.T) {
	var trees []string
	for i := 0; i < 20; i++ {
		f := newFixture(t)
		tipRef, _ := IntegratedRef(runID)
		u1 := unitFrom(t, f, nodeA, "one", func(d string) { writeFile(t, filepath.Join(d, "a.txt"), "from one\n") })
		u2 := unitFrom(t, f, nodeB, "two", func(d string) { writeFile(t, filepath.Join(d, "b.txt"), "from two\n") })
		u3 := unitFrom(t, f, nodeC, "three", func(d string) {
			writeFile(t, filepath.Join(d, "shared.txt"), "one\ntwo\nthree\nfour\n")
			writeFile(t, filepath.Join(d, "new", "c.txt"), "c\n")
		})
		for _, u := range []string{u1, u2, u3} {
			r, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, u)
			if err != nil || r.Conflict {
				t.Fatalf("run %d: %+v %v", i, r, err)
			}
		}
		trees = append(trees, gitIn(t, f.src, "rev-parse", tipRef+"^{tree}"))
	}
	for i, tree := range trees {
		if tree != trees[0] {
			t.Fatalf("run %d produced tree %s, run 0 produced %s", i, tree, trees[0])
		}
	}
}

func TestIntegrateStartsFromBaseWhenTipIsAbsent(t *testing.T) {
	f := newFixture(t)
	tipRef, _ := IntegratedRef(runID)
	empty := unitFrom(t, f, nodeA, "noop", func(string) {}) // a unit that changed nothing
	r, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, empty)
	if err != nil || r.Conflict || r.ChangedPaths != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if gitIn(t, f.src, "rev-parse", tipRef+"^{tree}") != gitIn(t, f.src, "rev-parse", f.pre.BaseSHA+"^{tree}") {
		t.Fatal("integrating a no-op unit leaves the base tree")
	}
}

func TestIntegrateRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	u := unitFrom(t, f, nodeA, "one", func(d string) { writeFile(t, filepath.Join(d, "a.txt"), "x\n") })
	tipRef, _ := IntegratedRef(runID)
	unitRef, _ := UnitRef(runID, "one")
	for _, bad := range []string{"refs/heads/main", unitRef, "HEAD", ""} {
		_, err := f.mgr.Integrate(context.Background(), f.pre, bad, u)
		wantCode(t, err, CodeInvalidInput)
	}
	for _, bad := range []string{"HEAD", "not-a-sha", ""} {
		_, err := f.mgr.Integrate(context.Background(), f.pre, tipRef, bad)
		wantCode(t, err, CodeInvalidInput)
	}
	_, err := f.mgr.Integrate(context.Background(), Source{Root: "rel", BaseSHA: f.pre.BaseSHA}, tipRef, u)
	wantCode(t, err, CodeInvalidInput)
	if _, statErr := os.Stat(filepath.Join(f.src, ".git", "refs", "agentplane", runID, "integrated")); !os.IsNotExist(statErr) {
		t.Fatal("rejected input must not create the tip ref")
	}
}
