package edit

import (
	"encoding/json"
	"strings"
	"testing"

	"agentplane/internal/graph"
)

// inspectorGraph has every node type, a loop, edges, graph inputs and
// outputs, comments, and config keys no descriptor lists (zz_*).
const inspectorGraph = `schema_version: 0
id: inspect
inputs:
  topic: {type: string, required: true}

nodes:
  # produces the draft
  write:
    type: shell
    inputs:
      topic: {type: string, required: true}
    config:
      command: /bin/echo # the tool
      args: [hello]
      input_env: {topic: TOPIC}
      zz_extra: {deep: [1, {x: y}]}
    outputs:
      stdout: {type: string}
      json: {type: json}
    loop:
      max_attempts: 2
      gate:
        inputs:
          doc: {type: json, required: true}
        config:
          check_id: ok
          kind: json_predicate
          input: doc
          field: [status]
          operator: eq
          expected: ok
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.doc}

  check:
    type: gate
    inputs:
      left: {type: string, required: true}
      right: {type: string, required: true}
    config:
      check_id: same
      kind: command
      command: /usr/bin/diff
      cwd: /tmp
      left_input: left
      right_input: right
    outputs:
      verdict: {type: string}

  approve:
    type: human
    inputs:
      summary: {type: string, required: true}
    config:
      prompt: Ship it?
      context_inputs: [summary]
      zz_note: keep me
    outputs:
      decision: {type: string, required: true}

edges:
  - {from: write.outputs.stdout, to: check.inputs.left}
  - {from: write.outputs.stdout, to: approve.inputs.summary}
graph_input_bindings:
  - {from: inputs.topic, to: write.inputs.topic}
outputs:
  draft: {from: write.outputs.stdout}
`

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func nodeOf(t *testing.T, src, id string) graph.NodeDefinition {
	t.Helper()
	n, ok := parse(t, src).Nodes[id]
	if !ok {
		t.Fatalf("node %s missing:\n%s", id, src)
	}
	return n
}

func TestSetConfigChangesOnlyTheValueLine(t *testing.T) {
	out := mustApply(t, inspectorGraph, Op{Op: OpSetConfig, Node: "write", Path: []string{"command"}, Value: raw("/bin/printf")})
	removed, added := lineDiff(inspectorGraph, out)
	if len(removed) != 1 || len(added) != 1 || added[0] != "      command: /bin/printf # the tool" {
		t.Fatalf("diff -%q +%q", removed, added)
	}
}

func TestSetConfigWritesEachJSONKindAsTheEngineReadsIt(t *testing.T) {
	cases := []struct {
		value any
		want  any
	}{
		{"true", "true"}, // a string that looks like a boolean stays a string
		{"two\nlines", "two\nlines"},
		{true, true},
		{nil, nil},
		{json.Number("3"), 3},
		{json.Number("2.0"), 2.0},
		{json.Number("1e3"), 1000.0},
		{[]any{"a", json.Number("1")}, []any{"a", 1}},
		{map[string]any{"b": "x", "a": []any{}}, map[string]any{"b": "x", "a": []any{}}},
	}
	for _, c := range cases {
		out := mustApply(t, inspectorGraph, Op{Op: OpSetConfig, Node: "write", Path: []string{"zz_value"}, Value: raw(c.value)})
		got := nodeOf(t, out, "write").Config["zz_value"]
		if !equalJSON(got, c.want) {
			t.Errorf("value %#v read back as %#v (%T)", c.value, got, got)
		}
	}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y) && sameKind(a, b)
}

func sameKind(a, b any) bool {
	switch a.(type) {
	case int:
		_, ok := b.(int)
		return ok
	case float64:
		_, ok := b.(float64)
		return ok
	}
	return true
}

func TestSetConfigKeepsObjectKeyOrderAndCreatesParents(t *testing.T) {
	out := mustApply(t, inspectorGraph, Op{Op: OpSetConfig, Node: "write", Path: []string{"env", "B"}, Value: json.RawMessage(`"2"`)},
		Op{Op: OpSetConfig, Node: "write", Path: []string{"zz_obj"}, Value: json.RawMessage(`{"z": 1, "a": 2}`)})
	if !strings.Contains(out, "      env:\n        B: \"2\"\n") {
		t.Errorf("env was not created as a block mapping:\n%s", out)
	}
	if strings.Index(out, "z: 1") > strings.Index(out, "a: 2") {
		t.Errorf("object keys lost their order:\n%s", out)
	}
}

