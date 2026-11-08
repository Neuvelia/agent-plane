package safeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

func resultFixture() runtime.RunResult {
	value, _ := graph.NewJSONValue(json.RawMessage(`{"safe":"private","api_token":"private","nested":{"private_key":"private","n":1234567890123456789012345678901234567890}}`))
	return runtime.RunResult{
		GraphID: "graph", GraphVersion: "v1", RunID: events.RunID("run"), State: runtime.RunFailed,
		Outputs: []runtime.PortValue{{Name: "report", Value: value, Provenance: &runtime.AcceptedProvenance{RunID: "run", NodeRunID: "node-run", AttemptID: "attempt", Port: "report"}}},
		Nodes:   []runtime.NodeResult{{NodeID: "node", NodeRunID: "node-run", State: runtime.NodeFailed, AcceptedAttemptID: "attempt", Outputs: []runtime.PortValue{{Name: "report", Value: value}}, Failure: &runtime.Failure{Kind: runtime.FailureExecutor, Err: errors.New("private raw failure"), Diagnostics: []graph.Diagnostic{{Code: "E_SAFE", Message: "private diagnostic"}}}, SkipReason: "private skip reason"}},
		Failure: &runtime.Failure{Kind: runtime.FailureExecutor, Err: errors.New("private top-level failure")},
	}
}

func TestProjectResultRedactsValuesAndOmitsCredentialKeysAndRawErrors(t *testing.T) {
	p, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	result := resultFixture()
	projected, err := p.ProjectResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Complete {
		t.Fatal("omitted and redacted output must be incomplete")
	}
	if len(projected.Nodes) != 1 || projected.Nodes[0].NodeRunID != "node-run" || projected.Nodes[0].Status != "failed" {
		t.Fatalf("nodes=%+v", projected.Nodes)
	}
	for _, data := range [][]byte{projected.Result.Bytes(), projected.Nodes[0].Projection.Bytes()} {
		for _, forbidden := range []string{"private", "api_token", "private_key", "raw failure", "diagnostic", "skip reason"} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Fatalf("projection contains %q: %s", forbidden, data)
			}
		}
		if !bytes.Contains(data, []byte("1234567890123456789012345678901234567890")) {
			t.Fatalf("number lexeme lost: %s", data)
		}
	}
	if raw, _ := result.Outputs[0].Value.AsJSON(); !bytes.Contains(raw, []byte("private")) {
		t.Fatal("input mutated")
	}
}

