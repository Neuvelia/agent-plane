package runtime_test

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/runtime"
)

// A rejected attempt's output is never recorded; only the accepted attempt's
// value is, with that attempt's identifier.
func TestValueRecordingRecordsOnlyTheAcceptedAttempt(t *testing.T) {
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		if n == 1 {
			return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":false,"try":"first"}`), ChangedFiles: []agent.ChangedFile{}}, nil
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":true,"try":"second"}`), ChangedFiles: []agent.ChangedFile{}}, nil
	}
	r, err := runtime.Run(context.Background(), loopDefinition(3), nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, RecordValues: true, LogRedactions: []string{"SECRET"}})
	if err != nil || r.State != run.RunSucceeded {
		t.Fatalf("%+v %v", r.Failure, err)
	}
	var accepted events.AttemptID
	var values []events.Event
	for _, e := range r.Events {
		if e.Type == events.AttemptFinished && e.Payload.(events.AttemptFinishedPayload).Outcome == "accepted" {
			accepted = e.AttemptID
		}
		if e.Type == events.ValueRecorded {
			values = append(values, e)
		}
	}
	if accepted == "" || len(values) != 1 {
		t.Fatalf("accepted=%q values=%d", accepted, len(values))
	}
	p := values[0].Payload.(events.ValueRecordedPayload)
	if values[0].NodeID != "work" || p.AttemptID != accepted || values[0].AttemptID != accepted || p.Port != "raw_metadata" || p.Type != "json" || p.Value == nil || !strings.Contains(*p.Value, "second") {
		t.Fatalf("recorded = %#v %+v", values[0], p)
	}
	encoded, _ := json.Marshal(r.Events)
	if strings.Contains(string(encoded), "first") {
		t.Fatal("rejected attempt output reached events")
	}
}

// Enrolled units record their immutable accepted values, one per node run,
// and the identifiers equal AcceptedProvenance.
func TestValueRecordingMatchesUnitProvenance(t *testing.T) {
	d := unitBatch([]string{unitRepo(t)}, 3)
	a := newLoopAdapter()
	counts := map[string]int{}
	var mu sync.Mutex
	a.produce = func(_ context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		mu.Lock()
		counts[in.Task]++
		n := counts[in.Task]
		mu.Unlock()
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(fmt.Sprintf(`{"ok":%t,"unit":%q,"attempt":%d}`, in.Task != "b" || n == 2, in.Task, n))}, nil
	}
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, MaxConcurrency: 4, RecordValues: true})
	if err != nil || r.State != run.RunSucceeded {
		t.Fatal(r.Failure, err)
	}
	recorded := map[string]events.Event{}
	for _, e := range r.Events {
		if e.Type == events.ValueRecorded {
			if _, dup := recorded[e.NodeID]; dup {
				t.Fatalf("node %q recorded twice", e.NodeID)
			}
			recorded[e.NodeID] = e
		}
	}
	for _, n := range r.Nodes {
		if len(n.Outputs) == 0 {
			continue
		}
		e, ok := recorded[n.NodeID]
		if !ok {
			t.Fatalf("unit %q not recorded", n.NodeID)
		}
		p := e.Payload.(events.ValueRecordedPayload)
		want, _ := n.Outputs[0].Value.AsJSON()
		prov := n.Outputs[0].Provenance
		if prov == nil || e.RunID != prov.RunID || p.NodeRunID != prov.NodeRunID || p.AttemptID != prov.AttemptID || p.Port != prov.Port || *p.Value != string(want) {
			t.Fatalf("unit %q: %+v vs %+v", n.NodeID, p, prov)
		}
	}
	if !strings.Contains(*recorded["b"].Payload.(events.ValueRecordedPayload).Value, `"attempt":2`) {
		t.Fatal("unit b must record its accepted second attempt")
	}
}