func TestSetConfigCreatesTheConfigMapping(t *testing.T) {
	src := strings.Replace(twoNodes, "    config:\n      command: \"true\"\n    outputs:", "    outputs:", 1)
	out := mustApply(t, src, Op{Op: OpSetConfig, Node: "a", Path: []string{"command"}, Value: raw("ls")})
	if !strings.Contains(out, "    type: shell\n    config:\n      command: ls\n    outputs:") {
		t.Errorf("config not inserted after type:\n%s", out)
	}
}

func TestSetConfigWritesAFloatReadably(t *testing.T) {
	out := mustApply(t, inspectorGraph, Op{Op: OpSetConfig, Node: "write", Path: []string{"zz_value"}, Value: json.RawMessage(`2.0`)})
	if !strings.Contains(out, "      zz_value: 2.0\n") {
		t.Errorf("float not written as 2.0:\n%s", out)
	}
}

func TestSetConfigWithTheSameValueInAnotherSpellingIsByteIdentical(t *testing.T) {
	src := strings.Replace(inspectorGraph, "cwd: /tmp", "cwd: '/tmp' # quoted", 1)
	if out := mustApply(t, src, Op{Op: OpSetConfig, Node: "check", Path: []string{"cwd"}, Value: raw("/tmp")}); out != src {
		removed, added := lineDiff(src, out)
		t.Errorf("an equal value rewrote the line: -%q +%q", removed, added)
	}
}

func TestSetConfigWithTheSameValueIsByteIdentical(t *testing.T) {
	for _, op := range []Op{
		{Op: OpSetConfig, Node: "write", Path: []string{"command"}, Value: raw("/bin/echo")},
		{Op: OpSetConfig, Node: "write", Path: []string{"args"}, Value: raw([]string{"hello"})},
		{Op: OpUnsetConfig, Node: "write", Path: []string{"absent"}},
		{Op: OpUnsetConfig, Node: "write", Path: []string{"command", "below"}},
		{Op: OpClearLoop, Node: "check"},
	} {
		if out := mustApply(t, inspectorGraph, op); out != inspectorGraph {
			removed, added := lineDiff(inspectorGraph, out)
			t.Errorf("%+v changed the file: -%q +%q", op, removed, added)
		}
	}
}

func TestUnsetConfigRemovesTheKeyAndEmptiedParents(t *testing.T) {
	out := mustApply(t, inspectorGraph, Op{Op: OpUnsetConfig, Node: "write", Path: []string{"zz_extra", "deep"}})
	if strings.Contains(out, "zz_extra") {
		t.Errorf("an emptied parent mapping stayed:\n%s", out)
	}
	if n := nodeOf(t, out, "write"); n.Config["command"] != "/bin/echo" {
		t.Errorf("unrelated config changed: %v", n.Config)
	}
}

func TestConfigRefusals(t *testing.T) {
	cases := []struct {
		op   Op
		code string
	}{
		{Op{Op: OpSetConfig, Node: "nope", Path: []string{"a"}, Value: raw(1)}, CodeNodeNotFound},
		{Op{Op: OpSetConfig, Node: "write", Path: []string{"command", "x"}, Value: raw(1)}, CodeConfigPath},
		{Op{Op: OpSetConfig, Node: "write", Path: []string{"a"}, Value: json.RawMessage(`{"a":1,"a":2}`)}, CodeValue},
		{Op{Op: OpSetConfig, Node: "write", Path: []string{"a"}, Value: json.RawMessage(`1e999`)}, CodeValue},
		{Op{Op: OpSetConfig, Node: "write", Path: []string{"a"}, Value: json.RawMessage(strings.Repeat("[", 40) + strings.Repeat("]", 40))}, CodeValue},
	}
	for _, c := range cases {
		if e := applyErr(t, inspectorGraph, c.op); e.Code != c.code {
			t.Errorf("%+v: code %s, want %s (%v)", c.op, e.Code, c.code, e)
		}
	}
}

