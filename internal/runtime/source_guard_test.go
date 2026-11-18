package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

func TestSourceGuardReportsAbsolutePathWrite(t *testing.T) {
	source := scopeRepo(t)
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		if err := os.WriteFile(filepath.Join(source, "leak.txt"), []byte("x"), 0o600); err != nil {
			return agent.AgentExecutionOutput{}, err
		}
		return agent.AgentExecutionOutput{RawMetadata: []byte(`{"ok":true}`)}, nil
	}
	w := &fakeWorkspaces{root: t.TempDir()}
	result, err := runtime.Run(context.Background(), isolatedLoop(source), nil, compose(a), isolationOptions(w))
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	observed := eventsOf(result, events.SourceWorkspaceChanged)
	if len(observed) != 1 {
		t.Fatalf("want one source_workspace_changed, got %d", len(observed))
	}
	payload := observed[0].Payload.(events.SourceWorkspaceChangedPayload)
	if len(payload.Paths) != 1 || payload.Paths[0] != "leak.txt" || payload.Omitted != 0 || payload.Indeterminate {
		t.Fatalf("payload %+v", payload)
	}
	if observed[0].NodeID != "" || observed[0].AttemptID != "" {
		t.Fatalf("the observation is run-level and unattributed: %+v", observed[0])
	}
	if i, finished := indexOfEvent(result, events.SourceWorkspaceChanged, ""), len(result.Events)-1; i < 0 || i >= finished || result.Events[finished].Type != events.RunFinished {
		t.Fatalf("source_workspace_changed must precede run_finished (at %d of %d)", i, len(result.Events))
	}
}

func TestSourceGuardSilentWhenUnchanged(t *testing.T) {
	source := scopeRepo(t)
	w := &fakeWorkspaces{root: t.TempDir()}
	result, err := runtime.Run(context.Background(), isolatedLoop(source), nil, compose(okAdapter()), isolationOptions(w))
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	if n := countEvents(result, events.SourceWorkspaceChanged); n != 0 {
		t.Fatalf("an unchanged source emits nothing, got %d events", n)
	}
}

func TestSourceGuardIndeterminateDoesNotFailRun(t *testing.T) {
	notGit := t.TempDir()
	w := &fakeWorkspaces{root: t.TempDir()}
	result, err := runtime.Run(context.Background(), isolatedLoop(notGit), nil, compose(okAdapter()), isolationOptions(w))
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("a guard failure must not fail the run: err=%v state=%s", err, result.State)
	}
	observed := eventsOf(result, events.SourceWorkspaceChanged)
	if len(observed) != 1 {
		t.Fatalf("want one indeterminate observation, got %d", len(observed))
	}
	payload := observed[0].Payload.(events.SourceWorkspaceChangedPayload)
	if !payload.Indeterminate || len(payload.Paths) != 0 || payload.Omitted != 0 {
		t.Fatalf("payload %+v", payload)
	}
}

func TestSourceGuardNotTakenWithoutIsolatedNodes(t *testing.T) {
	d := isolatedLoop(t.TempDir())
	d.Nodes["work"].Config["isolation"] = "none"
	result, err := runtime.Run(context.Background(), d, nil, compose(okAdapter()), isolationOptions(&fakeWorkspaces{}))
	if err != nil || countEvents(result, events.SourceWorkspaceChanged) != 0 {
		t.Fatalf("err=%v", err)
	}
}