func TestProjectResultFailsClosedOnRequiredIdentity(t *testing.T) {
	p, _ := NewProjector([]string{"private"})
	for name, change := range map[string]func(*runtime.RunResult){
		"run":      func(r *runtime.RunResult) { r.RunID = "private-run" },
		"node":     func(r *runtime.RunResult) { r.Nodes[0].NodeID = "private-node" },
		"node_run": func(r *runtime.RunResult) { r.Nodes[0].NodeRunID = "private-node-run" },
		"status":   func(r *runtime.RunResult) { r.Nodes[0].State = runtime.NodeState("private") },
	} {
		t.Run(name, func(t *testing.T) {
			r := resultFixture()
			change(&r)
			if _, err := p.ProjectResult(r); err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}

func TestProjectResultBoundsOversizedValueWithOmission(t *testing.T) {
	p, _ := NewProjector(nil)
	r := resultFixture()
	r.Outputs[0].Value = graph.NewStringValue(strings.Repeat("x", 4<<20))
	r.Nodes[0].Outputs[0].Value = r.Outputs[0].Value
	projected, err := p.ProjectResult(r)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Complete {
		t.Fatal("oversize omission must be incomplete")
	}
	if len(projected.Result.Bytes()) > 3<<20 || len(projected.Nodes[0].Projection.Bytes()) > 3<<20 {
		t.Fatal("projection exceeds indexed bound")
	}
	if bytes.Contains(projected.Result.Bytes(), []byte(strings.Repeat("x", 1024))) {
		t.Fatal("oversize value retained")
	}
}

func TestProjectResultOmitsUnsafeStructuredAndNumericValues(t *testing.T) {
	p, _ := NewProjector([]string{"123456789"})
	r := resultFixture()
	numeric, err := graph.NewNumberValue(json.Number("123456789"))
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := graph.NewJSONValue(json.RawMessage(`{"safe":1,"safe":2}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Outputs = []runtime.PortValue{{Name: "number", Value: numeric}, {Name: "json", Value: duplicate}}
	r.Nodes[0].Outputs = nil
	projected, err := p.ProjectResult(r)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Complete || bytes.Contains(projected.Result.Bytes(), []byte("123456789")) || bytes.Contains(projected.Result.Bytes(), []byte(`"safe"`)) {
		t.Fatalf("projection=%s complete=%v", projected.Result.Bytes(), projected.Complete)
	}
	if !bytes.Contains(projected.Result.Bytes(), []byte(`false,null`)) {
		t.Fatalf("missing explicit omissions: %s", projected.Result.Bytes())
	}
}

func TestProjectResultOmitsUnsafeOptionalProvenance(t *testing.T) {
	p, _ := NewProjector([]string{"private"})
	r := resultFixture()
	r.Outputs[0].Provenance.Port = "private-port"
	projected, err := p.ProjectResult(r)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Complete || bytes.Contains(projected.Result.Bytes(), []byte("private-port")) {
		t.Fatalf("projection=%s", projected.Result.Bytes())
	}
}

func TestProjectResultPreservesPortProvenanceAndFailureCodes(t *testing.T) {
	p, _ := NewProjector(nil)
	r := resultFixture()
	projected, err := p.ProjectResult(r)
	if err != nil {
		t.Fatal(err)
	}
	var root []any
	decoder := json.NewDecoder(bytes.NewReader(projected.Result.Bytes()))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		t.Fatal(err)
	}
	ports := root[5].([]any)
	port := ports[0].([]any)
	if port[0] != "report" || port[1] != true {
		t.Fatalf("port=%v", port)
	}
	provenance := port[3].([]any)
	if len(provenance) != 4 || provenance[0] != "run" || provenance[1] != "node-run" || provenance[2] != "attempt" || provenance[3] != "report" {
		t.Fatalf("provenance=%v", provenance)
	}
	value := port[2].([]any)
	object := value[1].(map[string]any)
	nested := object["nested"].(map[string]any)
	if nested["n"] != json.Number("1234567890123456789012345678901234567890") {
		t.Fatalf("number=%v", nested["n"])
	}
	failure := root[6].([]any)
	if failure[0] != json.Number("2") {
		t.Fatalf("failure=%v", failure)
	}
	if !bytes.Contains(projected.Nodes[0].Projection.Bytes(), []byte("E_SAFE")) {
		t.Fatalf("missing safe diagnostic code: %s", projected.Nodes[0].Projection.Bytes())
	}
}

func TestProjectAttemptSummaryProjectsTypedReferences(t *testing.T) {
	p, _ := NewProjector([]string{"private"})
	summary := runtime.AttemptSummary{AttemptID: "attempt", NodeID: "node", NodeRunID: "node-run", Outcome: runtime.AttemptAccepted,
		Check:    &runtime.AttemptCheckReference{CheckID: "check", Kind: events.CheckCommand, Verdict: events.CheckGreen, Sequence: 11},
		Envelope: &runtime.AttemptEnvelopeReference{UnitID: "node", CheckID: "check", Verdict: events.CheckGreen, Sequence: 12}}
	projected, complete, err := p.ProjectAttemptSummary(summary)
	if err != nil || !complete || projected.Status != "accepted" || !bytes.Contains(projected.Projection.Bytes(), []byte(`"check"`)) {
		t.Fatalf("projection=%+v complete=%v err=%v", projected, complete, err)
	}
	summary.Check.IdentityRedacted = true
	summary.Envelope.CheckID = "private-check"
	projected, complete, err = p.ProjectAttemptSummary(summary)
	if err != nil || complete || bytes.Contains(projected.Projection.Bytes(), []byte(`"check"`)) || bytes.Contains(projected.Projection.Bytes(), []byte("private")) {
		t.Fatalf("projection=%s complete=%v err=%v", projected.Projection.Bytes(), complete, err)
	}
	summary.AttemptID = "private-attempt"
	if _, _, err := p.ProjectAttemptSummary(summary); err == nil {
		t.Fatal("unsafe attempt ID accepted")
	}
}
