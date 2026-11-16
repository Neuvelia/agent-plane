package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/graph"
	"agentplane/internal/graph/edit"
)

// F2.2: every Gate kind can be configured with inspector operations alone into
// a graph that the one validation path (agentplane graph validate) accepts.
func TestEveryGateKindCanBeConfiguredByOperations(t *testing.T) {
	dir := t.TempDir()
	start := fmt.Sprintf(`schema_version: 0
id: loops
inputs:
  ref: {type: string, required: true}
nodes:
  build:
    type: shell
    inputs:
      ref: {type: string, required: true}
    config:
      command: /bin/echo
  fix:
    type: agent
    config:
      adapter: codex
      workspace: %q
      task: fix it
      allowed_paths: [src]
edges: []
graph_input_bindings:
  - {from: inputs.ref, to: build.inputs.ref}
outputs: {}
`, dir)
	cfg := func(v map[string]any) json.RawMessage {
		v["check_id"] = "check"
		b, _ := json.Marshal(v)
		return b
	}
	tru := true
	port := func(node, dir, name, typ string) edit.Op {
		return edit.Op{Op: edit.OpAddPort, Node: node, Direction: dir, Port: name, Type: typ}
	}
	cases := map[string][]edit.Op{
		"command": {
			port("build", edit.DirectionOutput, "stdout", "string"),
			{Op: edit.OpSetLoop, Node: "build", Gate: &edit.LoopGate{
				Inputs: map[string]edit.PortSpec{"left": {Type: "string", Required: true}, "right": {Type: "string", Required: true}},
				Config: cfg(map[string]any{"kind": "command", "command": "/usr/bin/diff", "cwd": dir, "left_input": "left", "right_input": "right"})},
				Bindings: []edit.LoopBinding{{From: "candidate.outputs.stdout", To: "gate.inputs.left"}, {From: "node.inputs.ref", To: "gate.inputs.right"}}},
		},
		"json_predicate": {
			{Op: edit.OpSetConfig, Node: "build", Path: []string{"stdout_json"}, Value: json.RawMessage(`true`)},
			port("build", edit.DirectionOutput, "json", "json"),
			{Op: edit.OpSetLoop, Node: "build", Gate: &edit.LoopGate{
				Inputs: map[string]edit.PortSpec{"doc": {Type: "json", Required: true}},
				Config: cfg(map[string]any{"kind": "json_predicate", "input": "doc", "field": []string{"status"}, "operator": "eq", "expected": "ok"})},
				Bindings: []edit.LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}}},
		},
		"reported_file_scope": {
			port("fix", edit.DirectionOutput, "changed_files", "json"),
			{Op: edit.OpSetLoop, Node: "fix", Gate: &edit.LoopGate{
				Inputs: map[string]edit.PortSpec{"files": {Type: "json", Required: true}},
				Config: cfg(map[string]any{"kind": "reported_file_scope", "input": "files", "allowed_paths": []string{"src/"}})},
				Bindings: []edit.LoopBinding{{From: "candidate.outputs.changed_files", To: "gate.inputs.files"}}},
		},
		"changed_files_scope": {
			port("fix", edit.DirectionOutput, "summary", "string"),
			{Op: edit.OpUpdatePort, Node: "fix", Direction: edit.DirectionOutput, Port: "summary", Required: &tru},
			{Op: edit.OpSetLoop, Node: "fix", Gate: &edit.LoopGate{
				Inputs: map[string]edit.PortSpec{"candidate": {Type: "string", Required: true}},
				Config: cfg(map[string]any{"kind": "changed_files_scope", "input": "candidate", "allowed_paths": []string{"src/"}, "mode": "warn", "workspace": dir})},
				Bindings: []edit.LoopBinding{{From: "candidate.outputs.summary", To: "gate.inputs.candidate"}}},
		},
	}
	for kind, ops := range cases {
		res, err := edit.Apply([]byte(start), ops)
		if err != nil {
			t.Fatalf("%s: operations refused: %v", kind, err)
		}
		def, err := graph.Parse(res.Text)
		if err != nil {
			t.Fatalf("%s: result does not parse: %v", kind, err)
		}
		var errs []string
		for _, d := range validateGraph(def).Diagnostics {
			if d.Severity == graph.SeverityError {
				errs = append(errs, d.Code+": "+d.Message)
			}
		}
		if len(errs) > 0 {
			t.Errorf("%s: graph validate reports %s\n%s", kind, strings.Join(errs, "; "), res.Text)
		}
	}
}
