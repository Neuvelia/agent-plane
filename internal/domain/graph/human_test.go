package graph

import (
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
		{"context input ok", "    config: {context_inputs: [note]}\n    inputs: {note: {type: string}}\n    outputs: {decision: {type: string}}\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Parse([]byte(humanDoc(tt.body)))
			if err != nil {
				t.Fatal(err)
			}
			result := Validate(d)
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
	d, err := Parse([]byte(humanDoc(body)))
	if err != nil {
		t.Fatal(err)
	}
	if Validate(d).Valid() {
		t.Fatal("a human node with a loop must be invalid")
	}
}
