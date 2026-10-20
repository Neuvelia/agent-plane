package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

func TestRunHandlersRejectMalformedSelectionsWithoutStarting(t *testing.T) {
	called := false
	runs := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		called = true
		return PreparedRun{}, protocolError("unavailable", "test")
	}, nil)
	defer runs.Close()
	handlers := NewRunHandlers(runs)
	for _, raw := range []string{
		`{}`,
		`{"idempotency_key":"bad"}`,
		`{"idempotency_key":"00000000-0000-4000-8000-000000000001","preflight_token":"bad"}`,
		`{"idempotency_key":"00000000-0000-4000-8000-000000000001","preflight_token":"0000000000000000000000000000000000000000000000000000000000000000","payload_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","document_id":"00000000-0000-0000-0000-000000000001","revision":1,"workspace_id":"00000000-0000-0000-0000-000000000002","inputs":{"x":{"type":"string","value":"ok","extra":1}},"max_concurrency":1}`,
		`{"idempotency_key":"00000000-0000-4000-8000-000000000001","preflight_token":"0000000000000000000000000000000000000000000000000000000000000000","payload_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","document_id":"00000000-0000-0000-0000-000000000001","revision":1,"workspace_id":"00000000-0000-0000-0000-000000000002","inputs":{},"max_concurrency":1,"unknown":true}`,
	} {
		if _, err := handlers["runs.start"](context.Background(), json.RawMessage(raw)); err == nil || err.Code != "invalid_request" {
			t.Fatalf("malformed Start accepted: %s => %v", raw, err)
		}
	}
	for _, method := range []string{"runs.cancel", "runs.snapshot"} {
		for _, raw := range []string{`{}`, `{"run_id":"bad"}`, `{"run_id":"00000000-0000-4000-8000-000000000001","extra":true}`} {
			if _, err := handlers[method](context.Background(), json.RawMessage(raw)); err == nil || err.Code != "invalid_request" {
				t.Fatalf("malformed %s accepted: %s => %v", method, raw, err)
			}
		}
	}
	if called {
		t.Fatal("malformed request reached run preparer")
	}
}

func TestRunHandlerStartsSavedPreflightAndReattachesWithFreshToken(t *testing.T) {
	preflight, doc, workspace, _ := preflightFixture(t, validGraphText)
	selection := PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	first, err := preflight.Preflight(context.Background(), selection)
	if err != nil || first.HasErrors || first.PreflightToken == "" {
		t.Fatalf("preflight = %#v, %v", first, err)
	}
	const runID = events.RunID("00000000-0000-4000-8000-000000000099")
	runner := func(_ context.Context, definition *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		collector := events.NewCollector(runID, definition.ID, first.GraphVersion)
		if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
			return runtime.RunResult{}, err
		}
		options.OnRunReady(runID, collector)
		return runtime.RunResult{RunID: runID, GraphID: definition.ID, GraphVersion: first.GraphVersion, State: runtime.RunSucceeded}, nil
	}
	runs := NewRunService(context.Background(), preflight.PrepareStart, runner)
	defer runs.Close()
	handlers := NewRunHandlers(runs)
	const key = "00000000-0000-4000-8000-000000000001"
	startParams := func(token string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"idempotency_key":%q,"preflight_token":%q,"payload_digest":%q,"document_id":%q,"revision":%d,"workspace_id":%q,"inputs":{},"max_concurrency":1}`,
			key, token, first.PayloadDigest, doc.DocumentID, doc.Revision, workspace.WorkspaceID))
	}
	started, serviceErr := handlers["runs.start"](context.Background(), startParams(first.PreflightToken))
	if serviceErr != nil || started.(RunStartResult).RunID != runID {
		t.Fatalf("start = %#v, %v", started, serviceErr)
	}
	second, err := preflight.Preflight(context.Background(), selection)
	if err != nil || second.PayloadDigest != first.PayloadDigest || second.PreflightToken == first.PreflightToken {
		t.Fatalf("fresh preflight = %#v, %v", second, err)
	}
	reattached, serviceErr := handlers["runs.start"](context.Background(), startParams(second.PreflightToken))
	if serviceErr != nil || reattached.(RunStartResult).RunID != runID {
		t.Fatalf("reattach = %#v, %v", reattached, serviceErr)
	}
	page, serviceErr := handlers["runs.events"](context.Background(), json.RawMessage(`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":0,"limit":8}`))
	if serviceErr != nil || len(page.(RunEventsPage).Events) != 1 || page.(RunEventsPage).Events[0].Type != events.RunStarted {
		t.Fatalf("replay after reattach = %#v, %v", page, serviceErr)
	}
	cancelled, serviceErr := handlers["runs.cancel"](context.Background(), json.RawMessage(`{"run_id":"00000000-0000-4000-8000-000000000099"}`))
	if serviceErr != nil || cancelled.(RunCancelResult).RunID != runID || cancelled.(RunCancelResult).State != runtime.RunSucceeded {
		t.Fatalf("completed run = %#v, %v", cancelled, serviceErr)
	}
}
