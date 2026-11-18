package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Retention is applied only by an explicit prune (F3.2 requirement 7, ADR-0027).
// Prune deletes whole run directories of finished runs, oldest first, and never
// anything else: an unterminated run (live or interrupted) is kept whatever its
// age because resume (F3.3) needs it, and so is a locked, corrupt or
// unsupported one. It does not touch Git worktrees or refs/agentplane/*, which
// F3.1 owns.

// PruneCandidate is one run a prune plan removes.
type PruneCandidate struct {
	RunID     string
	StartedAt time.Time
}

// PrunePlan is the runs a prune would remove, oldest first. A dry run shows the
// plan and stops; Prune executes the same plan.
type PrunePlan struct {
	Remove []PruneCandidate
}

// PruneFailure is a run whose deletion failed. It is reported and skipped, never
// retried in a loop.
type PruneFailure struct {
	RunID string
	Err   error
}

// PruneResult reports what Prune did. Skipped holds runs whose lock was taken by
// a writer between planning and deleting; they are left alone.
type PruneResult struct {
	Removed []string
	Skipped []string
	Failed  []PruneFailure
}

// prunable is what selection needs to know about one run.
type prunable struct {
	runID      string
	startedAt  time.Time
	finishedAt time.Time // when the run ended; its age is measured from here
	deletable  bool
	bytes      int64
}

// PlanPrune decides which runs the store's retention removes at now. A store
// with no retention, or only zero limits, plans nothing. A store written by a
// newer build is refused (ReadFormat), as it is read-only here.
func (s Store) PlanPrune(ctx context.Context, now time.Time) (PrunePlan, error) {
	format, err := s.ReadFormat()
	if err != nil {
		return PrunePlan{}, err
	}
	limits := format.Retention
	if limits == nil || (limits.MaxRuns == 0 && limits.MaxAgeDays == 0 && limits.MaxTotalBytes == 0) {
		return PrunePlan{}, nil
	}
	runs, err := s.pruneInventory(ctx, limits.MaxTotalBytes > 0)
	if err != nil {
		return PrunePlan{}, err
	}
	byID := make(map[string]prunable, len(runs))
	for _, r := range runs {
		byID[r.runID] = r
	}
	var plan PrunePlan
	for _, id := range selectForPrune(runs, *limits, now) {
		plan.Remove = append(plan.Remove, PruneCandidate{RunID: id, StartedAt: byID[id].startedAt})
	}
	return plan, nil
}

// pruneInventory lists every run directory prune may reason about, oldest
// first. Only real directories named like a run ID that hold an events.jsonl
// count: a symlink or a foreign directory is never read, counted or deleted.
// A run still being created is skipped. measure adds each run's size.
func (s Store) pruneInventory(ctx context.Context, measure bool) ([]prunable, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read store directory: %w", err)
	}
	var runs []prunable
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !ValidRunID(entry.Name()) {
			continue
		}
		run, ok, err := s.inspectForPrune(entry.Name(), measure)
		if err != nil {
			return nil, err
		}
		if ok {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return olderRun(runs[i], runs[j]) })
	return runs, nil
}

func olderRun(a, b prunable) bool {
	if !a.startedAt.Equal(b.startedAt) {
		return a.startedAt.Before(b.startedAt)
	}
	return a.runID < b.runID
}

// inspectForPrune classifies one run. ok is false for a directory that is not a
// run (no regular events.jsonl) or one that is still being created.
func (s Store) inspectForPrune(runID string, measure bool) (run prunable, ok bool, err error) {
	dir := filepath.Join(s.Dir, runID)
	info, statErr := os.Lstat(filepath.Join(dir, eventsFile))
	if statErr != nil || !info.Mode().IsRegular() {
		return prunable{}, false, nil
	}
	summary, listed := s.summarize(runID)
	if !listed {
		return prunable{}, false, nil
	}
	run = prunable{runID: runID, startedAt: summary.StartedAt, finishedAt: summary.StartedAt, deletable: terminalStatus(summary.Status) && summary.Error == ""}
	if summary.FinishedAt != nil {
		run.finishedAt = *summary.FinishedAt
	}
	if measure {
		if run.bytes, err = directoryBytes(dir); err != nil {
			return prunable{}, false, fmt.Errorf("measure run %s: %w", runID, err)
		}
	}
	return run, true, nil
}

