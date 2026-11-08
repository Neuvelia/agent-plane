package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPinUsesRunRevisionAndInvalidatesListCursor(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b"} {
		if err := store.Launch(ctx, LaunchRecord{RunID: id, ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.List(ctx, ListQuery{Limit: 1})
	if err != nil || page.NextCursor == "" {
		t.Fatalf("initial page=%+v, %v", page, err)
	}
	revision, err := store.Pin(ctx, "a", 0, true)
	if err != nil || revision != 1 {
		t.Fatalf("pin revision=%d, %v", revision, err)
	}
	if _, err := store.Pin(ctx, "a", 0, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale pin accepted: %v", err)
	}
	run, err := store.Get(ctx, "a")
	if err != nil || !run.Pinned || run.Revision != 1 {
		t.Fatalf("pinned run=%+v, %v", run, err)
	}
	if _, err := store.List(ctx, ListQuery{Limit: 1, Cursor: page.NextCursor}); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("pin did not stale list cursor: %v", err)
	}
}

func TestPruneCommitsBoundedBatchesBeforeLaterFailure(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	for i := 0; i < 129; i++ {
		id := fmt.Sprintf("run-%03d", i)
		if err := store.Launch(ctx, LaunchRecord{RunID: id, ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: old, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	content, err := ParseSafeEvidence([]byte("pruned evidence"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "first-evidence", RunID: "run-000", AttemptID: "attempt", Kind: "diff", CreatedAt: old, Content: &content}); err != nil {
		t.Fatal(err)
	}
	var evidencePath string
	if err := store.db.QueryRowContext(ctx, "SELECT relative_path FROM managed_evidence WHERE evidence_id='first-evidence'").Scan(&evidencePath); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 129; i++ {
		id := fmt.Sprintf("run-%03d", i)
		final := Finalization{RunID: id, EndedAt: old.Add(time.Hour), RuntimeState: "succeeded", Watermark: 1,
			Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}
		if i == 0 {
			final.Nodes = []NodeOutcome{{NodeRunID: "node-run", NodeID: "node", Status: "succeeded", Projection: safeJSON(t, `{}`)}}
			final.Attempts = []AttemptOutcome{{AttemptID: "attempt", NodeRunID: "node-run", Status: "accepted", Projection: safeJSON(t, `{}`)}}
		}
		if err := store.Finalize(ctx, final); err != nil {
			t.Fatalf("finalize %s: %v", id, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER fail_later_prune BEFORE DELETE ON runs WHEN OLD.run_id='run-128' BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := store.Prune(ctx, PruneOptions{Now: now, RetentionDays: 30})
	if !errors.Is(err, ErrUnavailable) || result.RemovedRuns != 128 || result.RemovedFiles != 1 {
		t.Fatalf("partial prune=%+v, %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(store.dir, evidencePath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan from committed batch retained: %v", err)
	}
	if _, err := store.Get(ctx, "run-000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("committed batch not removed: %v", err)
	}
	if _, err := store.Get(ctx, "run-128"); err != nil {
		t.Fatalf("failed batch removed: %v", err)
	}
}

func TestPruneRetrySweepsOrphanAfterCancelledCleanupWithNoEligibleRuns(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	launchEvidenceRun(t, store, "old", old)
	content, err := ParseSafeEvidence([]byte("orphan after commit"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "old", AttemptID: "a1", Kind: "diff", CreatedAt: old, Content: &content}); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := store.db.QueryRowContext(ctx, "SELECT relative_path FROM managed_evidence WHERE evidence_id='e1'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	// Reproduce the durable state after a prune batch commits and its cleanup
	// is cancelled: the row is gone, while its private evidence file remains.
	if _, err := store.db.ExecContext(ctx, "DELETE FROM runs WHERE run_id='old'"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Prune(cancelled, PruneOptions{Now: now, RetentionDays: 30}); err == nil {
		t.Fatal("cancelled prune unexpectedly succeeded")
	}
	result, err := store.Prune(ctx, PruneOptions{Now: now, RetentionDays: 30})
	if err != nil || result.RemovedRuns != 0 || result.RemovedFiles != 1 {
		t.Fatalf("retry prune=%+v, %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(store.dir, path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan survived retry: %v", err)
	}
}

func TestPruneKeepsActivePinnedPendingAndRecentRuns(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	for _, id := range []string{"old-complete", "old-interrupted", "old-active", "old-pinned", "old-pending", "recent"} {
		started := old
		if id == "recent" {
			started = now.Add(-2 * 24 * time.Hour)
		}
		if err := store.Launch(ctx, LaunchRecord{RunID: id, ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
			t.Fatal(err)
		}
		if id != "old-active" && id != "old-interrupted" {
			if err := store.Finalize(ctx, Finalization{RunID: id, EndedAt: started.Add(time.Hour), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE runs SET storage_state='interrupted' WHERE run_id='old-interrupted'`); err != nil {
		t.Fatal(err)
	}
	if revision, err := store.Pin(ctx, "old-pinned", 1, true); err != nil || revision != 2 {
		t.Fatalf("pin old run=%d, %v", revision, err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE runs SET pending_approval=1 WHERE run_id='old-pending'`); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "repository-file")
	if err := os.WriteFile(outside, []byte("must remain"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := store.Prune(ctx, PruneOptions{Now: now, RetentionDays: 30})
	if err != nil || result.RemovedRuns != 1 || result.SkippedActive != 1 || result.SkippedInterrupted != 1 || result.SkippedPinned != 1 || result.SkippedPending != 1 {
		t.Fatalf("prune result=%+v, %v", result, err)
	}
	for _, id := range []string{"old-complete"} {
		if _, err := store.Get(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("eligible run %s retained: %v", id, err)
		}
	}
	for _, id := range []string{"old-interrupted", "old-active", "old-pinned", "old-pending", "recent"} {
		if _, err := store.Get(ctx, id); err != nil {
			t.Fatalf("protected run %s removed: %v", id, err)
		}
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "must remain" {
		t.Fatalf("repository file changed: %q, %v", got, err)
	}
}
