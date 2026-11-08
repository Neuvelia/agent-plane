package history

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvidenceSweepDirectoryWalkRequestsBoundedBatches(t *testing.T) {
	reads := 0
	visited := 0
	err := walkEvidenceEntries(func(limit int) ([]os.DirEntry, error) {
		if limit <= 0 || limit > 128 {
			t.Fatalf("unbounded directory read: %d", limit)
		}
		reads++
		switch reads {
		case 1:
			return make([]os.DirEntry, 128), nil
		case 2:
			return make([]os.DirEntry, 1), io.EOF
		default:
			t.Fatal("walk read past EOF")
			return nil, io.EOF
		}
	}, func(os.DirEntry) error {
		visited++
		return nil
	})
	if err != nil || reads != 2 || visited != 129 {
		t.Fatalf("walk visited=%d reads=%d err=%v", visited, reads, err)
	}
}

func TestEvidenceSweepRemovesOrphansAcrossDirectoryBatches(t *testing.T) {
	store := testStore(t)
	if err := os.Mkdir(filepath.Join(store.dir, "evidence"), 0700); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	for i := 0; i < 257; i++ {
		name := fmt.Sprintf("%032x", i)
		if err := os.WriteFile(filepath.Join(store.dir, "evidence", name), []byte("orphan"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.sweepEvidenceCount(context.Background())
	if err != nil || removed != 257 {
		t.Fatalf("sweep removed=%d, %v", removed, err)
	}
	entries, err := os.ReadDir(filepath.Join(store.dir, "evidence"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("orphan files remain=%d, %v", len(entries), err)
	}
}

func launchEvidenceRun(t *testing.T, store *Store, id string, started time.Time) {
	t.Helper()
	if err := store.Launch(context.Background(), LaunchRecord{RunID: id, ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestManagedEvidenceSurvivesRestartAndReadsVerifiedChunks(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	content, err := ParseSafeEvidence(bytes.Repeat([]byte("ab"), 4<<20), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "run", AttemptID: "a1", NodeRunID: "n1", Kind: "diff", CreatedAt: started, Content: &content}); err != nil {
		t.Fatal(err)
	}
	first, err := store.Evidence(ctx, "e1", 0, 1<<20)
	if err != nil || first.Availability != "available" || first.TotalLength != 8<<20 || len(first.Data) != 1<<20 || first.NextOffset != 1<<20 || first.End {
		t.Fatalf("first chunk=%+v, %v", first, err)
	}
	if !bytes.Equal(first.Data, content.Bytes()[:1<<20]) {
		t.Fatal("first evidence chunk differs")
	}
	last, err := store.Evidence(ctx, "e1", 7<<20, 1<<20)
	if err != nil || !last.End || last.NextOffset != 8<<20 || !bytes.Equal(last.Data, content.Bytes()[7<<20:]) || last.Digest != first.Digest {
		t.Fatalf("last chunk length=%d, end=%v, next=%d, err=%v", len(last.Data), last.End, last.NextOffset, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.Evidence(ctx, "e1", 0, 16)
	if err != nil || !bytes.Equal(again.Data, []byte("abababababababab")) {
		t.Fatalf("reopened chunk=%q, %v", again.Data, err)
	}
}

func TestEmptyManagedEvidenceIsAvailableAndDistinctFromUnavailable(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	empty, err := ParseSafeEvidence([]byte{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "empty", RunID: "run", AttemptID: "a1", Kind: "preimage", CreatedAt: started, Content: &empty}); err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "missing", RunID: "run", AttemptID: "a1", Kind: "preimage", CreatedAt: started, OmissionReason: "preimage_missing"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	available, err := reopened.Evidence(ctx, "empty", 0, 16)
	if err != nil || available.Availability != "available" || available.TotalLength != 0 || len(available.Data) != 0 || available.NextOffset != 0 || !available.End || available.Digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty evidence=%+v, %v", available, err)
	}
	missing, err := reopened.Evidence(ctx, "missing", 0, 16)
	if err != nil || missing.Availability != "unavailable" || missing.OmissionReason != "preimage_missing" || missing.Digest != "" {
		t.Fatalf("missing evidence=%+v, %v", missing, err)
	}
}

func TestEvidenceRejectsCorruptOrUnapprovedOmissionReason(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started, OmissionReason: "preimage_missing"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "secret", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started, OmissionReason: "known secret"}); err == nil {
		t.Fatal("free-form evidence reason accepted")
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	defer store.db.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF")
	for _, reason := range []string{"known secret", strings.Repeat("x", (8<<20)+1)} {
		if _, err := store.db.ExecContext(ctx, "UPDATE managed_evidence SET omission_reason=? WHERE evidence_id='e1'", reason); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Evidence(ctx, "e1", 0, 16); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("corrupt omission reason length=%d returned: %v", len(reason), err)
		}
	}
}

func TestManagedEvidenceRejectsSecretsAndTampering(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if _, err := ParseSafeEvidence([]byte("before known-secret after"), []string{"known-secret"}); err == nil {
		t.Fatal("known secret accepted into evidence")
	}
	content, err := ParseSafeEvidence([]byte("safe evidence"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started, Content: &content}); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := store.db.QueryRowContext(ctx, "SELECT relative_path FROM managed_evidence WHERE evidence_id='e1'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.dir, path), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Evidence(ctx, "e1", 0, 100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("tampered evidence returned: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.dir, path)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.dir, path)); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
	if _, err := store.Evidence(ctx, "e1", 0, 100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("symlink evidence returned: %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "outside" {
		t.Fatalf("outside file changed: %q, %v", got, err)
	}
}

func TestManagedEvidenceUnavailableAndRunBudget(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "unavailable", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started, OmissionReason: "preimage_missing"}); err != nil {
		t.Fatal(err)
	}
	chunk, err := store.Evidence(ctx, "unavailable", 0, 100)
	if err != nil || chunk.Availability != "unavailable" || chunk.OmissionReason != "preimage_missing" || len(chunk.Data) != 0 {
		t.Fatalf("unavailable descriptor=%+v, %v", chunk, err)
	}
	if _, err := ParseSafeEvidence(bytes.Repeat([]byte("x"), (10<<20)+1), nil); err == nil {
		t.Fatal("overlarge evidence file accepted")
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO managed_evidence(evidence_id,run_id,attempt_id,kind,availability,created_at,relative_path,byte_length,digest) VALUES('budget','run','a1','diff','available',?,?,?,?)`, formatHistoryTime(started), "evidence/fake", 100<<20, "digest"); err != nil {
		t.Fatal(err)
	}
	small, err := ParseSafeEvidence([]byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "too-much", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started, Content: &small}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("run budget exceeded but accepted: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM managed_evidence WHERE evidence_id='too-much'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed capture persisted reference count=%d, %v", count, err)
	}
}

func TestPruneRemovesOnlyManagedEvidenceFiles(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	launchEvidenceRun(t, store, "run", old)
	content, err := ParseSafeEvidence([]byte("safe"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: old, Content: &content}); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := store.db.QueryRowContext(ctx, "SELECT relative_path FROM managed_evidence WHERE evidence_id='e1'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(ctx, Finalization{RunID: "run", EndedAt: old.Add(time.Hour), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}, Nodes: []NodeOutcome{{NodeRunID: "n1", NodeID: "node", Status: "succeeded", Projection: safeJSON(t, `{}`)}}, Attempts: []AttemptOutcome{{AttemptID: "a1", NodeRunID: "n1", Status: "succeeded", Projection: safeJSON(t, `{}`)}}}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "repository-file")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := store.Prune(ctx, PruneOptions{Now: now, RetentionDays: 30})
	if err != nil || result.RemovedRuns != 1 || result.RemovedFiles != 1 {
		t.Fatalf("prune managed file=%+v, %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(store.dir, path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed file still exists: %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "preserve" {
		t.Fatalf("repository file changed: %q, %v", got, err)
	}
}

func TestEvidenceReadRejectsDatabasePathEscape(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO managed_evidence(evidence_id,run_id,attempt_id,kind,availability,created_at,relative_path,byte_length,digest) VALUES('escape','run','a1','diff','available',?,?,?,?)`, formatHistoryTime(started), "../../outside", 3, "digest"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Evidence(ctx, "escape", 0, 3); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("escaped path accepted: %v", err)
	}
}

func TestEvidenceCaptureDatabaseFailureLeavesNoAdvertisedReference(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER reject_evidence BEFORE INSERT ON managed_evidence BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	content, err := ParseSafeEvidence([]byte("safe"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started, Content: &content}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("failed DB write advertised capture: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM managed_evidence WHERE evidence_id='e1'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed capture persisted reference count=%d, %v", count, err)
	}
	var entries []os.DirEntry
	entries, err = os.ReadDir(filepath.Join(store.dir, "evidence"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed capture leaked managed file: %d", len(entries))
	}
}

func TestReopenSweepsOnlyUnreferencedManagedEvidence(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	content, err := ParseSafeEvidence([]byte("kept"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started, Content: &content}); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(store.dir, "evidence", "0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(orphan, []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := os.Lstat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan survived reopen: %v", err)
	}
	chunk, err := reopened.Evidence(ctx, "e1", 0, 10)
	if err != nil || string(chunk.Data) != "kept" {
		t.Fatalf("referenced evidence lost: %q, %v", chunk.Data, err)
	}
}

func TestFinalizationRejectsUnboundManagedEvidenceAtomically(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	content, err := ParseSafeEvidence([]byte("safe"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e1", RunID: "run", AttemptID: "attempt-1", NodeRunID: "node-1", Kind: "preimage", CreatedAt: started, Content: &content}); err != nil {
		t.Fatal(err)
	}
	base := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}
	if err := store.Finalize(ctx, base); err == nil {
		t.Fatal("finalized run with evidence for an absent attempt")
	}
	run, err := store.Get(ctx, "run")
	if err != nil || run.StorageState != Active {
		t.Fatalf("failed finalization changed run: %+v, %v", run, err)
	}
	wrongNode := base
	wrongNode.Nodes = []NodeOutcome{{NodeRunID: "node-2", NodeID: "node", Status: "succeeded", Projection: safeJSON(t, `{}`)}}
	wrongNode.Attempts = []AttemptOutcome{{AttemptID: "attempt-1", NodeRunID: "node-2", Status: "succeeded", Projection: safeJSON(t, `{}`)}}
	if err := store.Finalize(ctx, wrongNode); err == nil {
		t.Fatal("finalized run with evidence bound to another node")
	}
	valid := base
	valid.Nodes = []NodeOutcome{{NodeRunID: "node-1", NodeID: "node", Status: "succeeded", Projection: safeJSON(t, `{}`)}}
	valid.Attempts = []AttemptOutcome{{AttemptID: "attempt-1", NodeRunID: "node-1", Status: "succeeded", Projection: safeJSON(t, `{}`)}}
	if err := store.Finalize(ctx, valid); err != nil {
		t.Fatalf("valid evidence binding rejected: %v", err)
	}
}

func TestFinalizationBindsNodeScopedEvidenceWithoutAttempt(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Now().UTC()
	launchEvidenceRun(t, store, "run", started)
	content, err := ParseSafeEvidence([]byte("node image"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "node-e", RunID: "run", NodeRunID: "node-1", Kind: "postimage", CreatedAt: started, Content: &content}); err != nil {
		t.Fatal(err)
	}
	base := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}
	if err := store.Finalize(ctx, base); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing node outcome accepted: %v", err)
	}
	wrong := base
	wrong.Nodes = []NodeOutcome{{NodeRunID: "other-node", NodeID: "work", Status: "succeeded", Projection: safeJSON(t, `{}`)}}
	if err := store.Finalize(ctx, wrong); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched node outcome accepted: %v", err)
	}
	run, err := store.Get(ctx, "run")
	if err != nil || run.StorageState != Active {
		t.Fatalf("failed finalization changed run: %+v %v", run, err)
	}
	valid := base
	valid.Nodes = []NodeOutcome{{NodeRunID: "node-1", NodeID: "work", Status: "succeeded", Projection: safeJSON(t, `{}`)}}
	if err := store.Finalize(ctx, valid); err != nil {
		t.Fatalf("node evidence binding rejected: %v", err)
	}
}