// A null intermediate key is replaced by a mapping in the typed prediction as
// in the tree, so set_config below it applies.
func TestSetConfigBelowANullKey(t *testing.T) {
	src := strings.Replace(inspectorGraph, "      zz_extra: {deep: [1, {x: y}]}\n", "      zz_extra: {deep: [1, {x: y}]}\n      zz_null:\n", 1)
	out := mustApply(t, src, Op{Op: OpSetConfig, Node: "write", Path: []string{"zz_null", "k"}, Value: raw(1)})
	if m, ok := asMapping(nodeOf(t, out, "write").Config["zz_null"]); !ok || m["k"] == nil {
		t.Errorf("zz_null.k not set:\n%s", out)
	}
}

// Removing or renaming an input that an empty input_env or context_inputs
// does not name leaves that key as written.
func TestUnrelatedPortEditKeepsEmptyReferenceLists(t *testing.T) {
	src := strings.Replace(inspectorGraph, "      input_env: {topic: TOPIC}\n", "      input_env: {}\n", 1)
	src = strings.Replace(src, "      context_inputs: [summary]\n", "      context_inputs: []\n", 1)
	src = strings.Replace(src, "      summary: {type: string, required: true}\n", "      summary: {type: string, required: true}\n      other: {type: string}\n", 1)
	for _, op := range []Op{
		{Op: OpRemovePort, Node: "write", Direction: DirectionInput, Port: "topic"},
		{Op: OpUpdatePort, Node: "write", Direction: DirectionInput, Port: "topic", NewPort: "subject"},
		{Op: OpRemovePort, Node: "approve", Direction: DirectionInput, Port: "other"},
		{Op: OpUpdatePort, Node: "approve", Direction: DirectionInput, Port: "other", NewPort: "more"},
	} {
		out := mustApply(t, src, op)
		for _, keep := range []string{"input_env: {}", "context_inputs: []"} {
			if !strings.Contains(out, keep) {
				t.Errorf("%+v dropped %q:\n%s", op, keep, out)
			}
		}
	}
}

// Renaming a Shell input onto a name input_env already has is refused with a
// structured error, not by the safety net.
func TestRenameInputOntoAnInputEnvKeyIsRefused(t *testing.T) {
	src := strings.Replace(inspectorGraph, "      input_env: {topic: TOPIC}\n", "      input_env: {topic: TOPIC, subject: SUBJECT}\n", 1)
	e := applyErr(t, src, Op{Op: OpUpdatePort, Node: "write", Direction: DirectionInput, Port: "topic", NewPort: "subject"})
	if e.Code != CodePortExists || e.Path != "nodes.write.inputs.topic" {
		t.Errorf("got %s at %s (%v)", e.Code, e.Path, e)
	}
}

func TestAddPortMatchesExistingStyle(t *testing.T) {
	out := mustApply(t, inspectorGraph, Op{Op: OpAddPort, Node: "write", Direction: DirectionOutput, Port: "stderr", Type: "string"},
		Op{Op: OpAddPort, Node: "check", Direction: DirectionInput, Port: "extra", Type: "json", Required: boolPtr(true)})
	_, added := lineDiff(inspectorGraph, out)
	want := []string{"      stderr: {type: string}", "      extra: {type: json, required: true}"}
	if strings.Join(added, "\n") != strings.Join(want, "\n") {
		t.Errorf("added %q, want %q", added, want)
	}
}