// directoryBytes sums the regular files below dir without following symlinks.
func directoryBytes(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// selectForPrune applies the limits to runs (oldest first) and returns the IDs
// to remove, oldest first. Only a deletable run is ever chosen. Order of the
// limits: age first, then the run count (every run, deletable or not, uses the
// budget), then total bytes. A limit that cannot be met because only kept runs
// remain simply leaves the excess.
func selectForPrune(runs []prunable, limits Retention, now time.Time) []string {
	marked := make(map[string]bool, len(runs))
	if limits.MaxAgeDays > 0 {
		markExpired(runs, now.AddDate(0, 0, -limits.MaxAgeDays), marked)
	}
	if limits.MaxRuns > 0 {
		markOverCount(runs, limits.MaxRuns, marked)
	}
	if limits.MaxTotalBytes > 0 {
		markOverBytes(runs, limits.MaxTotalBytes, marked)
	}
	var ids []string
	for _, r := range runs {
		if marked[r.runID] {
			ids = append(ids, r.runID)
		}
	}
	return ids
}

// markExpired marks every deletable run that finished before cutoff. Age counts
// from the end of the run: a run that took days is not expired the moment it ends.
func markExpired(runs []prunable, cutoff time.Time, marked map[string]bool) {
	for _, r := range runs {
		if r.deletable && r.finishedAt.Before(cutoff) {
			marked[r.runID] = true
		}
	}
}

// markOverCount marks the oldest deletable runs until at most limit runs
// remain; runs that cannot be deleted still count toward the limit.
func markOverCount(runs []prunable, limit int, marked map[string]bool) {
	remaining := len(runs) - len(marked)
	for _, r := range runs {
		if remaining <= limit {
			return
		}
		if r.deletable && !marked[r.runID] {
			marked[r.runID] = true
			remaining--
		}
	}
}

// markOverBytes marks the oldest deletable runs until the runs that remain fit
// in limit bytes.
func markOverBytes(runs []prunable, limit int64, marked map[string]bool) {
	var total int64
	for _, r := range runs {
		if !marked[r.runID] {
			total += r.bytes
		}
	}
	for _, r := range runs {
		if total <= limit {
			return
		}
		if r.deletable && !marked[r.runID] {
			marked[r.runID] = true
			total -= r.bytes
		}
	}
}

// Prune removes the planned runs. Each is locked first: a held lock means a
// writer owns it, so it is skipped. A deletion that fails (a Windows sharing
// violation, a permission error) is recorded and the rest continue. The error
// is nil only when nothing failed. A cancelled ctx stops before the next run and
// is part of the error.
func (s Store) Prune(ctx context.Context, plan PrunePlan) (PruneResult, error) {
	var result PruneResult
	var failures []error
	for _, candidate := range plan.Remove {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		err := s.removeRun(candidate.RunID)
		switch {
		case err == nil:
			result.Removed = append(result.Removed, candidate.RunID)
		case errors.Is(err, ErrRunLocked):
			result.Skipped = append(result.Skipped, candidate.RunID)
		default:
			result.Failed = append(result.Failed, PruneFailure{RunID: candidate.RunID, Err: err})
			failures = append(failures, fmt.Errorf("remove run %s: %w", candidate.RunID, err))
		}
	}
	return result, errors.Join(failures...)
}

// removeRun deletes one run directory under its exclusive lock. The lock handle
// is released before the delete because Windows cannot delete an open file; a
// finished run's ID cannot be reused by a writer (creation is exclusive).
func (s Store) removeRun(runID string) error {
	dir := filepath.Join(s.Dir, runID)
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a run directory", runID)
	}
	lock, err := acquireRunLock(filepath.Join(dir, lockFile), false)
	if err != nil {
		return err
	}
	if err := lock.release(); err != nil {
		return err
	}
	return s.ops().RemoveAll(dir)
}
