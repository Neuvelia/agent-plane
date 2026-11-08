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

func TestFinalizationStoresLongOmissionRangeWithoutExpandedInput(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "long-range", started)
	final := Finalization{RunID: "long-range", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 10002,
		Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`),
		Events:         []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}, {Sequence: 10002, Projection: safeJSON(t, `{}`)}},
		OmissionRanges: []OmissionRange{{From: 2, To: 10001, Reason: "not_retained"}}}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
	page, err := store.Events(ctx, "long-range", 1, 256)
	if err != nil || len(page.Omissions) != 1 || page.Omissions[0] != (OmissionRange{From: 2, To: 257, Reason: "not_retained"}) || page.NextSequence != 257 {
		t.Fatalf("range page=%+v, %v", page, err)
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatalf("same range should be idempotent: %v", err)
	}
	equivalent := final
	equivalent.OmissionRanges = []OmissionRange{{From: 2, To: 5000, Reason: "not_retained"}, {From: 5001, To: 10001, Reason: "not_retained"}}
	if err := store.Finalize(ctx, equivalent); err != nil {
		t.Fatalf("equivalent range partition should be idempotent: %v", err)
	}
	changed := final
	changed.OmissionRanges = []OmissionRange{{From: 2, To: 10001, Reason: "sensitive_identity"}}
	if err := store.Finalize(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed reason accepted: %v", err)
	}
	changed = final
	changed.OmissionRanges = []OmissionRange{{From: 2, To: 10000, Reason: "not_retained"}, {From: 10001, To: 10001, Reason: "sensitive_identity"}}
	if err := store.Finalize(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed boundary accepted: %v", err)
	}
}

func TestFinalizationRangeCoverageValidation(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "range-invalid", started)
	base := Finalization{RunID: "range-invalid", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 5,
		Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}
	for _, tc := range []struct {
		name      string
		ranges    []OmissionRange
		omissions []EventOmission
	}{
		{"gap", []OmissionRange{{From: 2, To: 4, Reason: "not_retained"}}, nil},
		{"overlapping ranges", []OmissionRange{{From: 2, To: 4, Reason: "not_retained"}, {From: 4, To: 5, Reason: "not_retained"}}, nil},
		{"event overlap", []OmissionRange{{From: 1, To: 5, Reason: "not_retained"}}, nil},
		{"individual overlap", []OmissionRange{{From: 2, To: 5, Reason: "not_retained"}}, []EventOmission{{Sequence: 3, Reason: "sensitive_identity"}}},
		{"bad reason", []OmissionRange{{From: 2, To: 5, Reason: "free form"}}, nil},
		{"zero start", []OmissionRange{{From: 0, To: 5, Reason: "not_retained"}}, nil},
		{"inverted", []OmissionRange{{From: 5, To: 2, Reason: "not_retained"}}, nil},
		{"past watermark", []OmissionRange{{From: 2, To: 6, Reason: "not_retained"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			final := base
			final.OmissionRanges, final.Omissions = tc.ranges, tc.omissions
			if err := store.Finalize(ctx, final); err == nil {
				t.Fatal("invalid range accepted")
			}
		})
	}
	run, err := store.Get(ctx, base.RunID)
	if err != nil || run.StorageState != Active {
		t.Fatalf("invalid ranges changed run: %+v, %v", run, err)
	}
	valid := base
	valid.Omissions = []EventOmission{{Sequence: 2, Reason: "sensitive_identity"}}
	valid.OmissionRanges = []OmissionRange{{From: 3, To: 5, Reason: "not_retained"}}
	if err := store.Finalize(ctx, valid); err != nil {
		t.Fatalf("valid mixed trace rejected: %v", err)
	}
}

func TestFinalizationAcceptsExplicitAndGenericOmissionRangesBeyondCollectorIndex(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "mixed-ranges", started)
	ranges := make([]OmissionRange, 4097)
	for index := range ranges {
		reason := "sensitive_identity"
		if index == len(ranges)-1 {
			reason = "not_retained"
		}
		ranges[index] = OmissionRange{From: uint64(index + 1), To: uint64(index + 1), Reason: reason}
	}
	final := Finalization{RunID: "mixed-ranges", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: uint64(len(ranges)),
		Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), OmissionRanges: ranges}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatalf("repeated ingestion: %v", err)
	}
}

func TestFinalizationRangeInsertFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "range-rollback", started)
	final := Finalization{RunID: "range-rollback", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 10,
		Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), OmissionRanges: []OmissionRange{{From: 1, To: 10, Reason: "not_retained"}}}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER fail_range_insert BEFORE INSERT ON event_omissions WHEN NEW.sequence=5 BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(ctx, final); err == nil {
		t.Fatal("range insert failure accepted")
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM event_omissions WHERE run_id=?", final.RunID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial omissions survived: %d, %v", count, err)
	}
	run, err := store.Get(ctx, final.RunID)
	if err != nil || run.StorageState != Active {
		t.Fatalf("failure changed run: %+v, %v", run, err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER fail_range_insert"); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Finalize(canceled, final); err == nil {
		t.Fatal("canceled finalization accepted")
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatalf("valid retry failed: %v", err)
	}
}