func TestPortRefusals(t *testing.T) {
	cases := []struct {
		op   Op
		code string
	}{
		{Op{Op: OpAddPort, Node: "write", Direction: DirectionOutput, Port: "stdout", Type: "string"}, CodePortExists},
		{Op{Op: OpAddPort, Node: "write", Direction: DirectionOutput, Port: "Bad", Type: "string"}, CodePortInvalid},
		{Op{Op: OpAddPort, Node: "write", Direction: DirectionOutput, Port: "x", Type: "text"}, CodePortInvalid},
		{Op{Op: OpUpdatePort, Node: "write", Direction: DirectionOutput, Port: "nope", Type: "string"}, CodePortNotFound},
		{Op{Op: OpUpdatePort, Node: "write", Direction: DirectionOutput, Port: "stdout", NewPort: "json"}, CodePortExists},
		{Op{Op: OpUpdatePort, Node: "write", Direction: DirectionOutput, Port: "stdout", Type: "number"}, CodePortConflict},
		{Op{Op: OpUpdatePort, Node: "write", Direction: DirectionOutput, Port: "json", Type: "string"}, CodePortConflict},
		{Op{Op: OpRemovePort, Node: "write", Direction: DirectionInput, Port: "nope"}, CodePortNotFound},
		{Op{Op: OpRemovePort, Node: "nope", Direction: DirectionInput, Port: "x"}, CodeNodeNotFound},
	}
	for _, c := range cases {
		if e := applyErr(t, inspectorGraph, c.op); e.Code != c.code {
			t.Errorf("%+v: code %s, want %s (%v)", c.op, e.Code, c.code, e)
		}
	}
}

func TestUpdatePortTypeAndRequiredTouchOnlyWhatChanged(t *testing.T) {
	out := mustApply(t, inspectorGraph, Op{Op: OpUpdatePort, Node: "check", Direction: DirectionInput, Port: "left", Required: boolPtr(false)})
	_, added := lineDiff(inspectorGraph, out)
	if len(added) != 1 || added[0] != "      left: {type: string}" {
		t.Errorf("added %q", added)
	}
}

// danglingCodes are the validator's reference errors a port edit must not leave.
var danglingCodes = map[string]bool{
	"E_EDGE_SOURCE": true, "E_EDGE_TARGET": true, "E_GRAPH_INPUT_TARGET_INPUT_NOT_FOUND": true,
	"E_GRAPH_OUTPUT_SOURCE_OUTPUT_NOT_FOUND": true, "E_LOOP_INVALID": true, "E_HUMAN_INVALID": true,
}

// dangling lists reference errors src has that inspectorGraph did not (the
// fixture's deliberate unknown keys already have diagnostics).
func dangling(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, d := range newErrors(parse(t, inspectorGraph), parse(t, src)) {
		if danglingCodes[d.Code] {
			out = append(out, d.Code+": "+d.Message)
		}
	}
	return out
}

func TestRenamePortUpdatesEveryReference(t *testing.T) {
	cases := []struct {
		op    Op
		gone  string
		shows []string
	}{
		{Op{Op: OpUpdatePort, Node: "write", Direction: DirectionOutput, Port: "stdout", NewPort: "text"}, "write.outputs.stdout",
			[]string{"{from: write.outputs.text, to: check.inputs.left}", "draft: {from: write.outputs.text}"}},
		{Op{Op: OpUpdatePort, Node: "write", Direction: DirectionOutput, Port: "json", NewPort: "doc"}, "candidate.outputs.json",
			[]string{"{from: candidate.outputs.doc, to: gate.inputs.doc}"}},
		{Op{Op: OpUpdatePort, Node: "write", Direction: DirectionInput, Port: "topic", NewPort: "subject"}, "write.inputs.topic",
			[]string{"to: write.inputs.subject}", "input_env: {subject: TOPIC}"}},
		{Op{Op: OpUpdatePort, Node: "check", Direction: DirectionInput, Port: "left", NewPort: "before"}, "left_input: left",
			[]string{"left_input: before", "to: check.inputs.before}"}},
		{Op{Op: OpUpdatePort, Node: "approve", Direction: DirectionInput, Port: "summary", NewPort: "brief"}, "[summary]",
			[]string{"context_inputs: [brief]", "to: approve.inputs.brief}"}},
	}
	for _, c := range cases {
		out := mustApply(t, inspectorGraph, c.op)
		if strings.Contains(out, c.gone) {
			t.Errorf("%s/%s: %q remains:\n%s", c.op.Node, c.op.Port, c.gone, out)
		}
		for _, s := range c.shows {
			if !strings.Contains(out, s) {
				t.Errorf("%s/%s: %q missing:\n%s", c.op.Node, c.op.Port, s, out)
			}
		}
		if d := dangling(t, out); len(d) > 0 {
			t.Errorf("%s/%s: dangling references %v", c.op.Node, c.op.Port, d)
		}
	}
}

