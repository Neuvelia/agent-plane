package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

type checkProbe struct {
	mode string
	fact events.GateCheckPayload
}

func (p checkProbe) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	panic("observed dispatch required")
}

type redCheck struct{}

func (redCheck) Error() string       { return "red" }
func (redCheck) GateVerdict() string { return "red" }
func (p checkProbe) ExecuteNodeObserved(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, o ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	f := p.fact
	if p.mode == "invalid" {
		f.Verdict = "bad"
	}
	if p.mode != "missing" {
		o.Check(f)
	}
	if p.mode == "duplicate" {
		o.Check(f)
	}
	f.Evidence[0].Path = "mutated"
	if p.mode == "error green" || p.mode == "operational red" {
		return nil, nil, errors.New("operational")
	}
	if p.mode == "red" {
		return nil, nil, redCheck{}
	}
	if p.mode == "outputs red" {
		return map[string]graph.Value{"verdict": graph.NewStringValue("red")}, nil, redCheck{}
	}
	return map[string]graph.Value{"verdict": graph.NewStringValue("green")}, nil, nil
}
func TestGateObservationFailsClosedAndOrdersFact(t *testing.T) {
	for _, mode := range []string{"green", "red", "duplicate", "invalid", "missing", "error green", "success red", "operational red", "outputs red", "identity mismatch"} {
		t.Run(mode, func(t *testing.T) {
			f := events.GateCheckPayload{CheckID: "SECRET-check", Kind: "json_predicate", Verdict: "green", Reason: "SECRET reason", Evidence: []events.GateEvidence{{Source: "json_predicate"}}}
			if strings.Contains(mode, "red") || mode == "red" {
				f.Verdict = "red"
			}
			if mode == "identity mismatch" {
				f.CheckID = "other"
			}
			d := definition(map[string]graph.NodeDefinition{"gate": {Type: "gate", Config: graph.NodeConfig{"check_id": "SECRET-check", "kind": "json_predicate"}, Outputs: map[string]graph.PortDefinition{"verdict": {Type: graph.ValueTypeString, Required: true}}}})
			r, e := Run(context.Background(), d, nil, checkProbe{mode, f}, Options{LogRedactions: []string{"SECRET"}})
			if e != nil {
				t.Fatal(e)
			}
			facts := 0
			for i, ev := range r.Events {
				if ev.Type == events.GateCheck {
					facts++
					p := ev.Payload.(events.GateCheckPayload)
					if strings.Contains(p.CheckID+p.Reason, "SECRET") || p.Evidence[0].Path != "" || ev.RunID != r.RunID || ev.NodeRunID != r.Nodes[0].NodeRunID || r.Events[i+1].Type != events.NodeFinished {
						t.Fatal("fact redaction/copy/correlation/order")
					}
				}
			}
			valid := mode == "green" || mode == "red"
			if (facts == 1) != valid || (r.State == RunSucceeded) != (mode == "green") || mode != "green" && len(r.Nodes[0].Outputs) > 0 {
				t.Fatalf("mode=%s facts=%d result=%+v", mode, facts, r)
			}
		})
	}
}
func TestGateCaptureRedactsBeforeTruncation(t *testing.T) {
	r, e := newLogRedactor([]string{"SECRET"})
	if e != nil {
		t.Fatal(e)
	}
	c := newCheckCapture(r)
	c.Write([]byte(strings.Repeat("x", 8190) + "SEC"))
	c.Write([]byte("RET" + strings.Repeat("y", 100)))
	b, n := c.Finish()
	if len(b) != 8192 || n == 0 || strings.Contains(string(b), "SEC") || !strings.HasSuffix(string(b), "**") {
		t.Fatalf("%q omitted=%d", b[len(b)-10:], n)
	}
	b[0] = 'z'
	again, _ := c.Finish()
	if again[0] != 'x' {
		t.Fatal("capture alias")
	}
}
