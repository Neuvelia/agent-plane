package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"agentplane/internal/domain/events"
)

var pruneNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

func daysAgo(days int) time.Time { return pruneNow.AddDate(0, 0, -days) }

func setRetention(t *testing.T, s Store, limits Retention) {
	t.Helper()
	data, err := json.Marshal(Format{StoreFormat: CurrentFormat, Retention: &limits})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Dir, dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, formatFile), data, fileMode); err != nil {
		t.Fatal(err)
	}
}

func finishedAgo(t *testing.T, s Store, n, days int) string {
	t.Helper()
	id := seededRunID(n)
	finishedRun(t, s, id, daysAgo(days))
	return id
}

func planIDs(t *testing.T, s Store) []string {
	t.Helper()
	plan, err := s.PlanPrune(pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range plan.Remove {
		ids = append(ids, c.RunID)
	}
	return ids
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

func TestNoRetentionConfiguredMeansPruneDoesNothing(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	finishedAgo(t, s, 1, 400)
	for name, limits := range map[string]*Retention{"no store.json retention": nil, "all limits zero": {}} {
		if limits != nil {
			setRetention(t, s, *limits)
		}
		if ids := planIDs(t, s); len(ids) != 0 {
			t.Fatalf("%s: plan %v", name, ids)
		}
	}
}

func TestPruneNeverRemovesAnUnterminatedRunWhateverItsAge(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	interrupted := seededRunID(1)
	write(t, s, interrupted, started(interrupted, daysAgo(900))) // no terminal event, no writer
	live := seededRunID(2)
	w := s.NewWriter([]byte("schema_version: 0\n"))
	if err := w.Append(started(live, daysAgo(800))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeWriter(t, w) })
	old := finishedAgo(t, s, 3, 700)
	setRetention(t, s, Retention{MaxAgeDays: 1, MaxRuns: 1, MaxTotalBytes: 1})
	if got := planIDs(t, s); !reflect.DeepEqual(got, []string{old}) {
		t.Fatalf("only the finished run may be planned, got %v", got)
	}
	plan, _ := s.PlanPrune(pruneNow)
	if _, err := s.Prune(plan); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{interrupted, live} {
		if !exists(t, filepath.Join(s.Dir, id)) {
			t.Fatalf("an unterminated run %s was removed", id)
		}
	}
}

func TestSelectForPruneHonoursEachLimitOldestFirst(t *testing.T) {
	run := func(id string, days int, deletable bool, bytes int64) prunable {
		return prunable{runID: id, startedAt: daysAgo(days), deletable: deletable, bytes: bytes}
	}
	// oldest first, as the inventory delivers them
	runs := []prunable{
		run("a", 90, true, 100), run("b", 60, false, 100), run("c", 30, true, 100), run("d", 10, true, 100), run("e", 1, true, 100),
	}
	cases := []struct {
		name   string
		limits Retention
		want   []string
	}{
		{"nothing configured", Retention{}, nil},
		{"age alone", Retention{MaxAgeDays: 45}, []string{"a"}},
		{"age spares a kept run", Retention{MaxAgeDays: 20}, []string{"a", "c"}},
		{"count alone, kept run uses the budget", Retention{MaxRuns: 3}, []string{"a", "c"}},
		{"count beyond what can be removed", Retention{MaxRuns: 1}, []string{"a", "c", "d", "e"}},
		{"bytes alone", Retention{MaxTotalBytes: 250}, []string{"a", "c", "d"}},
		{"age then count", Retention{MaxAgeDays: 45, MaxRuns: 3}, []string{"a", "c"}},
		{"all three", Retention{MaxAgeDays: 45, MaxRuns: 4, MaxTotalBytes: 200}, []string{"a", "c", "d"}},
		{"within every limit", Retention{MaxAgeDays: 365, MaxRuns: 9, MaxTotalBytes: 9999}, nil},
	}
	for _, c := range cases {
		if got := selectForPrune(runs, c.limits, pruneNow); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPruneHonoursMaxRunsMaxAgeAndMaxTotalBytesOnDisk(t *testing.T) {
	build := func() (Store, []string) {
		s := Store{Dir: t.TempDir()}
		ids := []string{finishedAgo(t, s, 1, 50), finishedAgo(t, s, 2, 40), finishedAgo(t, s, 3, 10), finishedAgo(t, s, 4, 5)}
		return s, ids
	}
	one := func() int64 {
		s, ids := build()
		n, err := directoryBytes(filepath.Join(s.Dir, ids[0]))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}()
	cases := []struct {
		name   string
		limits Retention
		keep   []int // indexes of the runs that survive
	}{
		{"max_age_days", Retention{MaxAgeDays: 30}, []int{2, 3}},
		{"max_runs", Retention{MaxRuns: 2}, []int{2, 3}},
		{"max_total_bytes", Retention{MaxTotalBytes: one * 2}, []int{2, 3}},
		{"combined", Retention{MaxAgeDays: 45, MaxRuns: 2}, []int{2, 3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, ids := build()
			setRetention(t, s, c.limits)
			plan, err := s.PlanPrune(pruneNow)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Prune(plan)
			if err != nil || len(result.Failed) != 0 {
				t.Fatalf("%v %+v", err, result)
			}
			for i, id := range ids {
				kept := false
				for _, k := range c.keep {
					kept = kept || k == i
				}
				if exists(t, filepath.Join(s.Dir, id)) != kept {
					t.Fatalf("run %d: kept=%v on disk=%v", i, kept, !kept)
				}
			}
			if exists(t, filepath.Join(s.Dir, formatFile)) == false {
				t.Fatal("store.json must survive a prune")
			}
		})
	}
}

func TestPlanIsReadOnlyAndMatchesWhatPruneRemoves(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, b := finishedAgo(t, s, 1, 90), finishedAgo(t, s, 2, 1)
	setRetention(t, s, Retention{MaxAgeDays: 30})
	plan, err := s.PlanPrune(pruneNow)
	if err != nil || len(plan.Remove) != 1 || plan.Remove[0].RunID != a {
		t.Fatalf("%v %+v", err, plan)
	}
	if !exists(t, filepath.Join(s.Dir, a)) || !exists(t, snapshotPath(s, b)) {
		t.Fatal("planning must delete and rewrite nothing")
	}
	result, err := s.Prune(plan)
	if err != nil || !reflect.DeepEqual(result.Removed, []string{a}) || exists(t, filepath.Join(s.Dir, a)) {
		t.Fatalf("%v %+v", err, result)
	}
}

func TestPruneNeverRemovesALockedCorruptOrUnsupportedRun(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	corrupt := seededRunID(1)
	finishedRun(t, s, corrupt, daysAgo(500))
	if err := os.WriteFile(filepath.Join(s.Dir, corrupt, eventsFile), []byte("{not json}\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(snapshotPath(s, corrupt)); err != nil {
		t.Fatal(err)
	}
	unsupported := seededRunID(2)
	futureSchemaRun(t, s, unsupported)
	locked := seededRunID(3)
	w := s.NewWriter([]byte("schema_version: 0\n"))
	for _, e := range []events.Event{started(locked, daysAgo(500)), finished(locked, 2, "succeeded", daysAgo(500))} {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { closeWriter(t, w) })
	setRetention(t, s, Retention{MaxAgeDays: 1})

	plan, err := s.PlanPrune(pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range plan.Remove {
		if c.RunID == corrupt || c.RunID == unsupported {
			t.Fatalf("a corrupt or unsupported run was planned: %v", plan.Remove)
		}
	}
	// The locked run reads as finished, so it is planned; the lock still stops it.
	result, err := s.Prune(plan)
	if err != nil || !reflect.DeepEqual(result.Skipped, []string{locked}) || len(result.Removed) != 0 {
		t.Fatalf("%v %+v", err, result)
	}
	for _, id := range []string{corrupt, unsupported, locked} {
		if !exists(t, filepath.Join(s.Dir, id)) {
			t.Fatalf("%s was removed", id)
		}
	}
}

func TestPruneSkipsForeignDirectoriesAndSymlinks(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	real := finishedAgo(t, s, 1, 90)
	foreign := filepath.Join(s.Dir, seededRunID(2)) // looks like a run ID, holds no events.jsonl
	if err := os.Mkdir(foreign, dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "notes.txt"), []byte("mine"), fileMode); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	finishedRun(t, Store{Dir: outside}, seededRunID(3), daysAgo(900))
	// Same ID as its target, so only the symlink rule (not a content mismatch) can protect it.
	link := filepath.Join(s.Dir, seededRunID(3))
	if err := os.Symlink(filepath.Join(outside, seededRunID(3)), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	setRetention(t, s, Retention{MaxAgeDays: 1})
	plan, err := s.PlanPrune(pruneNow)
	if err != nil || len(plan.Remove) != 1 || plan.Remove[0].RunID != real {
		t.Fatalf("%v %+v", err, plan)
	}
	if _, err := s.Prune(plan); err != nil {
		t.Fatal(err)
	}
	if !exists(t, filepath.Join(foreign, "notes.txt")) || !exists(t, link) || !exists(t, filepath.Join(outside, seededRunID(3), eventsFile)) {
		t.Fatal("a foreign directory, a symlink or its target was touched")
	}
}

// removalFS fails the removal of chosen runs.
type removalFS struct {
	osFS
	failing map[string]error
}

func (f removalFS) RemoveAll(path string) error {
	if err := f.failing[filepath.Base(path)]; err != nil {
		return err
	}
	return f.osFS.RemoveAll(path)
}

func TestPruneContinuesAfterOneFailure(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, b, c := finishedAgo(t, s, 1, 90), finishedAgo(t, s, 2, 80), finishedAgo(t, s, 3, 70)
	setRetention(t, s, Retention{MaxAgeDays: 1})
	sharing := errors.New("injected sharing violation")
	s.fs = removalFS{failing: map[string]error{b: sharing}}
	plan, err := s.PlanPrune(pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Prune(plan)
	if !errors.Is(err, sharing) {
		t.Fatalf("the aggregate error must carry the failure: %v", err)
	}
	if !reflect.DeepEqual(result.Removed, []string{a, c}) || len(result.Failed) != 1 || result.Failed[0].RunID != b {
		t.Fatalf("%+v", result)
	}
	if !exists(t, filepath.Join(s.Dir, b)) {
		t.Fatal("the failing run must still be on disk")
	}
}

func TestRetentionValuesAreValidatedAndANewerStoreIsRefused(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	finishedAgo(t, s, 1, 90)
	if err := os.WriteFile(filepath.Join(s.Dir, formatFile), []byte(`{"store_format":1,"retention":{"max_runs":-1}}`), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanPrune(pruneNow); err == nil {
		t.Fatal("a negative limit must be refused")
	}
	if err := os.WriteFile(filepath.Join(s.Dir, formatFile), []byte(`{"store_format":99,"retention":{"max_runs":1}}`), fileMode); err != nil {
		t.Fatal(err)
	}
	var formatErr *FormatError
	if _, err := s.PlanPrune(pruneNow); !errors.As(err, &formatErr) {
		t.Fatalf("a store from a newer build must not be pruned: %v", err)
	}
}