func TestRemovePortRemovesEveryReferenceInOneBatch(t *testing.T) {
	cases := []Op{
		{Op: OpRemovePort, Node: "write", Direction: DirectionOutput, Port: "stdout"},
		{Op: OpRemovePort, Node: "write", Direction: DirectionInput, Port: "topic"},
		{Op: OpRemovePort, Node: "approve", Direction: DirectionInput, Port: "summary"},
	}
	for _, op := range cases {
		out := mustApply(t, inspectorGraph, op)
		ref := op.Node + "." + portSection(op.Direction) + "." + op.Port
		if strings.Contains(out, ref) {
			t.Errorf("%s remains:\n%s", ref, out)
		}
		if d := dangling(t, out); len(d) > 0 {
			t.Errorf("%s: dangling references %v\n%s", ref, d, out)
		}
	}
	out := mustApply(t, inspectorGraph, Op{Op: OpRemovePort, Node: "check", Direction: DirectionInput, Port: "left"})
	if strings.Contains(out, "left_input") {
		t.Errorf("the gate still names the removed input:\n%s", out)
	}
}

func TestSetLoopWritesABlockTheEngineReads(t *testing.T) {
	op := Op{Op: OpSetLoop, Node: "write", MaxAttempts: intPtr(3),
		Gate: &LoopGate{Inputs: map[string]PortSpec{"doc": {Type: "json", Required: true}},
			Config: json.RawMessage(`{"check_id":"c","kind":"json_predicate","input":"doc","field":["ok"],"operator":"eq","expected":true}`)},
		Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}}}
	out := mustApply(t, inspectorGraph, op)
	n := nodeOf(t, out, "write")
	if n.Loop == nil || n.Loop.MaxAttempts != 3 || n.Loop.Gate.Config["expected"] != true || len(n.Loop.Bindings) != 1 {
		t.Fatalf("loop not as set: %+v", n.Loop)
	}
	src := strings.Replace(twoNodes, "      count: {type: number}\n", "      count: {type: number}\n      json: {type: json}\n", 1)
	added := mustApply(t, src, Op{Op: OpSetLoop, Node: "a", Gate: op.Gate, Bindings: op.Bindings})
	if !strings.Contains(added, "    type: shell\n    loop:\n      gate:\n        inputs:\n          doc: {type: json, required: true}\n        config:\n          check_id: c\n") {
		t.Errorf("loop not inserted after type in the expected shape:\n%s", added)
	}
	if strings.Contains(added, "max_attempts") {
		t.Errorf("an absent max_attempts was written:\n%s", added)
	}
	cleared := mustApply(t, out, Op{Op: OpClearLoop, Node: "write"})
	if nodeOf(t, cleared, "write").Loop != nil || strings.Contains(cleared, "loop:") {
		t.Errorf("clear_loop left a loop:\n%s", cleared)
	}
}

