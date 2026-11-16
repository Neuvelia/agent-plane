package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"agentplane/internal/fields"
)

// testCatalogue stands in for the composition root's catalogue: the API never
// imports executors, so its tests use descriptors of the same shape.
func testCatalogue() []NodeType {
	kindIs := func(k string) []fields.Rule { return []fields.Rule{{Key: "kind", Equals: k}} }
	gateFields := []fields.Field{
		{ID: "gate.config.check_id", Key: "check_id", Kind: fields.String, Required: true, MaxBytes: 256},
		{ID: "gate.config.kind", Key: "kind", Kind: fields.Enum, Required: true, Enum: []string{"command", "json_predicate"}},
		{ID: "gate.config.command", Kind: fields.Group, Requires: kindIs("command"), Fields: []fields.Field{
			{ID: "gate.config.command.command", Key: "command", Kind: fields.String, Locked: &fields.Locked{Value: "/usr/bin/diff", Reason: "fixed"}},
		}},
		{ID: "gate.config.json_predicate", Kind: fields.Group, Requires: kindIs("json_predicate"), Fields: []fields.Field{
			{ID: "gate.config.json_predicate.input", Key: "input", Kind: fields.InputName, InputTypes: []string{"json"}, InputRequired: true},
			{ID: "gate.config.json_predicate.field", Key: "field", Kind: fields.List, Item: &fields.Field{ID: "gate.config.json_predicate.field.item", Kind: fields.String}},
		}},
	}
	return []NodeType{
		{Type: "shell", Config: []fields.Field{
			{ID: "shell.config.command", Key: "command", Kind: fields.String, Required: true},
			{ID: "shell.config.args", Key: "args", Kind: fields.List, Item: &fields.Field{ID: "shell.config.args.item", Kind: fields.String}},
			{ID: "shell.config.env", Key: "env", Kind: fields.Map},
			{ID: "shell.config.stdout_json", Key: "stdout_json", Kind: fields.Bool, Default: false},
		}, Outputs: []fields.Port{{Name: "stdout", Type: "string"}, {Name: "json", Type: "json", Requires: []fields.Rule{{Key: "stdout_json", Equals: "true"}}}},
			Loop: &LoopType{Fields: []fields.Field{{ID: "loop.max_attempts", Key: "max_attempts", Kind: fields.Int, Default: 3, Min: fields.IntPtr(1), Max: fields.IntPtr(10)}}, Gate: gateFields, GateKinds: []string{"command", "json_predicate"}}},
		{Type: "gate", Config: gateFields, Outputs: []fields.Port{{Name: "verdict", Type: "string"}}},
		{Type: "human", Config: []fields.Field{{ID: "human.config.prompt", Key: "prompt", Kind: fields.Text, MaxBytes: 1024}},
			Outputs: []fields.Port{{Name: "decision", Type: "string", Required: true}}, OutputsLocked: true},
	}
}

const inspectGraph = `schema_version: 0
id: inspect
inputs: {}
nodes:
  a:
    type: shell
    config:
      command: "true"
      args: [x, {nested: map}]  # a map where a string is expected
      future_key:
        deep: {list: [1, 2]}
      stdout_json: true
    outputs:
      json: {type: json}
    loop:
      max_attempts: 2
      gate:
        inputs:
          doc: {type: json, required: true}
        config: {check_id: c, kind: json_predicate, input: doc, field: [ok], left_input: stale}
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.doc}
  b:
    type: shell
    inputs:
      i: {type: json}
    config: {command: "true"}
  c:
    type: plugin
    config: {anything: [1, 2]}
edges:
  - {from: a.outputs.json, to: b.inputs.i}
outputs: {}
`

type inspectDoc struct {
	NodeTypes map[string]nodeTypeDTO `json:"node_types"`
	NodeInfo  map[string]nodeInfoDTO `json:"node_info"`
	Hash      string                 `json:"hash"`
	Text      string                 `json:"text"`
}

