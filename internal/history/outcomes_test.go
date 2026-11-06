package history

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFinalizationStoresAndPagesNodeAndAttemptOutcomes(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	final := Finalization{
		RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}},
		Nodes: []NodeOutcome{
			{NodeRunID: "node-a", NodeID: "a", Status: "succeeded", Projection: safeJSON(t, `{"checks":[{"status":"green"}]}`)},
			{NodeRunID: "node-b", NodeID: "b", Status: "succeeded", Projection: safeJSON(t, `{"output":"safe"}`)},
		},
		Attempts: []AttemptOutcome{
			{AttemptID: "attempt-a", NodeRunID: "node-a", Status: "succeeded", Projection: safeJSON(t, `{"return":{"value":"safe"}}`)},
			{AttemptID: "attempt-b", NodeRunID: "node-b", Status: "succeeded", Projection: safeJSON(t, `{"return":{"value":"safe"}}`)},
		},
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatalf("same outcome ingestion not idempotent: %v", err)
	}
	first, err := store.Outcomes(ctx, "run", 1, "", "")
	if err != nil || len(first.Nodes) != 1 || first.Nodes[0].NodeRunID != "node-a" || len(first.Attempts) != 1 || first.Attempts[0].AttemptID != "attempt-a" || first.NextNodeCursor == "" || first.NextAttemptCursor == "" {
		t.Fatalf("first outcomes=%+v, %v", first, err)
	}
	if string(first.Nodes[0].Projection.Bytes()) != `{"checks":[{"status":"green"}]}` || string(first.Attempts[0].Projection.Bytes()) != `{"return":{"value":"safe"}}` {
		t.Fatalf("safe detail lost: %+v", first)
	}
	second, err := store.Outcomes(ctx, "run", 1, first.NextNodeCursor, first.NextAttemptCursor)
	if err != nil || len(second.Nodes) != 1 || second.Nodes[0].NodeRunID != "node-b" || len(second.Attempts) != 1 || second.Attempts[0].AttemptID != "attempt-b" || second.NextNodeCursor != "" || second.NextAttemptCursor != "" {
		t.Fatalf("second outcomes=%+v, %v", second, err)
	}
	conflict := final
	conflict.Attempts = append([]AttemptOutcome(nil), final.Attempts...)
	conflict.Attempts[0].Projection = safeJSON(t, `{"return":{"value":"different"}}`)
	if err := store.Finalize(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed outcome accepted: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE attempt_outcomes SET safe_json=? WHERE run_id='run' AND attempt_id='attempt-a'`, `{"tampered":true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Outcomes(ctx, "run", 2, "", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("tampered attempt displayed: %v", err)
	}
}

func TestFinalizationRejectsAttemptWithoutNodeAtomically(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	final := Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}, Attempts: []AttemptOutcome{{AttemptID: "attempt", NodeRunID: "missing-node", Status: "failed", Projection: safeJSON(t, `{}`)}}}
	if err := store.Finalize(ctx, final); err == nil {
		t.Fatal("orphan attempt accepted")
	}
	run, err := store.Get(ctx, "run")
	if err != nil || run.StorageState != Active {
		t.Fatalf("failed finalization changed run: %+v, %v", run, err)
	}
}

func TestOutcomeCursorDoesNotRestartExhaustedSide(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	if err := store.Finalize(ctx, Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}, Nodes: []NodeOutcome{
		{NodeRunID: "n1", NodeID: "one", Status: "succeeded", Projection: safeJSON(t, `{}`)},
		{NodeRunID: "n2", NodeID: "two", Status: "succeeded", Projection: safeJSON(t, `{}`)},
	}, Attempts: []AttemptOutcome{{AttemptID: "a1", NodeRunID: "n1", Status: "succeeded", Projection: safeJSON(t, `{}`)}}}); err != nil {
		t.Fatal(err)
	}
	first, err := store.Outcomes(ctx, "run", 1, "", "")
	if err != nil || first.NextNodeCursor == "" || first.NextAttemptCursor != "" {
		t.Fatalf("first cursors=%+v, %v", first, err)
	}
	second, err := store.Outcomes(ctx, "run", 1, first.NextNodeCursor, first.NextAttemptCursor)
	if err != nil || len(second.Nodes) != 1 || second.Nodes[0].NodeRunID != "n2" || len(second.Attempts) != 0 {
		t.Fatalf("exhausted attempts restarted: %+v, %v", second, err)
	}
}