func TestSetLoopRefusesBindingsTheValidatorWouldRefuse(t *testing.T) {
	gate := func(inputs map[string]PortSpec) *LoopGate {
		return &LoopGate{Inputs: inputs, Config: json.RawMessage(`{"check_id":"c","kind":"json_predicate"}`)}
	}
	doc := map[string]PortSpec{"doc": {Type: "json", Required: true}}
	two := map[string]PortSpec{"doc": {Type: "json"}, "text": {Type: "string"}}
	cases := []struct {
		name string
		op   Op
	}{
		{"not a producer", Op{Op: OpSetLoop, Node: "check", Gate: gate(map[string]PortSpec{"v": {Type: "string", Required: true}}), Bindings: []LoopBinding{{From: "candidate.outputs.verdict", To: "gate.inputs.v"}}}},
		{"human cannot loop", Op{Op: OpSetLoop, Node: "approve", Gate: gate(map[string]PortSpec{"v": {Type: "string", Required: true}}), Bindings: []LoopBinding{{From: "candidate.outputs.decision", To: "gate.inputs.v"}}}},
		{"budget too small", Op{Op: OpSetLoop, Node: "write", MaxAttempts: intPtr(0), Gate: gate(doc), Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}}}},
		{"budget too large", Op{Op: OpSetLoop, Node: "write", MaxAttempts: intPtr(11), Gate: gate(doc), Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}}}},
		{"unequal types", Op{Op: OpSetLoop, Node: "write", Gate: gate(doc), Bindings: []LoopBinding{{From: "candidate.outputs.stdout", To: "gate.inputs.doc"}}}},
		{"duplicate target", Op{Op: OpSetLoop, Node: "write", Gate: gate(two), Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}, {From: "candidate.outputs.json", To: "gate.inputs.doc"}}}},
		{"no candidate output", Op{Op: OpSetLoop, Node: "write", Gate: gate(two), Bindings: []LoopBinding{{From: "node.inputs.topic", To: "gate.inputs.text"}}}},
		{"undeclared source", Op{Op: OpSetLoop, Node: "write", Gate: gate(doc), Bindings: []LoopBinding{{From: "candidate.outputs.nope", To: "gate.inputs.doc"}}}},
		{"undeclared target", Op{Op: OpSetLoop, Node: "write", Gate: gate(doc), Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.nope"}}}},
		{"config not an object", Op{Op: OpSetLoop, Node: "write", Gate: &LoopGate{Inputs: doc, Config: json.RawMessage(`[1]`)}, Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}}}},
	}
	for _, c := range cases {
		if e := applyErr(t, inspectorGraph, c.op); e.Code != CodeLoopInvalid {
			t.Errorf("%s: code %s, want %s (%v)", c.name, e.Code, CodeLoopInvalid, e)
		}
	}
}

func TestSetUnits(t *testing.T) {
	out := mustApply(t, inspectorGraph, Op{Op: OpSetUnits, Units: []string{"write", "approve"}})
	if !strings.Contains(out, "id: inspect\nunits: [write, approve]\ninputs:") {
		t.Errorf("units not inserted in canonical place:\n%s", out)
	}
	if got := parse(t, out).Units; len(got) != 2 {
		t.Errorf("units read back as %v", got)
	}
	if cleared := mustApply(t, out, Op{Op: OpSetUnits, Units: []string{}}); cleared != inspectorGraph {
		t.Errorf("clearing units did not restore the file:\n%s", cleared)
	}
	for _, units := range [][]string{{"nope"}, {"write", "write"}} {
		if e := applyErr(t, inspectorGraph, Op{Op: OpSetUnits, Units: units}); e.Code != CodeUnitsInvalid {
			t.Errorf("%v: code %s", units, e.Code)
		}
	}
}

func TestDecodeChecksTheNewOperations(t *testing.T) {
	bad := []string{
		`[{"op":"set_config","node":"a","path":["x"]}]`,
		`[{"op":"set_config","node":"a","path":[],"value":1}]`,
		`[{"op":"set_config","node":"a","path":[""],"value":1}]`,
		`[{"op":"unset_config","path":["x"]}]`,
		`[{"op":"add_port","node":"a","direction":"sideways","port":"p","type":"string"}]`,
		`[{"op":"add_port","node":"a","direction":"input","port":"p"}]`,
		`[{"op":"update_port","node":"a","direction":"input","port":"p"}]`,
		`[{"op":"remove_port","node":"a","direction":"input"}]`,
		`[{"op":"set_loop","node":"a"}]`,
		`[{"op":"clear_loop"}]`,
		`[{"op":"set_units"}]`,
		`[{"op":"set_loop","node":"a","gate":{"config":{},"extra":1}}]`,
	}
	for _, b := range bad {
		if _, err := Decode([]byte(b)); err == nil {
			t.Errorf("Decode accepted %s", b)
		}
	}
	good := `[{"op":"set_units","units":[]},{"op":"set_config","node":"a","path":["x","y"],"value":null}]`
	if _, err := Decode([]byte(good)); err != nil {
		t.Errorf("Decode refused %s: %v", good, err)
	}
}
