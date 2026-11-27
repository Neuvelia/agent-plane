package yamlgraph_test

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
	"fmt"
	"strings"
	"testing"
)

func humanDoc(body string) string {
	return "schema_version: 0\nid: h\ninputs: {}\nnodes:\n  ask:\n    type: human\n" + body + "edges: []\noutputs: {}\n"
}

func TestHumanNodeValidation(t *testing.T) {
	good := "    config: {prompt: ok}\n    outputs: {decision: {type: string, required: true}}\n"
	for _, tt := range []struct {
		name, body string
		ok         bool
	}{
		{"valid", good, true},
		{"missing decision output", "    config: {prompt: ok}\n", false},
		{"wrong decision type", "    outputs: {decision: {type: json}}\n", false},
		{"extra output", "    outputs: {decision: {type: string}, other: {type: string}}\n", false},
		{"unknown config key", "    config: {command: /bin/true}\n    outputs: {decision: {type: string}}\n", false},
		{"prompt not string", "    config: {prompt: 3}\n    outputs: {decision: {type: string}}\n", false},
		{"prompt too long", "    config: {prompt: \"" + strings.Repeat("a", 1025) + "\"}\n    outputs: {decision: {type: string}}\n", false},
		{"context input undeclared", "    config: {context_inputs: [missing]}\n    outputs: {decision: {type: string}}\n", false},
		{"context input not string", "    config: {context_inputs: [data]}\n    inputs: {data: {type: json}}\n    outputs: {decision: {type: string}}\n", false},
		{"on_reject fail", "    config: {on_reject: fail}\n    outputs: {decision: {type: string}}\n", true},
		{"on_reject unknown value", "    config: {on_reject: retry}\n    outputs: {decision: {type: string}}\n", false},
		{"on_reject not string", "    config: {on_reject: true}\n    outputs: {decision: {type: string}}\n", false},
		{"context input ok", "    config: {context_inputs: [note]}\n    inputs: {note: {type: string}}\n    outputs: {decision: {type: string}}\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := yamlgraph.Parse([]byte(humanDoc(tt.body)))
			if err != nil {
				t.Fatal(err)
			}
			result := graph.Validate(d)
			hasHumanError := false
			for _, diag := range result.Diagnostics {
				if diag.Code == "E_HUMAN_INVALID" {
					hasHumanError = true
				}
			}
			if hasHumanError == tt.ok {
				t.Fatalf("ok=%v diagnostics=%v", tt.ok, result.Diagnostics)
			}
		})
	}
}

func TestHumanNodeCannotLoop(t *testing.T) {
	body := "    outputs: {decision: {type: string}}\n    loop:\n      gate:\n        inputs: {v: {type: string, required: true}}\n        config: {check_id: c, kind: json_predicate}\n      bindings: [{from: candidate.outputs.decision, to: gate.inputs.v}]\n"
	d, err := yamlgraph.Parse([]byte(humanDoc(body)))
	if err != nil {
		t.Fatal(err)
	}
	if graph.Validate(d).Valid() {
		t.Fatal("a human node with a loop must be invalid")
	}
}

// With on_reject: continue every node bound to the decision runs on a
// rejection, and no node kind can judge the verdict string: a Shell, Agent or
// loop node runs its side effects first, and a command Gate compares files,
// not values. So the decision may reach graph outputs only (ADR-0043).
//
// roadmap: F2.10-AC2
func TestHumanOnRejectContinueExposesTheDecisionOnlyAsAGraphOutput(t *testing.T) {
	const head = `schema_version: 0
id: h
inputs: {}
nodes:
  ask:
    type: human
    config: {on_reject: continue}
    outputs: {decision: {type: string, required: true}}
`
	const shellReads = `  after:
    type: shell
    config: {command: /bin/true, input_env: {verdict: VERDICT}}
    inputs: {verdict: {type: string, required: true}}
    outputs: {}
`
	const gate = `  check:
    type: gate
    config: {check_id: approved, kind: command, command: /usr/bin/diff, left_input: verdict, right_input: verdict}
    inputs: {verdict: {type: string, required: true}}
    outputs: {}
`
	const loop = `  fix:
    type: shell
    config: {command: /bin/true}
    inputs: {verdict: {type: string, required: true}}
    outputs: {stdout: {type: string, required: true}}
    loop:
      max_attempts: 2
      gate:
        inputs: {verdict: {type: string, required: true}, candidate: {type: string, required: true}}
        config: {check_id: approved, kind: command, command: /usr/bin/diff, left_input: verdict, right_input: candidate}
      bindings:
        - {from: node.inputs.verdict, to: gate.inputs.verdict}
        - {from: candidate.outputs.stdout, to: gate.inputs.candidate}
`
	const toShell = "  - {from: ask.outputs.decision, to: after.inputs.verdict}\n"
	const toGate = "  - {from: ask.outputs.decision, to: check.inputs.verdict}\n"
	for _, tt := range []struct {
		name, doc string
		ok        bool
	}{
		{"graph output", head + "edges: []\noutputs: {verdict: {from: ask.outputs.decision}}\n", true},
		{"no consumer", head + "edges: []\noutputs: {}\n", false},
		{"shell consumer", head + shellReads + "edges:\n" + toShell + "outputs: {}\n", false},
		{"shell beside a graph output", head + shellReads + "edges:\n" + toShell + "outputs: {verdict: {from: ask.outputs.decision}}\n", false},
		{"gate consumer", head + gate + "edges:\n" + toGate + "outputs: {}\n", false},
		{"gate beside a graph output", head + gate + "edges:\n" + toGate + "outputs: {verdict: {from: ask.outputs.decision}}\n", false},
		{"loop forwarding to its gate", head + loop + "edges: [{from: ask.outputs.decision, to: fix.inputs.verdict}]\noutputs: {}\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := yamlgraph.Parse([]byte(tt.doc))
			if err != nil {
				t.Fatal(err)
			}
			result := graph.Validate(d)
			bad := false
			for _, diag := range result.Diagnostics {
				bad = bad || diag.Code == "E_HUMAN_REJECT_UNROUTED"
			}
			if bad == tt.ok {
				t.Fatalf("ok=%v diagnostics=%v", tt.ok, result.Diagnostics)
			}
		})
	}
}

// Several E_HUMAN_INVALID errors share the node's path, so they must come out
// in the same order on every validation (deterministic diagnostics, F0.2).
func TestHumanConfigErrorsHaveAStableOrder(t *testing.T) {
	body := "    config: {prompt: 5, on_reject: retry, context_inputs: 7}\n    outputs: {decision: {type: string}}\n"
	d, err := yamlgraph.Parse([]byte(humanDoc(body)))
	if err != nil {
		t.Fatal(err)
	}
	first := fmt.Sprint(graph.Validate(d).Diagnostics)
	for range 100 {
		if got := fmt.Sprint(graph.Validate(d).Diagnostics); got != first {
			t.Fatalf("diagnostics changed order:\n%s\n%s", first, got)
		}
	}
}
