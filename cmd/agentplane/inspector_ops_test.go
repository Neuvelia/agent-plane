package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/adapter/yamlgraph/edit"
	"agentplane/internal/domain/graph"
	"agentplane/internal/gate"
)

// F2.2: every Gate kind the catalogue offers can be configured with inspector
// operations alone into a graph that the one validation path (agentplane graph
// validate) accepts. The kinds are read from the served descriptors, so a kind
// added to the Gate fails these tests until a case below covers it.

// loopProducer names what a producer type offers the loop's Gate: a string
// output and a json output (with the operations that declare them), and the
// start-graph node that has a required string input `ref`.
type loopProducer struct {
	node      string
	stringOut string
	jsonOut   string
	prep      []edit.Op
}

// gateKindCase configures one Gate kind: its declared inputs, its config and
// which candidate output / node input feeds each input.
type gateKindCase struct {
	inputs   map[string]edit.PortSpec
	config   map[string]any
	bindings func(p loopProducer) []edit.LoopBinding
	// needsRequiredString makes the candidate's string output required first.
	needsRequiredString bool
}

const gateCheckID = "check"

func inspectorStartGraph(dir string) string {
	return fmt.Sprintf(`schema_version: 0
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
      stdout_json: true
    outputs:
      stdout: {type: string}
      stderr: {type: string}
      json: {type: json}
  fix:
    type: agent
    inputs:
      ref: {type: string, required: true}
    config:
      adapter: codex
      workspace: %q
      task: fix it
      allowed_paths: [src]
edges: []
graph_input_bindings:
  - {from: inputs.ref, to: build.inputs.ref}
  - {from: inputs.ref, to: fix.inputs.ref}
outputs: {}
`, dir)
}

func loopProducers() map[string]loopProducer {
	return map[string]loopProducer{
		"shell": {node: "build", stringOut: "stdout", jsonOut: "json"},
		"agent": {node: "fix", stringOut: "summary", jsonOut: "changed_files", prep: []edit.Op{
			{Op: edit.OpAddPort, Node: "fix", Direction: edit.DirectionOutput, Port: "summary", Type: "string"},
			{Op: edit.OpAddPort, Node: "fix", Direction: edit.DirectionOutput, Port: "changed_files", Type: "json"},
		}},
	}
}

func gateKindCases(dir string) map[string]gateKindCase {
	req := edit.PortSpec{Type: "string", Required: true}
	reqJSON := edit.PortSpec{Type: "json", Required: true}
	from := func(out, in string) edit.LoopBinding {
		return edit.LoopBinding{From: out, To: in}
	}
	return map[string]gateKindCase{
		gate.KindCommand: {
			inputs: map[string]edit.PortSpec{"left": req, "right": req},
			config: map[string]any{"command": "/usr/bin/diff", "cwd": dir, "left_input": "left", "right_input": "right"},
			bindings: func(p loopProducer) []edit.LoopBinding {
				return []edit.LoopBinding{from("candidate.outputs."+p.stringOut, "gate.inputs.left"), from("node.inputs.ref", "gate.inputs.right")}
			},
		},
		gate.KindJSONPredicate: {
			inputs: map[string]edit.PortSpec{"doc": reqJSON},
			config: map[string]any{"input": "doc", "field": []string{"status"}, "operator": "eq", "expected": "ok"},
			bindings: func(p loopProducer) []edit.LoopBinding {
				return []edit.LoopBinding{from("candidate.outputs."+p.jsonOut, "gate.inputs.doc")}
			},
		},
		gate.KindReportedFileScope: {
			inputs: map[string]edit.PortSpec{"files": reqJSON},
			config: map[string]any{"input": "files", "allowed_paths": []string{"src/"}},
			bindings: func(p loopProducer) []edit.LoopBinding {
				return []edit.LoopBinding{from("candidate.outputs."+p.jsonOut, "gate.inputs.files")}
			},
		},
		gate.KindChangedFilesScope: {
			inputs: map[string]edit.PortSpec{"candidate": req},
			config: map[string]any{"input": "candidate", "allowed_paths": []string{"src/"}, "mode": "warn", "workspace": dir},
			bindings: func(p loopProducer) []edit.LoopBinding {
				return []edit.LoopBinding{from("candidate.outputs."+p.stringOut, "gate.inputs.candidate")}
			},
			needsRequiredString: true,
		},
	}
}

func validateEdited(t *testing.T, start string, ops []edit.Op) {
	t.Helper()
	res, err := edit.Apply([]byte(start), ops)
	if err != nil {
		t.Fatalf("operations refused: %v", err)
	}
	def, err := yamlgraph.Parse(res.Text)
	if err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, res.Text)
	}
	var errs []string
	for _, d := range validateGraph(def).Diagnostics {
		if d.Severity == graph.SeverityError {
			errs = append(errs, d.Code+": "+d.Message)
		}
	}
	if len(errs) > 0 {
		t.Errorf("graph validate reports %s\n%s", strings.Join(errs, "; "), res.Text)
	}
}

