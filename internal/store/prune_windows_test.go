//go:build windows

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// F3.2 requirement 7 on Windows: a file another program holds open cannot be
// deleted (os.Open shares for read and write, not for delete), so deleting a run
// whose log is open fails with a sharing violation. Prune must report that run,
// skip it, never retry it, and still remove the others. The Unix equivalent is
// TestPruneContinuesAfterOneFailure, which injects the failure; here the
// operating system produces it.

// countingFS counts removal attempts per path.
type countingFS struct {
	osFS
	removals map[string]int
}

func (c *countingFS) RemoveAll(path string) error {
	c.removals[filepath.Base(path)]++
	return c.osFS.RemoveAll(path)
}

func TestPruneReportsAndSkipsARunWhoseLogIsHeldOpen(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	held, other1, other2 := finishedAgo(t, s, 2, 80), finishedAgo(t, s, 1, 90), finishedAgo(t, s, 3, 70)
	setRetention(t, s, Retention{MaxAgeDays: 1})
	counting := &countingFS{removals: map[string]int{}}
	s.fs = counting

	plan, err := s.PlanPrune(context.Background(), pruneNow)
	if err != nil || len(plan.Remove) != 3 {
		t.Fatalf("the plan must hold all three finished runs: %v %v", err, plan.Remove)
	}
	// Opened after planning, as a viewer opening the run would.
	log, err := os.Open(filepath.Join(s.Dir, held, eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	result, err := s.Prune(context.Background(), plan)
	if err == nil {
		t.Fatal("a deletion that failed must make the aggregate error non-nil")
	}
	if !reflect.DeepEqual(result.Removed, []string{other1, other2}) {
		t.Fatalf("the other runs must still be removed: %+v", result)
	}
	if len(result.Failed) != 1 || result.Failed[0].RunID != held || result.Failed[0].Err == nil || !errors.Is(err, result.Failed[0].Err) {
		t.Fatalf("the held run must be reported with its error: %+v / %v", result.Failed, err)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("a sharing violation is a failure, not a skipped lock: %v", result.Skipped)
	}
	if counting.removals[held] != 1 {
		t.Fatalf("a failed deletion must not be retried: %d attempts", counting.removals[held])
	}
	for _, id := range []string{other1, other2} {
		if exists(t, filepath.Join(s.Dir, id)) {
			t.Fatalf("%s must be gone", id)
		}
	}
	if !exists(t, filepath.Join(s.Dir, held, eventsFile)) {
		t.Fatal("the log that is held open cannot have been deleted")
	}
	// What a failed delete leaves behind is informative on this platform: Go's
	// RemoveAll removes the other files before it reports the one it could not.
	entries, readErr := os.ReadDir(filepath.Join(s.Dir, held))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Logf("files left in the run that could not be deleted: %v", names)
}

func TestPruneRemovesARunOnceItsLogIsNoLongerHeldOpen(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	id := finishedAgo(t, s, 1, 90)
	setRetention(t, s, Retention{MaxAgeDays: 1})
	log, err := os.Open(filepath.Join(s.Dir, id, eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil { // the viewer closed it before prune ran
		t.Fatal(err)
	}
	plan, err := s.PlanPrune(context.Background(), pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Prune(context.Background(), plan)
	if err != nil || !reflect.DeepEqual(result.Removed, []string{id}) || exists(t, filepath.Join(s.Dir, id)) {
		t.Fatalf("a closed log must not block the removal: %v %+v", err, result)
	}
}