func decodeDoc(t *testing.T, out map[string]any) inspectDoc {
	t.Helper()
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var d inspectDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOpenReturnsDescriptorsForTheTypesPresent(t *testing.T) {
	s, ts, dir := docServer(t)
	s.NodeTypes = testCatalogue()
	g := grantFor(t, ts, writeGraph(t, dir, "i.yaml", inspectGraph), "open")
	status, out := call(t, ts, "POST", "/graphs/open", map[string]any{"grant": g})
	if status != 200 {
		t.Fatalf("open: %d %v", status, out)
	}
	d := decodeDoc(t, out)
	if len(d.NodeTypes) != 1 || d.NodeTypes["shell"].Loop == nil {
		t.Fatalf("node types present: %v", d.NodeTypes)
	}
	_, out = call(t, ts, "POST", "/graphs/open", map[string]any{"grant": g, "all_node_types": true})
	if d = decodeDoc(t, out); len(d.NodeTypes) != 3 {
		t.Errorf("all node types: %v", d.NodeTypes)
	}
	if !d.NodeTypes["human"].OutputsLocked || d.NodeTypes["human"].Loop != nil || d.NodeTypes["gate"].Loop != nil {
		t.Errorf("human/gate: %+v %+v", d.NodeTypes["human"], d.NodeTypes["gate"])
	}
}

func TestDescriptorWireShape(t *testing.T) {
	s, ts, dir := docServer(t)
	s.NodeTypes = testCatalogue()
	g := grantFor(t, ts, writeGraph(t, dir, "i.yaml", inspectGraph), "open")
	_, out := call(t, ts, "POST", "/graphs/open", map[string]any{"grant": g, "all_node_types": true})
	types := out["node_types"].(map[string]any)
	shell := types["shell"].(map[string]any)
	args := shell["config"].([]any)[1].(map[string]any)
	if args["id"] != "shell.config.args" || args["key"] != "args" || args["kind"] != "list" || args["item"].(map[string]any)["kind"] != "string" {
		t.Errorf("list descriptor: %v", args)
	}
	if _, present := args["required"]; present {
		t.Errorf("false members are omitted: %v", args)
	}
	jsonPort := shell["outputs"].([]any)[1].(map[string]any)
	if !reflect.DeepEqual(jsonPort, map[string]any{"name": "json", "type": "json", "requires": []any{map[string]any{"key": "stdout_json", "equals": "true"}}}) {
		t.Errorf("output port: %v", jsonPort)
	}
	loop := shell["loop"].(map[string]any)
	maxAttempts := loop["fields"].([]any)[0].(map[string]any)
	if maxAttempts["min"] != 1.0 || maxAttempts["max"] != 10.0 || maxAttempts["default"] != 3.0 {
		t.Errorf("loop field: %v", maxAttempts)
	}
	if !reflect.DeepEqual(loop["gate_kinds"], []any{"command", "json_predicate"}) {
		t.Errorf("gate kinds: %v", loop["gate_kinds"])
	}
	group := loop["gate"].([]any)[2].(map[string]any)
	locked := group["fields"].([]any)[0].(map[string]any)["locked"]
	if group["requires"] == nil || !reflect.DeepEqual(locked, map[string]any{"value": "/usr/bin/diff", "reason": "fixed"}) {
		t.Errorf("gate group: %v", group)
	}
	if shell["outputs_locked"] != false || types["human"].(map[string]any)["outputs_locked"] != true {
		t.Errorf("outputs_locked must always be present")
	}
}

func TestNodeInfoListsAdvancedKeysAndIndependence(t *testing.T) {
	s, ts, dir := docServer(t)
	s.NodeTypes = testCatalogue()
	g := grantFor(t, ts, writeGraph(t, dir, "i.yaml", inspectGraph), "open")
	_, out := call(t, ts, "POST", "/graphs/open", map[string]any{"grant": g})
	d := decodeDoc(t, out)
	a := d.NodeInfo["a"]
	wantA := []advancedDTO{
		{Key: "args", YAML: "[x, {nested: map}] # a map where a string is expected", Reason: "shape"},
		{Key: "future_key", YAML: "deep: {list: [1, 2]}", Reason: "unknown"},
	}
	if !a.KnownType || !reflect.DeepEqual(a.Advanced, wantA) {
		t.Errorf("a advanced: %+v", a.Advanced)
	}
	if want := []advancedDTO{{Key: "left_input", YAML: "stale", Reason: "unknown"}}; !reflect.DeepEqual(a.LoopAdvanced, want) {
		t.Errorf("a loop advanced (a key of another Gate kind): %+v", a.LoopAdvanced)
	}
	if b := d.NodeInfo["b"]; len(b.Advanced) != 0 || b.LoopAdvanced != nil {
		t.Errorf("b has nothing advanced: %+v", b)
	}
	c := d.NodeInfo["c"]
	if c.KnownType || !reflect.DeepEqual(c.Advanced, []advancedDTO{{Key: "anything", YAML: "[1, 2]", Reason: "unknown"}}) {
		t.Errorf("an unknown type shows every key as advanced: %+v", c)
	}
	want := map[string][]string{"a": {"c"}, "b": {"c"}, "c": {"a", "b"}}
	for id, list := range want {
		if !reflect.DeepEqual(d.NodeInfo[id].IndependentOf, list) {
			t.Errorf("%s independent of %v, want %v", id, d.NodeInfo[id].IndependentOf, list)
		}
	}
}

func TestAdvancedEntriesSurviveAnEdit(t *testing.T) {
	s, ts, dir := docServer(t)
	s.NodeTypes = testCatalogue()
	g := grantFor(t, ts, writeGraph(t, dir, "i.yaml", inspectGraph), "open")
	_, out := call(t, ts, "POST", "/graphs/open", map[string]any{"grant": g})
	before := decodeDoc(t, out)
	ops := json.RawMessage(`[{"op":"set_config","node":"a","path":["command"],"value":"/bin/echo"},{"op":"add_port","node":"b","direction":"output","port":"stdout","type":"string"}]`)
	status, out := call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": before.Hash, "ops_version": 1, "ops": ops, "all_node_types": true})
	if status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}
	after := decodeDoc(t, out)
	for id, info := range before.NodeInfo {
		if !reflect.DeepEqual(info.Advanced, after.NodeInfo[id].Advanced) || !reflect.DeepEqual(info.LoopAdvanced, after.NodeInfo[id].LoopAdvanced) {
			t.Errorf("%s advanced changed: %+v -> %+v", id, info, after.NodeInfo[id])
		}
	}
	if len(after.NodeTypes) != 3 {
		t.Errorf("edit honours all_node_types: %d", len(after.NodeTypes))
	}
}