func withCheckID(t *testing.T, kind string, cfg map[string]any) json.RawMessage {
	t.Helper()
	full := map[string]any{"check_id": gateCheckID, "kind": kind}
	for k, v := range cfg {
		full[k] = v
	}
	b, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEveryLoopGateKindCanBeConfiguredByOperations(t *testing.T) {
	dir := t.TempDir()
	start := inspectorStartGraph(dir)
	cases := gateKindCases(dir)
	producers := loopProducers()
	covered := 0
	for _, nt := range nodeCatalogue() {
		if nt.Loop == nil {
			continue
		}
		p, ok := producers[nt.Type]
		if !ok {
			t.Fatalf("node type %q can loop and has no producer in loopProducers", nt.Type)
		}
		enum := kindEnum(t, nt.Loop.Gate)
		for _, kind := range enum {
			c, ok := cases[kind]
			if !ok {
				t.Fatalf("%s loop offers gate kind %q and no case in gateKindCases configures it", nt.Type, kind)
			}
			covered++
			t.Run(nt.Type+"/"+kind, func(t *testing.T) {
				ops := append([]edit.Op(nil), p.prep...)
				if c.needsRequiredString {
					tru := true
					ops = append(ops, edit.Op{Op: edit.OpUpdatePort, Node: p.node, Direction: edit.DirectionOutput, Port: p.stringOut, Required: &tru})
				}
				ops = append(ops, edit.Op{Op: edit.OpSetLoop, Node: p.node,
					Gate:     &edit.LoopGate{Inputs: c.inputs, Config: withCheckID(t, kind, c.config)},
					Bindings: c.bindings(p)})
				validateEdited(t, start, ops)
			})
		}
	}
	if covered == 0 {
		t.Fatal("the catalogue offers no loop gate kind")
	}
	assertCasesMatchDescriptors(t, cases)
}

func TestEveryStandaloneGateKindCanBeConfiguredByOperations(t *testing.T) {
	dir := t.TempDir()
	start := inspectorStartGraph(dir)
	cases := gateKindCases(dir)
	// Which producer outputs feed a standalone Gate input, by input type.
	feed := map[string][]string{"string": {"build.outputs.stdout", "build.outputs.stderr"}, "json": {"build.outputs.json"}}
	for _, kind := range kindEnum(t, catalogueByType(t)["gate"].Config) {
		c, ok := cases[kind]
		if !ok {
			t.Fatalf("a standalone Gate offers kind %q and no case in gateKindCases configures it", kind)
		}
		t.Run(kind, func(t *testing.T) {
			ops := []edit.Op{{Op: edit.OpAddNode, ID: "chk", Type: "gate"}}
			names := make([]string, 0, len(c.inputs))
			for name := range c.inputs {
				names = append(names, name)
			}
			sort.Strings(names)
			next := map[string]int{}
			for _, name := range names {
				spec := c.inputs[name]
				sources := feed[spec.Type]
				if len(sources) == 0 {
					t.Fatalf("no producer output feeds an input of type %q", spec.Type)
				}
				ops = append(ops, edit.Op{Op: edit.OpAddPort, Node: "chk", Direction: edit.DirectionInput, Port: name, Type: spec.Type, Required: &spec.Required})
				ops = append(ops, edit.Op{Op: edit.OpAddEdge, From: sources[next[spec.Type]%len(sources)], To: "chk.inputs." + name})
				next[spec.Type]++
			}
			full := map[string]any{}
			if err := json.Unmarshal(withCheckID(t, kind, c.config), &full); err != nil {
				t.Fatal(err)
			}
			keys := make([]string, 0, len(full))
			for k := range full {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v, err := json.Marshal(full[k])
				if err != nil {
					t.Fatal(err)
				}
				ops = append(ops, edit.Op{Op: edit.OpSetConfig, Node: "chk", Path: []string{k}, Value: v})
			}
			validateEdited(t, start, ops)
		})
	}
	assertCasesMatchDescriptors(t, cases)
}

// The Gate's own kind enum is the reference in both directions: a kind it adds
// fails until a case configures it, and a case for a kind it dropped is dead.
func assertCasesMatchDescriptors(t *testing.T, cases map[string]gateKindCase) {
	t.Helper()
	all := map[string]bool{}
	for _, k := range kindEnum(t, gate.ConfigFields()) {
		all[k] = true
		if _, ok := cases[k]; !ok {
			t.Errorf("the Gate descriptors list kind %q and no case in gateKindCases configures it", k)
		}
	}
	for kind := range cases {
		if !all[kind] {
			t.Errorf("gateKindCases covers %q, which the Gate descriptors do not list", kind)
		}
	}
}
