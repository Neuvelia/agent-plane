package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

// agentProgressFixture is the desktop F2.7 recording of a non-loop Agent node
// that emits normalized progress and node-level node_evidence (no attempt ID).
// Regenerate with:
//
//	AGENTPLANE_UPDATE_FIXTURES=1 go test ./internal/runtime -run TestDesktopAgentProgressFixture
//
// Without the variable the test fails when the committed file no longer matches
// what the engine emits (per-run values are normalized as for agent-scope-retry).
// The bounded `progress_omitted` marker needs megabytes of text and is covered by
// a synthetic payload test in the renderer instead of this recording.
const agentProgressFixture = "../../desktop/test/fixtures/agent-progress.json"

// progressExecutor stands in for an Agent adapter: it reports normalized
// progress and returns Git-observed evidence, as the Codex adapter does.
type progressExecutor struct{}

func (progressExecutor) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	return nil, nil
}

func (progressExecutor) ExecuteNodeObserved(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	exit := 0
	o.Progress(events.Status, events.StatusPayload{Code: "started"})
	o.Progress(events.TextDelta, events.TextDeltaPayload{SourceID: "msg-1", Text: "Looking at "})
	o.Progress(events.TextDelta, events.TextDeltaPayload{SourceID: "msg-1", Text: "the failing test."})
	o.Progress(events.ToolCallStarted, events.ToolCallPayload{CallID: "call-1", Name: "read_file", Status: "in_progress"})
	o.Progress(events.ToolCallFinished, events.ToolCallPayload{CallID: "call-1", Name: "read_file", Status: "completed"})
	o.Progress(events.CommandStarted, events.CommandPayload{CommandID: "cmd-1", Status: "in_progress"})
	o.Progress(events.CommandFinished, events.CommandPayload{CommandID: "cmd-1", Status: "completed", ExitCode: &exit})
	o.Progress(events.FileChanged, events.FileChangedPayload{Path: "src/claimed.go", Action: "update", Provenance: "reported"})
	o.Progress(events.Status, events.StatusPayload{Code: "finished"})
	return nil, &events.NodeEvidencePayload{AdapterID: "fake", AdapterVersion: "1.0.0", ChangedFiles: []events.ChangedFile{{Path: "src/a.go", Provenance: "observed"}, {Path: "src/b.go", Provenance: "observed"}}}, nil
}

func TestDesktopAgentProgressFixture(t *testing.T) {
	d := &graph.Definition{SchemaVersion: 0, ID: "agent-progress", Inputs: map[string]graph.InputDefinition{}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}, Nodes: map[string]graph.NodeDefinition{"scout": {Type: "test"}}}
	r, err := runtime.Run(context.Background(), d, nil, progressExecutor{}, runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || r.State != run.RunSucceeded {
		t.Fatalf("run=%+v err=%v", r, err)
	}
	got, err := json.MarshalIndent(r.Events, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("AGENTPLANE_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(agentProgressFixture, got, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(agentProgressFixture)
	if err != nil {
		t.Fatal(err)
	}
	if g, w := normalizeFixture(t, got), normalizeFixture(t, bytes.TrimSpace(want)); !reflect.DeepEqual(g, w) {
		gj, _ := json.MarshalIndent(g, "", " ")
		wj, _ := json.MarshalIndent(w, "", " ")
		t.Fatalf("%s is stale; regenerate with AGENTPLANE_UPDATE_FIXTURES=1\n--- engine:\n%s\n--- committed:\n%s", agentProgressFixture, gj, wj)
	}
}
