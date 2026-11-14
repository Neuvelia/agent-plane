package edit

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"agentplane/internal/graph"

	"go.yaml.in/yaml/v3"
)

// A save with no semantic change must write the identical bytes.
func TestNoOpIsByteIdentical(t *testing.T) {
	for name, src := range repoGraphs(t) {
		res, err := Apply(src, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(res.Text, src) {
			r, a := lineDiff(string(src), string(res.Text))
			t.Errorf("%s: no-op changed the file\nremoved %q\nadded %q", name, r, a)
		}
	}
	// comments.yaml is not here on purpose: it shows the accepted normalisation
	// of the space before a trailing comment (see TestTrailingCommentSpacing).
	for _, name := range []string{"crlf.yaml", "bom.yaml"} {
		src := fixtures(t)[name]
		res, err := Apply(src, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(res.Text, src) {
			r, a := lineDiff(string(src), string(res.Text))
			t.Errorf("%s: no-op changed the file\nremoved %q\nadded %q", name, r, a)
		}
	}
}

func TestEnvelopeKeptAfterAnEdit(t *testing.T) {
	crlf := fixtures(t)["crlf.yaml"]
	res, err := Apply(crlf, []Op{{Op: OpSetLayout, Nodes: map[string]Position{"left": {X: 10, Y: 20}}}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ReplaceAll(res.Text, []byte("\r\n"), nil), []byte("\n")) {
		t.Errorf("CRLF file got a bare LF line ending")
	}
	bom := fixtures(t)["bom.yaml"]
	res, err = Apply(bom, []Op{{Op: OpSetName, Name: ptr("renamed")}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(res.Text, utf8BOM) || bytes.HasPrefix(res.Text[3:], utf8BOM) {
		t.Errorf("byte order mark not preserved exactly once")
	}
}

// Each operation changes exactly the expected lines of demo-vertical.yaml.
func TestGoldenDiffs(t *testing.T) {
	src := string(repoGraphs(t)["demo-vertical.yaml"])
	cases := []struct {
		name           string
		op             Op
		removed, added []string
	}{
		{"set_layout creates the block", Op{Op: OpSetLayout, Nodes: map[string]Position{"fast": {X: 120, Y: 80}}}, nil,
			[]string{"metadata:", "  layout:", "    nodes:", "      fast: {x: 120, y: 80}"}},
		{"set_name inserts after id", Op{Op: OpSetName, Name: ptr("Demo")}, nil, []string{"name: Demo"}},
		{"add_node appends a starter", Op{Op: OpAddNode, ID: "extra", Type: "shell"}, nil,
			[]string{"  extra:", "    type: shell", "    config:", `      command: ""`}},
	}
	for _, c := range cases {
		out := mustApply(t, src, c.op)
		removed, added := lineDiff(src, out)
		if fmt.Sprint(removed) != fmt.Sprint(c.removed) || fmt.Sprint(added) != fmt.Sprint(c.added) {
			t.Errorf("%s:\nremoved %q (want %q)\nadded %q (want %q)", c.name, removed, c.removed, added, c.added)
		}
	}
}

func TestRemoveNodeDiffAndCleanup(t *testing.T) {
	src := string(repoGraphs(t)["demo-vertical.yaml"])
	out := mustApply(t, src, Op{Op: OpRemoveNode, ID: "apply"})
	removed, added := lineDiff(src, out)
	if len(added) != 0 {
		t.Errorf("remove added lines: %q", added)
	}
	text := strings.Join(removed, "\n")
	for _, want := range []string{"  apply:", "approve.outputs.decision"} {
		if !strings.Contains(text, want) {
			t.Errorf("removal did not remove %q:\n%s", want, text)
		}
	}
	if def := parse(t, out); len(def.Nodes) != 3 || len(def.Edges) != 2 {
		t.Errorf("after removal: %d nodes, %d edges", len(def.Nodes), len(def.Edges))
	}
}

func TestRemoveNodeTakesEverythingThatReferencesIt(t *testing.T) {
	src := `schema_version: 0
id: refs
metadata:
  layout:
    nodes:
      a: {x: 1, y: 2}
units: [a]
inputs:
  message: {type: string}
nodes:
  a:
    type: shell
    config:
      command: "true"
    inputs:
      text: {type: string}
    outputs:
      stdout: {type: string}
  b:
    type: shell
    config:
      command: "true"
    inputs:
      text: {type: string}
edges:
  - {from: a.outputs.stdout, to: b.inputs.text}
graph_input_bindings:
  - {from: inputs.message, to: a.inputs.text}
outputs:
  result: {from: a.outputs.stdout}
`
	out := mustApply(t, src, Op{Op: OpRemoveNode, ID: "a"})
	def := parse(t, out)
	if _, ok := def.Nodes["a"]; ok || len(def.Edges) != 0 || len(def.GraphInputBindings) != 0 || len(def.Outputs) != 0 || len(def.Units) != 0 || def.Metadata != nil {
		t.Errorf("references survived removal:\n%s", out)
	}
	if strings.Contains(out, "units") || strings.Contains(out, "metadata") {
		t.Errorf("empty units or metadata left behind:\n%s", out)
	}
}

func TestRenameNodeUpdatesEveryReference(t *testing.T) {
	src := `schema_version: 0
id: refs
metadata:
  layout:
    nodes:
      a: {x: 1, y: 2}
units: [a]
inputs:
  message: {type: string}
nodes:

  # the producer
  a:
    type: shell
    config:
      command: "true"
    inputs:
      text: {type: string}
    outputs:
      stdout: {type: string}

  b:
    type: shell
    config:
      command: "true"
    inputs:
      text: {type: string}
edges:
  - {from: a.outputs.stdout, to: b.inputs.text}
graph_input_bindings:
  - {from: inputs.message, to: a.inputs.text}
outputs:
  result: {from: a.outputs.stdout}
`
	out := mustApply(t, src, Op{Op: OpRenameNode, ID: "a", NewID: "producer"})
	for _, stale := range []string{" a:", "a.outputs", "a.inputs", "[a]"} {
		if strings.Contains(out, stale) {
			t.Errorf("stale reference %q after rename:\n%s", stale, out)
		}
	}
	removed, added := lineDiff(src, out)
	if len(removed) != len(added) {
		t.Errorf("rename should only change lines: removed %q added %q", removed, added)
	}
	if !strings.Contains(out, "\n\n  # the producer\n  producer:") {
		t.Errorf("rename lost the blank line or the comment above the node:\n%s", out)
	}
	if v1, v2 := version(t, src), version(t, out); v1 == v2 {
		t.Errorf("renaming a node must change the semantic version")
	}
}

func TestEdgeOperations(t *testing.T) {
	out := mustApply(t, twoNodes, Op{Op: OpAddEdge, From: "a.outputs.stdout", To: "b.inputs.text"})
	if def := parse(t, out); len(def.Edges) != 1 {
		t.Fatalf("edge not added:\n%s", out)
	}
	removed, added := lineDiff(twoNodes, out)
	if len(removed) != 1 || removed[0] != "edges: []" || fmt.Sprint(added) != "[edges:   - from: a.outputs.stdout     to: b.inputs.text]" {
		t.Errorf("an empty edges list becomes a block list: removed %q added %q", removed, added)
	}
	back := mustApply(t, out, Op{Op: OpRemoveEdge, From: "a.outputs.stdout", To: "b.inputs.text"})
	if !strings.Contains(back, "edges: []") {
		t.Errorf("removing the last edge should leave an empty list:\n%s", back)
	}
}

func TestTrailingCommentSpacing(t *testing.T) {
	src := string(fixtures(t)["comments.yaml"])
	out := mustApply(t, src, nil...)
	r, a := lineDiff(src, out)
	if len(r) != len(a) || len(r) == 0 {
		t.Fatalf("expected only the spacing before trailing comments to change: removed %q added %q", r, a)
	}
	for i := range r {
		if strings.Join(strings.Fields(r[i]), " ") != strings.Join(strings.Fields(a[i]), " ") {
			t.Errorf("more than spacing changed: %q -> %q", r[i], a[i])
		}
	}
}

func TestAddEdgeMatchesExistingStyle(t *testing.T) {
	src := strings.Replace(twoNodes, "edges: []", "edges:\n  - {from: a.outputs.stdout, to: b.inputs.text}", 1)
	out := mustApply(t, src, Op{Op: OpAddEdge, From: "a.outputs.stdout", To: "b.inputs.other"})
	if !strings.Contains(out, "  - {from: a.outputs.stdout, to: b.inputs.other}") {
		t.Errorf("new edge should be a flow item like the first:\n%s", out)
	}
	gone := mustApply(t, out, Op{Op: OpRemoveEdge, From: "a.outputs.stdout", To: "b.inputs.other"})
	if gone != src {
		t.Errorf("add then remove did not restore the file:\n%s", gone)
	}
}

func TestEdgeRefusals(t *testing.T) {
	cases := []struct {
		name, from, to, code string
	}{
		{"type mismatch", "a.outputs.count", "b.inputs.text", "E_BINDING_TYPE_MISMATCH"},
		{"unknown output", "a.outputs.nope", "b.inputs.text", "E_EDGE_SOURCE_OUTPUT_NOT_FOUND"},
		{"unknown input", "a.outputs.stdout", "b.inputs.nope", "E_EDGE_TARGET_INPUT_NOT_FOUND"},
		{"unknown node", "zzz.outputs.stdout", "b.inputs.text", "E_EDGE_SOURCE_NODE_NOT_FOUND"},
		{"self edge", "a.outputs.stdout", "a.inputs.text", ""},
	}
	for _, c := range cases {
		q := queryOf(t, twoNodes, c.from, c.to)
		if q.OK {
			t.Errorf("%s: can_connect said ok", c.name)
			continue
		}
		if c.code != "" && q.Code != c.code {
			t.Errorf("%s: code %s, want %s", c.name, q.Code, c.code)
		}
		if e := applyErr(t, twoNodes, Op{Op: OpAddEdge, From: c.from, To: c.to}); e.Code != CodeCannotConnect || e.OpIndex != 0 {
			t.Errorf("%s: add_edge error %+v", c.name, e)
		}
	}
	// A second provider for the same input.
	src := mustApply(t, twoNodes, Op{Op: OpAddEdge, From: "a.outputs.stdout", To: "b.inputs.text"})
	if q := queryOf(t, src, "a.outputs.stdout", "b.inputs.text"); q.OK || q.Code != "E_INPUT_MULTIPLE_PROVIDERS" {
		t.Errorf("duplicate provider: %+v", q)
	}
	// A cycle.
	cyc := `schema_version: 0
id: cyc
inputs: {}
nodes:
  a:
    type: shell
    config: {command: "true"}
    inputs:
      x: {type: string}
    outputs:
      y: {type: string}
  b:
    type: shell
    config: {command: "true"}
    inputs:
      y: {type: string}
    outputs:
      x: {type: string}
edges:
  - {from: a.outputs.y, to: b.inputs.y}
outputs: {}
`
	if q := queryOf(t, cyc, "b.outputs.x", "a.inputs.x"); q.OK || q.Code != "E_GRAPH_CYCLE" {
		t.Errorf("cycle: %+v", q)
	}
	if e := applyErr(t, twoNodes, Op{Op: OpRemoveEdge, From: "a.outputs.stdout", To: "b.inputs.text"}); e.Code != CodeEdgeNotFound {
		t.Errorf("removing a missing edge: %+v", e)
	}
}

func queryOf(t *testing.T, src, from, to string) *QueryResult {
	t.Helper()
	res, err := Apply([]byte(src), []Op{{Op: OpCanConnect, From: from, To: to}})
	if err != nil {
		t.Fatalf("can_connect: %v", err)
	}
	if !bytes.Equal(res.Text, []byte(src)) {
		t.Errorf("a query changed the text")
	}
	return res.Results[0].Query
}

// can_connect has no rules of its own: whenever it says ok, the edge produces no
// new error; whenever it says no, the code is the validator's.
func TestCanConnectAgreesWithTheValidator(t *testing.T) {
	for name, src := range repoGraphs(t) {
		def := parse(t, string(src))
		var outs, ins []string
		for id, n := range def.Nodes {
			for p := range n.Outputs {
				outs = append(outs, id+".outputs."+p)
			}
			for p := range n.Inputs {
				ins = append(ins, id+".inputs."+p)
			}
		}
		sort.Strings(outs)
		sort.Strings(ins)
		before := graph.Validate(def)
		for _, o := range outs {
			for _, i := range ins {
				q := queryOf(t, string(src), o, i)
				after := *def
				after.Edges = append(append([]graph.EdgeDefinition(nil), def.Edges...), graph.EdgeDefinition{From: graph.NodeOutputReference(o), To: graph.NodeInputReference(i)})
				added := newErrors(def, &after)
				if q.OK != (len(added) == 0) {
					t.Errorf("%s: %s -> %s: can_connect ok=%v but validator added %d errors (before had %d diagnostics)", name, o, i, q.OK, len(added), len(before.Diagnostics))
				}
				if !q.OK && q.Code != added[0].Code {
					t.Errorf("%s: %s -> %s: code %s, validator %s", name, o, i, q.Code, added[0].Code)
				}
			}
		}
	}
}

func TestAddNodeStarters(t *testing.T) {
	for _, typ := range []string{"shell", "agent", "gate", "human"} {
		out := mustApply(t, twoNodes, Op{Op: OpAddNode, ID: "n1", Type: typ, Layout: &Position{X: 40.4, Y: 59.6}})
		def := parse(t, out)
		if def.Nodes["n1"].Type != typ {
			t.Errorf("%s: type %q", typ, def.Nodes["n1"].Type)
		}
		layout, _ := asMap(def.Metadata["layout"])
		nodes, _ := asMap(layout["nodes"])
		pos, _ := asMap(nodes["n1"])
		if pos["x"] != 40 || pos["y"] != 60 {
			t.Errorf("%s: layout %v, want rounded 40,60", typ, pos)
		}
	}
	out := mustApply(t, twoNodes, Op{Op: OpAddNode, ID: "ask", Type: "human"})
	if o := parse(t, out).Nodes["ask"].Outputs["decision"]; o.Type != graph.ValueTypeString || !o.Required {
		t.Errorf("human starter must declare the decision output: %+v", o)
	}
	if diags := graph.Validate(parse(t, out)).Diagnostics; hasCode(diags, "E_HUMAN_INVALID") {
		t.Errorf("human starter is not valid per the validator: %+v", diags)
	}
}

func hasCode(diags []graph.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestOperationErrors(t *testing.T) {
	cases := []struct {
		name string
		op   Op
		code string
	}{
		{"bad id", Op{Op: OpAddNode, ID: "Bad Id", Type: "shell"}, CodeNodeID},
		{"id with dot", Op{Op: OpAddNode, ID: "a.b", Type: "shell"}, CodeNodeID},
		{"unknown type", Op{Op: OpAddNode, ID: "n", Type: "robot"}, CodeNodeType},
		{"duplicate id", Op{Op: OpAddNode, ID: "a", Type: "shell"}, CodeNodeExists},
		{"remove missing", Op{Op: OpRemoveNode, ID: "zzz"}, CodeNodeNotFound},
		{"rename missing", Op{Op: OpRenameNode, ID: "zzz", NewID: "y"}, CodeNodeNotFound},
		{"rename to existing", Op{Op: OpRenameNode, ID: "a", NewID: "b"}, CodeNodeExists},
		{"rename to invalid", Op{Op: OpRenameNode, ID: "a", NewID: "9x"}, CodeNodeID},
		{"layout for missing node", Op{Op: OpSetLayout, Nodes: map[string]Position{"zzz": {}}}, CodeNodeNotFound},
		{"layout out of range", Op{Op: OpSetLayout, Nodes: map[string]Position{"a": {X: 1e9}}}, CodeLayout},
	}
	for _, c := range cases {
		e := applyErr(t, twoNodes, c.op)
		if e.Code != c.code || e.OpIndex != 0 {
			t.Errorf("%s: %+v, want %s at op 0", c.name, e, c.code)
		}
	}
	// A failure anywhere cancels the whole batch, and says which operation failed.
	e := applyErr(t, twoNodes, Op{Op: OpAddNode, ID: "ok", Type: "shell"}, Op{Op: OpRemoveNode, ID: "zzz"})
	if e.OpIndex != 1 || e.Code != CodeNodeNotFound {
		t.Errorf("batch error %+v", e)
	}
}

func TestMalformedLayoutIsNeverOverwritten(t *testing.T) {
	src := strings.Replace(twoNodes, "inputs: {}", "metadata:\n  layout: sideways\ninputs: {}", 1)
	e := applyErr(t, src, Op{Op: OpSetLayout, Nodes: map[string]Position{"a": {X: 1, Y: 2}}})
	if e.Code != CodeLayout {
		t.Errorf("got %+v", e)
	}
}

func TestPresentationOperations(t *testing.T) {
	src := string(repoGraphs(t)["demo-vertical.yaml"])
	base := version(t, src)
	steps := []Op{
		{Op: OpSetName, Name: ptr("A name")},
		{Op: OpSetLayout, Nodes: map[string]Position{"fast": {X: 5, Y: 6}, "flaky": {X: 7.4, Y: 8.6}}},
		{Op: OpSetLayout, Nodes: map[string]Position{"fast": {X: 50, Y: 60}}},
		{Op: OpSetName, Name: ptr("123")},
		{Op: OpSetName, Name: ptr("")},
		{Op: OpSetLayout, Clear: true},
	}
	out := src
	for i, op := range steps {
		out = mustApply(t, out, op)
		if v := version(t, out); v != base {
			t.Errorf("step %d (%s) changed the semantic version", i, op.Op)
		}
	}
	if out != src {
		r, a := lineDiff(src, out)
		t.Errorf("setting and clearing presentation fields did not restore the file: removed %q added %q", r, a)
	}
	if def := parse(t, mustApply(t, src, Op{Op: OpSetName, Name: ptr("123")})); def.Name != "123" {
		t.Errorf("a numeric-looking name must stay a string, got %q", def.Name)
	}
}

func TestPresentationOperationsAreIdempotent(t *testing.T) {
	src := string(repoGraphs(t)["hello-parallel.yaml"])
	once := mustApply(t, src, Op{Op: OpSetLayout, Nodes: map[string]Position{"left": {X: 10, Y: 20}}}, Op{Op: OpSetName, Name: ptr("N")})
	twice := mustApply(t, once, Op{Op: OpSetLayout, Nodes: map[string]Position{"left": {X: 10, Y: 20}}}, Op{Op: OpSetName, Name: ptr("N")})
	if once != twice {
		t.Errorf("repeating the same presentation operations changed the text")
	}
}

func TestLayoutEntriesAreAddedInIDOrderAndFollowRemoveAndRename(t *testing.T) {
	src := string(repoGraphs(t)["demo-vertical.yaml"])
	out := mustApply(t, src, Op{Op: OpSetLayout, Nodes: map[string]Position{"flaky": {X: 2, Y: 2}, "apply": {X: 1, Y: 1}, "fast": {X: 3, Y: 3}}})
	if i, j := strings.Index(out, "apply: {"), strings.Index(out, "fast: {"); i < 0 || j < 0 || i > j {
		t.Errorf("new layout entries should be in node id order:\n%s", out)
	}
	renamed := mustApply(t, out, Op{Op: OpRenameNode, ID: "fast", NewID: "quick"})
	if !strings.Contains(renamed, "quick: {x: 3, y: 3}") || strings.Contains(renamed, "fast: {") {
		t.Errorf("rename did not move the layout entry:\n%s", renamed)
	}
	removed := mustApply(t, renamed, Op{Op: OpRemoveNode, ID: "quick"})
	if strings.Contains(removed, "quick:") && strings.Contains(removed, "{x: 3") {
		t.Errorf("remove left a stale layout entry:\n%s", removed)
	}
}

func TestReadOnlyFiles(t *testing.T) {
	hostile := fixtures(t)["hostile.yaml"]
	got := Inspect(hostile)
	if fmt.Sprint(got) != "[alias anchor merge_key]" {
		t.Errorf("hostile reasons %v", got)
	}
	e := applyErr(t, string(hostile), Op{Op: OpSetName, Name: ptr("x")})
	if e.Code != CodeReadOnly || fmt.Sprint(e.Reasons) != "[alias anchor merge_key]" {
		t.Errorf("hostile edit: %+v", e)
	}
	if got := Inspect([]byte("schema_version: 0\nid: [")); fmt.Sprint(got) != "[parse_error]" {
		t.Errorf("broken yaml reasons %v", got)
	}
	if got := Inspect([]byte("schema_version: 0\nid: x\nsurprise: 1\n")); fmt.Sprint(got) != "[parse_error]" {
		t.Errorf("an unknown field is a parse error, got %v", got)
	}
	e = applyErr(t, "schema_version: 0\nid: x\nsurprise: 1\n", Op{Op: OpSetName, Name: ptr("x")})
	if e.Code != CodeParse {
		t.Errorf("unparseable file: %+v", e)
	}
	for name, src := range repoGraphs(t) {
		if got := Inspect(src); len(got) != 0 {
			t.Errorf("%s should be editable, got %v", name, got)
		}
	}
}

// The safety net refuses a result that does not mean what the operations asked.
func TestSafetyNetRefusesCorruption(t *testing.T) {
	defer func() { tamper = nil }()
	tamper = func(doc *yaml.Node) {
		nodes, _ := get(docMapping(doc), "nodes")
		cfg, _ := get(nodes.Content[1], "config")
		v, _ := get(cfg, "command")
		v.Value = "rm"
	}
	e := applyErr(t, twoNodes, Op{Op: OpSetName, Name: ptr("x")})
	if e.Code != CodeSafetyNet {
		t.Errorf("tampering was not caught: %+v", e)
	}
	// A presentation batch whose tree changes the version is refused as well.
	tamper = func(doc *yaml.Node) {
		top := docMapping(doc)
		id, _ := get(top, "id")
		id.Value = "other"
	}
	if e := applyErr(t, twoNodes, Op{Op: OpSetName, Name: ptr("x")}); e.Code != CodeSafetyNet {
		t.Errorf("id change was not caught: %+v", e)
	}
}

func TestDecodeIsStrict(t *testing.T) {
	ok, err := Decode([]byte(`[{"op":"add_node","id":"n","type":"shell"},{"op":"set_name","name":""},{"op":"set_layout","clear":true}]`))
	if err != nil || len(ok) != 3 {
		t.Fatalf("valid batch rejected: %v", err)
	}
	bad := map[string]string{
		"unknown field":      `[{"op":"add_node","id":"n","type":"shell","colour":"red"}]`,
		"unknown op":         `[{"op":"explode"}]`,
		"missing id":         `[{"op":"add_node","type":"shell"}]`,
		"missing from":       `[{"op":"add_edge","to":"b.inputs.x"}]`,
		"layout both":        `[{"op":"set_layout","clear":true,"nodes":{"a":{"x":1,"y":2}}}]`,
		"layout neither":     `[{"op":"set_layout"}]`,
		"set_name no name":   `[{"op":"set_name"}]`,
		"not an array":       `{"op":"add_node"}`,
		"trailing documents": `[] []`,
	}
	for name, body := range bad {
		if _, err := Decode([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	many := "[" + strings.Repeat(`{"op":"set_name","name":"x"},`, MaxOps) + `{"op":"set_name","name":"x"}]`
	if _, err := Decode([]byte(many)); err == nil || err.(*Error).Code != CodeTooManyOps {
		t.Errorf("too many operations not refused: %v", err)
	}
}

// After removing or renaming any node, nothing refers to what is gone.
func TestRemoveAndRenameLeaveNoDanglingReference(t *testing.T) {
	dangling := func(diags []graph.Diagnostic) []string {
		var out []string
		for _, d := range diags {
			if strings.Contains(d.Code, "NOT_FOUND") || strings.Contains(d.Code, "REFERENCE_INVALID") {
				out = append(out, d.Code)
			}
		}
		return out
	}
	for name, src := range repoGraphs(t) {
		def := parse(t, string(src))
		for id := range def.Nodes {
			out := mustApply(t, string(src), Op{Op: OpSetLayout, Nodes: map[string]Position{id: {X: 1, Y: 1}}}, Op{Op: OpRemoveNode, ID: id})
			if d := dangling(graph.Validate(parse(t, out)).Diagnostics); len(d) > 0 {
				t.Errorf("%s: removing %s left %v", name, id, d)
			}
			if strings.Contains(out, "layout") {
				t.Errorf("%s: removing %s left a layout entry:\n%s", name, id, out)
			}
			out = mustApply(t, string(src), Op{Op: OpRenameNode, ID: id, NewID: "renamed"})
			if d := dangling(graph.Validate(parse(t, out)).Diagnostics); len(d) > 0 {
				t.Errorf("%s: renaming %s left %v", name, id, d)
			}
		}
	}
}

// Comments stay attached to the same lines after edits that do not touch them.
func TestCommentsKeepTheirPlace(t *testing.T) {
	for _, name := range []string{"demo-vertical.yaml", "comments.yaml"} {
		src := repoGraphs(t)
		data, ok := src[name]
		if !ok {
			data = fixtures(t)[name]
		}
		before := commentAnchors(string(data))
		out := mustApply(t, string(data), Op{Op: OpSetName, Name: ptr("x")}, Op{Op: OpSetLayout, Nodes: map[string]Position{firstNodeID(t, data): {X: 1, Y: 2}}}, Op{Op: OpAddNode, ID: "zzz", Type: "shell"})
		after := commentAnchors(out)
		for comment, anchor := range before {
			if after[comment] != anchor {
				t.Errorf("%s: comment %q moved from %q to %q", name, comment, anchor, after[comment])
			}
		}
	}
}

func firstNodeID(t *testing.T, data []byte) string {
	t.Helper()
	ids := []string{}
	for id := range parse(t, string(data)).Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids[0]
}

// commentAnchors maps each comment to the code line it belongs to: the same line
// for a trailing comment, the next code line for a comment on its own line.
func commentAnchors(text string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next != "" && !strings.HasPrefix(next, "#") {
					out[trim] = next
					break
				}
			}
			continue
		}
		if at := strings.Index(line, " #"); at > 0 {
			out[strings.TrimSpace(line[at:])] = strings.TrimSpace(line[:at])
		}
	}
	return out
}

// A long random walk of operations never trips the safety net, and every step
// leaves a file the engine parses.
func TestRandomWalks(t *testing.T) {
	for name, start := range repoGraphs(t) {
		rng := rand.New(rand.NewSource(int64(len(name))))
		src := string(start)
		next := 0
		for step := 0; step < 150; step++ {
			def := parse(t, src)
			ids := make([]string, 0, len(def.Nodes))
			for id := range def.Nodes {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			var op Op
			switch rng.Intn(6) {
			case 0:
				next++
				op = Op{Op: OpAddNode, ID: fmt.Sprintf("n%d", next), Type: []string{"shell", "agent", "gate", "human"}[rng.Intn(4)]}
			case 1:
				if len(ids) == 0 {
					continue
				}
				op = Op{Op: OpRemoveNode, ID: ids[rng.Intn(len(ids))]}
			case 2:
				if len(ids) == 0 {
					continue
				}
				next++
				op = Op{Op: OpRenameNode, ID: ids[rng.Intn(len(ids))], NewID: fmt.Sprintf("r%d", next)}
			case 3:
				if len(ids) == 0 {
					continue
				}
				op = Op{Op: OpSetLayout, Nodes: map[string]Position{ids[rng.Intn(len(ids))]: {X: float64(rng.Intn(2000)), Y: float64(rng.Intn(2000))}}}
			case 4:
				op = Op{Op: OpSetName, Name: ptr(fmt.Sprintf("name %d", rng.Intn(5)))}
			default:
				if len(def.Edges) == 0 {
					continue
				}
				e := def.Edges[rng.Intn(len(def.Edges))]
				op = Op{Op: OpRemoveEdge, From: string(e.From), To: string(e.To)}
			}
			res, err := Apply([]byte(src), []Op{op})
			if err != nil {
				if e := err.(*Error); e.Code == CodeSafetyNet {
					t.Fatalf("%s step %d: safety net tripped on %+v: %v\n%s", name, step, op, e, src)
				}
				continue // a legitimate refusal
			}
			src = string(res.Text)
			if _, err := graph.Parse(res.Text); err != nil {
				t.Fatalf("%s step %d: result does not parse: %v", name, step, err)
			}
		}
	}
}

func TestLargeGraphIsFast(t *testing.T) {
	var b strings.Builder
	b.WriteString("schema_version: 0\nid: big\ninputs: {}\nnodes:\n")
	const n = 200
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  n%03d:\n    type: shell\n    config: {command: \"true\"}\n    inputs:\n      i1: {type: string}\n      i2: {type: string}\n    outputs:\n      o: {type: string}\n", i)
	}
	b.WriteString("edges:\n")
	for i := 2; i < n; i++ {
		fmt.Fprintf(&b, "  - {from: n%03d.outputs.o, to: n%03d.inputs.i1}\n  - {from: n%03d.outputs.o, to: n%03d.inputs.i2}\n", i-1, i, i-2, i)
	}
	b.WriteString("outputs: {}\n")
	src := b.String()
	if edges := len(parse(t, src).Edges); edges < 390 {
		t.Fatalf("generated only %d edges", edges)
	}
	start := time.Now()
	if _, err := Apply([]byte(src), []Op{{Op: OpAddNode, ID: "extra", Type: "shell", Layout: &Position{X: 1, Y: 2}}}); err != nil {
		t.Fatal(err)
	}
	limit := 250 * time.Millisecond
	if raceDetector {
		limit *= 10 // the race detector slows this down by an order of magnitude
	}
	if d := time.Since(start); d > limit {
		t.Errorf("applying one operation to a 200-node graph took %v (limit %v)", d, limit)
	}
}

// A folded block scalar is re-flowed (an accepted normalisation) but means the
// same text, and long arguments are never wrapped.
func TestLongLinesAndFoldedScalars(t *testing.T) {
	src := fixtures(t)["long.yaml"]
	out := mustApply(t, string(src), Op{Op: OpSetName, Name: ptr("changed")})
	before, after := parse(t, string(src)), parse(t, out)
	if fmt.Sprint(before.Metadata) != fmt.Sprint(after.Metadata) {
		t.Errorf("metadata changed: %v -> %v", before.Metadata, after.Metadata)
	}
	args := after.Nodes["a"].Config["args"].([]any)
	if len(args[0].(string)) != 190 {
		t.Errorf("a long argument was altered: %d bytes", len(args[0].(string)))
	}
	if strings.Count(out, "xxxxxxxxxx\n") > 1 {
		t.Errorf("a long argument was wrapped over several lines")
	}
}
