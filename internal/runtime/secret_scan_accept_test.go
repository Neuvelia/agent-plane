package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

// This pins the wiring: acceptBatchResults itself must close the window of a
// node it finishes. The worker result carries no scan (the worker expected a
// retry), the node fails, and the coordinator records the unobserved scan after
// node_finished. The disagreement cannot be produced through runtime.Run with
// the public ports, so the coordinator is driven directly.
func TestAcceptBatchResultsClosesWindowOfFinishedNode(t *testing.T) {
	m, emitted := scanTestMachine()
	m.nodes["a"].State = run.NodeRunning
	m.nodeStarted = map[string]time.Time{"a": {}}
	m.scans.store("a", scanSession{window: noopWindow{}})
	definition := &graph.Definition{Nodes: map[string]graph.NodeDefinition{"a": {Type: "shell"}}}
	retry := m.acceptBatchResults(context.Background(), definition, []completedNode{{id: "a", err: errors.New("boom")}}, &batchState{})
	if len(retry) != 0 {
		t.Fatalf("retry = %v, want none", retry)
	}
	finished, scanned := -1, -1
	for i, e := range *emitted {
		if e.Type == events.NodeFinished {
			finished = i
		}
		if check, ok := e.Payload.(events.GateCheckPayload); ok && check.Kind == events.CheckSecretInWorkspace {
			scanned = i
			if check.Reason != "secret_scan_unobserved" || check.Evidence[0].Reference.Reason != "scan_not_completed" {
				t.Errorf("check = %s, reference %+v", check.Reason, check.Evidence[0].Reference)
			}
		}
	}
	if finished < 0 || scanned < finished {
		t.Fatalf("node_finished at %d, scan at %d, want the scan result after node_finished", finished, scanned)
	}
}
