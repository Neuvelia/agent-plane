package history

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLaunchIdentifierBoundaryAndAggregateProjectionLimit(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	base := LaunchRecord{RunID: strings.Repeat("r", 256), ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}
	if err := store.Launch(ctx, base); err != nil {
		t.Fatalf("256-byte run ID rejected: %v", err)
	}
	tooLong := base
	tooLong.RunID += "x"
	if err := store.Launch(ctx, tooLong); err == nil {
		t.Fatal("257-byte run ID accepted")
	}
	large := safeJSON(t, `{"text":"`+strings.Repeat("x", 4<<20)+`"}`)
	tooLarge := base
	tooLarge.RunID = "oversized"
	tooLarge.Graph = large
	tooLarge.Launch = large
	if err := store.Launch(ctx, tooLarge); !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("unreadable combined snapshot accepted: %v", err)
	}
	if _, err := store.Get(ctx, "oversized"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oversized marker persisted: %v", err)
	}
}

func TestGetAndListRejectCorruptOversizedResponses(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	large := safeJSON(t, `{"text":"`+strings.Repeat("x", 7<<20)+`"}`)
	if _, err := store.db.ExecContext(ctx, "UPDATE runs SET safe_graph_json=?,graph_digest=? WHERE run_id='run'", large.data, large.digest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "run"); !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("oversized detail returned: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE runs SET project_id=? WHERE run_id='run'", strings.Repeat("x", 7<<20)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, ListQuery{Limit: 1}); !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("oversized list returned: %v", err)
	}
}

func TestFinalizeRejectsAggregateSnapshotOverflowAtomically(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	initial := safeJSON(t, `{"text":"`+strings.Repeat("x", 2<<20)+`"}`)
	result := safeJSON(t, `{"text":"`+strings.Repeat("x", 3<<20)+`"}`)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: initial, Launch: initial, Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	final := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: result, Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}
	if err := store.Finalize(ctx, final); !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("oversized finalized snapshot accepted: %v", err)
	}
	run, err := store.Get(ctx, "run")
	if err != nil || run.StorageState != Active {
		t.Fatalf("failed finalization changed marker: %+v, %v", run, err)
	}
}

func TestFinalizeRejectsIndexedProjectionTooLargeAtomically(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	large := safeJSON(t, `{"x":"`+strings.Repeat("x", 3<<20)+`"}`)
	small := safeJSON(t, `{}`)
	for _, kind := range []string{"event", "node", "attempt"} {
		launchEvidenceRun(t, store, kind, started)
		final := Finalization{RunID: kind, EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: small, Completeness: small,
			Events:   []Event{{Sequence: 1, Projection: small}},
			Nodes:    []NodeOutcome{{NodeRunID: "n", NodeID: "node", Status: "succeeded", Projection: small}},
			Attempts: []AttemptOutcome{{AttemptID: "a", NodeRunID: "n", Status: "succeeded", Projection: small}}}
		switch kind {
		case "event":
			final.Events[0].Projection = large
		case "node":
			final.Nodes[0].Projection = large
		case "attempt":
			final.Attempts[0].Projection = large
		}
		if err := store.Finalize(ctx, final); !errors.Is(err, ErrPageTooLarge) {
			t.Fatalf("%s oversized projection accepted: %v", kind, err)
		}
		run, err := store.Get(ctx, kind)
		if err != nil || run.StorageState != Active {
			t.Fatalf("%s failed finalization mutated run: %+v, %v", kind, run, err)
		}
	}
}

func TestMaximumIndexedProjectionsRemainReadableAtLimitOne(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	boundary := safeJSON(t, `{"x":"`+strings.Repeat("x", (3<<20)-8)+`"}`)
	if len(boundary.data) != 3<<20 {
		t.Fatalf("test projection length=%d", len(boundary.data))
	}
	final := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`),
		Events:   []Event{{Sequence: 1, Projection: boundary}},
		Nodes:    []NodeOutcome{{NodeRunID: "n", NodeID: "node", Status: "succeeded", Projection: boundary}},
		Attempts: []AttemptOutcome{{AttemptID: "a", NodeRunID: "n", Status: "succeeded", Projection: boundary}}}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
	if page, err := store.Events(ctx, "run", 0, 1); err != nil || len(page.Events) != 1 {
		t.Fatalf("maximum event unreadable: %d, %v", len(page.Events), err)
	}
	if page, err := store.Outcomes(ctx, "run", 1, "", ""); err != nil || len(page.Nodes) != 1 || len(page.Attempts) != 1 {
		t.Fatalf("maximum outcomes unreadable: nodes=%d attempts=%d, %v", len(page.Nodes), len(page.Attempts), err)
	}
}

func TestEventsRejectCursorAboveWatermark(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if _, err := store.Events(ctx, "run", 1, 1); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("active event cursor above watermark accepted: %v", err)
	}
	if err := store.Finalize(ctx, Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(ctx, "run", 2, 1); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("complete event cursor above watermark accepted: %v", err)
	}
	if page, err := store.Events(ctx, "run", 1, 1); err != nil || page.NextSequence != 1 || page.Watermark != 1 {
		t.Fatalf("cursor at watermark rejected: %+v, %v", page, err)
	}
}

func TestGetRejectsCompleteRunWithMissingTerminalFields(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if err := store.Finalize(ctx, Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	defer store.db.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF")
	for _, field := range []string{"ended_at", "runtime_state", "safe_result_json", "result_digest"} {
		if _, err := store.db.ExecContext(ctx, "UPDATE runs SET "+field+"=NULL WHERE run_id='run'"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Get(ctx, "run"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("complete run with NULL %s returned: %v", field, err)
		}
		if _, err := store.List(ctx, ListQuery{Limit: 1}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("complete run with NULL %s listed: %v", field, err)
		}
		if _, err := store.Events(ctx, "run", 0, 1); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("complete run with NULL %s exposed finished events: %v", field, err)
		}
		if _, err := store.db.ExecContext(ctx, "UPDATE runs SET ended_at=?,runtime_state='succeeded',safe_result_json='{}',result_digest=? WHERE run_id='run'", formatHistoryTime(started.Add(time.Second)), safeJSON(t, `{}`).digest); err != nil {
			t.Fatal(err)
		}
	}
}