// Typed text is validated, not edited: the answer must keep the inspector in step.
func TestValidateTextCarriesInspectorData(t *testing.T) {
	s, ts, _ := docServer(t)
	s.NodeTypes = testCatalogue()
	_, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": inspectGraph})
	d := decodeDoc(t, out)
	if len(d.NodeTypes) != 1 || len(d.NodeInfo["a"].Advanced) != 2 || !reflect.DeepEqual(d.NodeInfo["c"].IndependentOf, []string{"a", "b"}) {
		t.Errorf("validate(text): %v", out)
	}
	_, out = call(t, ts, "POST", "/graphs/validate", map[string]any{"text": "schema_version: 0\nid: [\n"})
	if _, present := out["node_info"]; present || out["node_types"] == nil {
		t.Errorf("validate(unparseable): %v", out)
	}
}

func TestInspectorDataWithoutACatalogueOrAParse(t *testing.T) {
	_, ts, dir := docServer(t)
	g := grantFor(t, ts, writeGraph(t, dir, "pair.yaml", pairGraph), "open")
	_, out := call(t, ts, "POST", "/graphs/open", map[string]any{"grant": g, "all_node_types": true})
	d := decodeDoc(t, out)
	if d.NodeTypes == nil || len(d.NodeTypes) != 0 {
		t.Errorf("no catalogue: node_types must be an empty object, got %v", out["node_types"])
	}
	a := d.NodeInfo["a"]
	if a.KnownType || len(a.Advanced) != 1 || a.Advanced[0].Key != "command" || a.Advanced[0].YAML != `"true"` {
		t.Errorf("without descriptors every key is advanced: %+v", a)
	}
	if !reflect.DeepEqual(a.IndependentOf, []string{"b"}) {
		t.Errorf("independence: %v", a.IndependentOf)
	}
	bad := grantFor(t, ts, writeGraph(t, dir, "bad.yaml", "schema_version: 0\nid: [\n"), "open")
	_, out = call(t, ts, "POST", "/graphs/open", map[string]any{"grant": bad})
	if _, present := out["node_info"]; present || out["node_types"] == nil {
		t.Errorf("unparseable: node_info absent, node_types present: %v", out)
	}
	// stat keeps its own strict body: the open-only member is refused there.
	if status, out := call(t, ts, "POST", "/graphs/stat", map[string]any{"grant": g, "all_node_types": true}); status != 400 {
		t.Errorf("stat with all_node_types: %d %v", status, out)
	}
}
