package history

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFinalizationStoresOmissionsAndPagesOriginalSequence(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "project", WorkspaceID: "workspace", DocumentID: "doc", GraphID: "graph", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	final := Finalization{
		RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 3,
		Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{"omitted":1}`),
		Events: []Event{
			{Sequence: 1, Projection: safeJSON(t, `{"sequence":1}`)},
			{Sequence: 3, Projection: safeJSON(t, `{"sequence":3}`)},
		},
		Omissions: []EventOmission{{Sequence: 2, Reason: "sensitive_identity"}},
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
	first, err := store.Events(ctx, "run", 0, 2)
	if err != nil || len(first.Events) != 1 || first.Events[0].Sequence != 1 || len(first.Omissions) != 1 || first.Omissions[0] != (OmissionRange{From: 2, To: 2, Reason: "sensitive_identity"}) || first.NextSequence != 2 || first.Watermark != 3 || !first.Finished {
		t.Fatalf("first page=%+v, %v", first, err)
	}
	second, err := store.Events(ctx, "run", first.NextSequence, 2)
	if err != nil || len(second.Events) != 1 || second.Events[0].Sequence != 3 || len(second.Omissions) != 0 || second.NextSequence != 3 || second.Watermark != 3 {
		t.Fatalf("second page=%+v, %v", second, err)
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatalf("same omissions should be idempotent: %v", err)
	}
	conflict := final
	conflict.Omissions = []EventOmission{{Sequence: 2, Reason: "not_retained"}}
	if err := store.Finalize(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed omission accepted: %v", err)
	}
}

func TestEventOmissionRejectsFreeFormAndCorruptStoredReasons(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	final := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Omissions: []EventOmission{{Sequence: 1, Reason: "known secret"}}}
	if err := store.Finalize(ctx, final); err == nil {
		t.Fatal("free-form event omission reason accepted")
	}
	final.Omissions[0].Reason = "sensitive_identity"
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	defer store.db.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF")
	if _, err := store.db.ExecContext(ctx, "UPDATE event_omissions SET reason='known secret' WHERE run_id='run'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(ctx, "run", 0, 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("corrupt event omission returned: %v", err)
	}
}

func TestEventPageReadKeepsOneSQLiteSnapshot(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var state StorageState
	if err := tx.QueryRowContext(ctx, "SELECT storage_state FROM runs WHERE run_id='run'").Scan(&state); err != nil || state != Active {
		t.Fatalf("initial snapshot=%s, %v", state, err)
	}
	other, err := sql.Open("sqlite", filepath.Join(store.dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	writer, err := other.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	projection := safeJSON(t, `{}`)
	if _, err := writer.ExecContext(ctx, "UPDATE runs SET storage_state='complete',ended_at=?,runtime_state='succeeded',safe_result_json=?,result_digest=? WHERE run_id='run'", formatHistoryTime(started.Add(time.Second)), projection.data, projection.digest); err != nil {
		writer.Rollback()
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, "INSERT INTO events(run_id,sequence,event_json,digest) VALUES('run',1,?,?)", projection.data, projection.digest); err != nil {
		writer.Rollback()
		t.Fatal(err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	page, err := store.eventsWithin(ctx, tx, "run", 0, 10)
	if err != nil || page.Finished || page.Watermark != 0 || len(page.Events) != 0 {
		t.Fatalf("page mixed pre/post-write snapshots: %+v, %v", page, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	current, err := store.Events(ctx, "run", 0, 10)
	if err != nil || !current.Finished || current.Watermark != 1 || len(current.Events) != 1 {
		t.Fatalf("current page=%+v, %v", current, err)
	}
}

func TestEventPageRejectsOversizedEncodedResponse(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	large := safeJSON(t, `{"text":"`+strings.Repeat("x", (3<<20)-100)+`"}`)
	if err := store.Finalize(ctx, Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 3, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: large}, {Sequence: 2, Projection: large}, {Sequence: 3, Projection: large}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(ctx, "run", 0, 3); !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("oversized page error=%v", err)
	}
	page, err := store.Events(ctx, "run", 0, 1)
	if err != nil || len(page.Events) != 1 || page.NextSequence != 1 {
		t.Fatalf("smaller page=%+v, %v", page, err)
	}
}

func TestFinalizationRejectsEventAndOmissionAtSameSequence(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	final := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}, Omissions: []EventOmission{{Sequence: 1, Reason: "sensitive_identity"}}}
	if err := store.Finalize(ctx, final); err == nil {
		t.Fatal("event and omission at the same original sequence were accepted")
	}
	run, err := store.Get(ctx, "run")
	if err != nil || run.StorageState != Active {
		t.Fatalf("failed finalization changed run: %+v, %v", run, err)
	}
}

func TestFinalizationRequiresSourceWatermarkAndExplainedSequenceGaps(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	base := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}, {Sequence: 3, Projection: safeJSON(t, `{}`)}}}
	if err := store.Finalize(ctx, base); err == nil {
		t.Fatal("events accepted without source watermark")
	}
	base.Watermark = 3
	if err := store.Finalize(ctx, base); err == nil {
		t.Fatal("unexplained middle sequence accepted")
	}
	base.Omissions = []EventOmission{{Sequence: 2, Reason: "sensitive_identity"}}
	if err := store.Finalize(ctx, base); err != nil {
		t.Fatalf("fully explained trace rejected: %v", err)
	}
	page, err := store.Events(ctx, "run", 0, 3)
	if err != nil || page.Watermark != 3 || page.NextSequence != 3 {
		t.Fatalf("stored trace page=%+v, %v", page, err)
	}
}
