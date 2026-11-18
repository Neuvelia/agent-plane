package runtime

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

type observedProbe struct {
	*combinedProbe
	ids  run.ExecutionCorrelation
	fail bool
}

func (p *observedProbe) ExecuteNodeObserved(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, o ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	p.ids = o.Correlation
	o.Log("stdout", []byte("safe SECRET log"))
	e := &events.NodeEvidencePayload{AdapterID: "provider", AdapterVersion: "SECRET", ChangedFiles: []events.ChangedFile{{Path: "SECRET/file", Provenance: "observed"}}}
	if p.fail {
		return nil, e, errors.New("failure")
	}
	return nil, e, nil
}
func TestObservedPrecedenceCorrelationEvidenceAndRedaction(t *testing.T) {
	p := &observedProbe{combinedProbe: &combinedProbe{correlationProbe: &correlationProbe{received: map[string]run.ExecutionCorrelation{}}}}
	for _, failed := range []bool{false, true} {
		p.fail = failed
		r, e := Run(context.Background(), definition(map[string]graph.NodeDefinition{"one": {Type: "test"}}), nil, p, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{"SECRET"}})
		if e != nil {
			t.Fatal(e)
		}
		if p.logs != 0 || p.legacy != 0 || len(p.received) != 0 || p.ids.RunID != r.RunID || p.ids.NodeRunID != r.Nodes[0].NodeRunID {
			t.Fatalf("dispatch %+v", p)
		}
		found := false
		for i, event := range r.Events {
			if event.Type == events.NodeEvidence {
				found = true
				if r.Events[i+1].Type != events.NodeFinished || event.NodeRunID != p.ids.NodeRunID {
					t.Fatal("evidence ordering/correlation")
				}
			}
		}
		if found == failed {
			t.Fatalf("failed=%v evidence=%v", failed, found)
		}
		b, _ := json.Marshal(r.Events)
		if strings.Contains(string(b), "SECRET") {
			t.Fatal("evidence secret leaked")
		}
	}
}

type safeCodeError struct{}

func (safeCodeError) Error() string          { return "SECRET" }
func (safeCodeError) DiagnosticCode() string { return "fixed_failure" }
func TestExecutorFailureCarriesOnlySafeDiagnosticCode(t *testing.T) {
	e := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, safeCodeError{}
	})
	r, err := Run(context.Background(), definition(map[string]graph.NodeDefinition{"one": {Type: "test"}}), nil, e, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range r.Events {
		if event.Type == events.NodeFinished {
			p := event.Payload.(events.NodeFinishedPayload)
			found = len(p.DiagnosticCodes) == 1 && p.DiagnosticCodes[0] == "fixed_failure"
		}
	}
	if !found {
		t.Fatal("safe terminal code missing")
	}
}
