package edit

import (
	"agentplane/internal/adapter/yamlgraph"
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/graph"
)

// dataGraph has graph inputs, a graph input binding, an edge and a graph
// output, with comments the operations must leave alone.
const dataGraph = `schema_version: 0
id: data
# what the caller supplies
inputs:
  msg: {type: string, required: true} # the message
  n: {type: number}

nodes:
  a:
    type: shell
    config: {command: "true"}
    inputs:
      text: {type: string, required: true}
      count: {type: number}
    outputs:
      out: {type: string}
      num: {type: number}

  b:
    type: shell
    config: {command: "true"}
    inputs:
      text: {type: string, required: true}
      other: {type: string}
    outputs:
      out: {type: string}

edges:
  - {from: a.outputs.out, to: b.inputs.text}

graph_input_bindings:
  - from: inputs.msg
    to: a.inputs.text

outputs:
  result:
    from: b.outputs.out
`

func TestGraphInputOperations(t *testing.T) {
	cases := []struct {
		name    string
		op      Op
		code    string // expected refusal; empty means success
		removed []string
		added   []string
	}{
		{"add", Op{Op: OpAddGraphInput, ID: "fresh", Type: "number", Required: boolPtr(true)}, "", nil,
			[]string{"  fresh: {type: number, required: true}"}},
		{"add optional by default", Op{Op: OpAddGraphInput, ID: "fresh", Type: "json"}, "", nil,
			[]string{"  fresh: {type: json}"}},
		{"add existing", Op{Op: OpAddGraphInput, ID: "msg", Type: "string"}, CodeGraphInputExists, nil, nil},
		{"add invalid type", Op{Op: OpAddGraphInput, ID: "fresh", Type: "text"}, CodePortInvalid, nil, nil},
		{"add invalid name", Op{Op: OpAddGraphInput, ID: "Fresh", Type: "string"}, CodePortInvalid, nil, nil},
		{"retype an unbound input", Op{Op: OpUpdateGraphInput, ID: "n", Type: "string"}, "", []string{"  n: {type: number}"}, []string{"  n: {type: string}"}},
		{"retype a bound input", Op{Op: OpUpdateGraphInput, ID: "msg", Type: "number"}, CodeGraphInputTypeConflict, nil, nil},
		{"retype to an invalid type", Op{Op: OpUpdateGraphInput, ID: "n", Type: "text"}, CodePortInvalid, nil, nil},
		{"make required", Op{Op: OpUpdateGraphInput, ID: "n", Required: boolPtr(true)}, "", []string{"  n: {type: number}"}, []string{"  n: {type: number, required: true}"}},
		{"make optional", Op{Op: OpUpdateGraphInput, ID: "msg", Required: boolPtr(false)}, "",
			[]string{"  msg: {type: string, required: true} # the message"}, []string{"  msg: {type: string} # the message"}},
		{"rename updates bindings", Op{Op: OpUpdateGraphInput, ID: "msg", NewID: "message"}, "",
			[]string{"  msg: {type: string, required: true} # the message", "  - from: inputs.msg"},
			[]string{"  message: {type: string, required: true} # the message", "  - from: inputs.message"}},
		{"rename onto an existing input", Op{Op: OpUpdateGraphInput, ID: "msg", NewID: "n"}, CodeGraphInputExists, nil, nil},
		{"rename to an invalid name", Op{Op: OpUpdateGraphInput, ID: "msg", NewID: "Bad Name"}, CodePortInvalid, nil, nil},
		{"update a missing input", Op{Op: OpUpdateGraphInput, ID: "zzz", Required: boolPtr(true)}, CodeGraphInputNotFound, nil, nil},
		{"remove takes its bindings", Op{Op: OpRemoveGraphInput, ID: "msg"}, "",
			[]string{"  msg: {type: string, required: true} # the message", "graph_input_bindings:", "  - from: inputs.msg", "    to: a.inputs.text"}, []string{"graph_input_bindings: []"}},
		{"remove an unbound input", Op{Op: OpRemoveGraphInput, ID: "n"}, "", []string{"  n: {type: number}"}, nil},
		{"remove a missing input", Op{Op: OpRemoveGraphInput, ID: "zzz"}, CodeGraphInputNotFound, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.code != "" {
				if e := applyErr(t, dataGraph, c.op); e.Code != c.code {
					t.Fatalf("code %s (%s), want %s", e.Code, e.Message, c.code)
				}
				return
			}
			out := mustApply(t, dataGraph, c.op)
			removed, added := lineDiff(dataGraph, out)
			if !reflect.DeepEqual(removed, c.removed) || !reflect.DeepEqual(added, c.added) {
				t.Errorf("removed %q added %q\nwant removed %q added %q", removed, added, c.removed, c.added)
			}
		})
	}
}

func TestGraphInputNoOpsChangeNothing(t *testing.T) {
	for _, op := range []Op{
		{Op: OpUpdateGraphInput, ID: "msg", Type: "string", Required: boolPtr(true)},
		{Op: OpUpdateGraphInput, ID: "msg", NewID: "msg"},
		{Op: OpSetGraphOutput, ID: "result", From: "b.outputs.out"},
	} {
		if out := mustApply(t, dataGraph, op); out != dataGraph {
			r, a := lineDiff(dataGraph, out)
			t.Errorf("%+v changed the text: removed %q added %q", op, r, a)
		}
	}
}

func TestRemovingAnInputLeavesARequiredTargetUnboundForTheValidator(t *testing.T) {
	out := mustApply(t, dataGraph, Op{Op: OpRemoveGraphInput, ID: "msg"})
	for _, d := range graph.Validate(parse(t, out)).Diagnostics {
		if d.Code == "E_REQUIRED_INPUT_UNBOUND" {
			return
		}
	}
	t.Errorf("the unbound required input was not reported")
}

func TestGraphInputBindingOperations(t *testing.T) {
	cases := []struct {
		name    string
		ops     []Op
		code    string
		removed []string
		added   []string
	}{
		{"add", []Op{{Op: OpAddGraphInputBinding, From: "inputs.n", To: "a.inputs.count"}}, "", nil,
			[]string{"  - from: inputs.n", "    to: a.inputs.count"}},
		{"add refused: input already provided", []Op{{Op: OpAddGraphInputBinding, From: "inputs.msg", To: "b.inputs.text"}}, CodeCannotConnect, nil, nil},
		{"add refused: type mismatch", []Op{{Op: OpAddGraphInputBinding, From: "inputs.n", To: "b.inputs.other"}}, CodeCannotConnect, nil, nil},
		{"add refused: unknown graph input", []Op{{Op: OpAddGraphInputBinding, From: "inputs.zzz", To: "b.inputs.other"}}, CodeCannotConnect, nil, nil},
		{"add refused: unknown target", []Op{{Op: OpAddGraphInputBinding, From: "inputs.msg", To: "zzz.inputs.x"}}, CodeCannotConnect, nil, nil},
		{"add refused: not a graph input reference", []Op{{Op: OpAddGraphInputBinding, From: "a.outputs.out", To: "b.inputs.other"}}, CodeCannotConnect, nil, nil},
		{"remove", []Op{{Op: OpRemoveGraphInputBinding, From: "inputs.msg", To: "a.inputs.text"}}, "",
			[]string{"graph_input_bindings:", "  - from: inputs.msg", "    to: a.inputs.text"}, []string{"graph_input_bindings: []"}},
		{"remove a missing binding", []Op{{Op: OpRemoveGraphInputBinding, From: "inputs.n", To: "a.inputs.count"}}, CodeBindingNotFound, nil, nil},
		{"replace a provider is one atomic batch", []Op{
			{Op: OpRemoveGraphInputBinding, From: "inputs.msg", To: "a.inputs.text"},
			{Op: OpRemoveEdge, From: "a.outputs.out", To: "b.inputs.text"},
			{Op: OpAddGraphInputBinding, From: "inputs.msg", To: "b.inputs.text"},
		}, "", []string{"edges:", "  - {from: a.outputs.out, to: b.inputs.text}", "    to: a.inputs.text"}, []string{"edges: []", "    to: b.inputs.text"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.code != "" {
				if e := applyErr(t, dataGraph, c.ops...); e.Code != c.code {
					t.Fatalf("code %s (%s), want %s", e.Code, e.Message, c.code)
				}
				return
			}
			out := mustApply(t, dataGraph, c.ops...)
			removed, added := lineDiff(dataGraph, out)
			if !reflect.DeepEqual(removed, c.removed) || !reflect.DeepEqual(added, c.added) {
				t.Errorf("removed %q added %q\nwant removed %q added %q", removed, added, c.removed, c.added)
			}
		})
	}
}

func TestRefusedReplaceKeepsTheOldProvider(t *testing.T) {
	e := applyErr(t, dataGraph,
		Op{Op: OpRemoveGraphInputBinding, From: "inputs.msg", To: "a.inputs.text"},
		Op{Op: OpAddGraphInputBinding, From: "inputs.n", To: "a.inputs.text"}) // number into a string port
	if e.Code != CodeCannotConnect || e.OpIndex != 1 {
		t.Errorf("code %s at %d", e.Code, e.OpIndex)
	}
}

func TestGraphOutputOperations(t *testing.T) {
	cases := []struct {
		name    string
		op      Op
		code    string
		removed []string
		added   []string
	}{
		{"declare", Op{Op: OpSetGraphOutput, ID: "second", From: "a.outputs.num"}, "", nil, []string{"  second:", "    from: a.outputs.num"}},
		{"change source", Op{Op: OpSetGraphOutput, ID: "result", From: "a.outputs.out"}, "", []string{"    from: b.outputs.out"}, []string{"    from: a.outputs.out"}},
		{"missing node", Op{Op: OpSetGraphOutput, ID: "second", From: "zzz.outputs.out"}, CodeGraphOutputInvalid, nil, nil},
		{"missing port", Op{Op: OpSetGraphOutput, ID: "second", From: "a.outputs.zzz"}, CodeGraphOutputInvalid, nil, nil},
		{"not an output reference", Op{Op: OpSetGraphOutput, ID: "second", From: "a.inputs.text"}, CodeGraphOutputInvalid, nil, nil},
		{"invalid name", Op{Op: OpSetGraphOutput, ID: "Bad", From: "a.outputs.out"}, CodeGraphOutputInvalid, nil, nil},
		{"remove", Op{Op: OpRemoveGraphOutput, ID: "result"}, "", []string{"outputs:", "  result:", "    from: b.outputs.out"}, []string{"outputs: {}"}},
		{"remove a missing output", Op{Op: OpRemoveGraphOutput, ID: "zzz"}, CodeGraphOutputInvalid, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.code != "" {
				if e := applyErr(t, dataGraph, c.op); e.Code != c.code {
					t.Fatalf("code %s (%s), want %s", e.Code, e.Message, c.code)
				}
				return
			}
			out := mustApply(t, dataGraph, c.op)
			removed, added := lineDiff(dataGraph, out)
			if !reflect.DeepEqual(removed, c.removed) || !reflect.DeepEqual(added, c.added) {
				t.Errorf("removed %q added %q\nwant removed %q added %q", removed, added, c.removed, c.added)
			}
		})
	}
}

func TestEmptyingKeepsTheKeysTheSchemaRequires(t *testing.T) {
	out := mustApply(t, dataGraph,
		Op{Op: OpRemoveGraphInput, ID: "msg"}, Op{Op: OpRemoveGraphInput, ID: "n"},
		Op{Op: OpRemoveEdge, From: "a.outputs.out", To: "b.inputs.text"},
		Op{Op: OpRemoveGraphOutput, ID: "result"})
	def := parse(t, out)
	if def.Inputs == nil || def.Edges == nil || def.Outputs == nil {
		t.Fatalf("a required collection disappeared:\n%s", out)
	}
	for _, want := range []string{"inputs: {}", "edges: []", "outputs: {}"} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	out = mustApply(t, dataGraph, Op{Op: OpRemoveGraphInputBinding, From: "inputs.msg", To: "a.inputs.text"})
	if !strings.Contains(out, "graph_input_bindings: []\n") {
		t.Errorf("an emptied binding list is written as []:\n%s", out)
	}
	if _, err := yamlgraph.Parse([]byte(out)); err != nil {
		t.Error(err)
	}
}

func TestABindingsFreeFileGainsTheKeyAndKeepsItsOtherLines(t *testing.T) {
	out := mustApply(t, twoNodes,
		Op{Op: OpAddGraphInput, ID: "words", Type: "string", Required: boolPtr(true)},
		Op{Op: OpAddGraphInputBinding, From: "inputs.words", To: "b.inputs.text"})
	removed, added := lineDiff(twoNodes, out)
	if !reflect.DeepEqual(removed, []string{"inputs: {}"}) {
		t.Errorf("removed %q", removed)
	}
	want := []string{"inputs:", "  words: {type: string, required: true}", "graph_input_bindings:", "  - from: inputs.words", "    to: b.inputs.text"}
	if !reflect.DeepEqual(added, want) {
		t.Errorf("added %q, want %q", added, want)
	}
	if idx := strings.Index(out, "graph_input_bindings:"); idx < strings.Index(out, "edges:") || idx > strings.Index(out, "\noutputs:") {
		t.Errorf("graph_input_bindings is not between edges and outputs:\n%s", out)
	}
}

func TestRenameNodeStillRewritesGraphInputBindingTargets(t *testing.T) {
	out := mustApply(t, dataGraph, Op{Op: OpRenameNode, ID: "a", NewID: "first"})
	if !strings.Contains(out, "    to: first.inputs.text") {
		t.Errorf("binding target not renamed:\n%s", out)
	}
}

// corpus is every graph the engine ships or tests with, by name.
func corpus(t *testing.T) map[string][]byte {
	t.Helper()
	all := map[string][]byte{"dataGraph": []byte(dataGraph), "inspectorGraph": []byte(inspectorGraph)}
	for k, v := range repoGraphs(t) {
		all[k] = v
	}
	for k, v := range fixtures(t) {
		all["testdata/"+k] = v
	}
	return all
}

// editable is the corpus minus the files the canvas refuses to patch (anchors,
// aliases, merge keys: testdata/hostile.yaml), which edit_test.go covers.
func editable(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for k, v := range corpus(t) {
		if len(Inspect(v)) == 0 {
			out[k] = v
		}
	}
	if _, ok := out["testdata/hostile.yaml"]; ok || len(out) < 6 {
		t.Fatalf("unexpected editable corpus: %d graphs, hostile included=%v", len(out), ok)
	}
	return out
}

// An add followed by its inverse restores the semantic version, on every graph.
func TestGraphDataOperationsRoundTripOnTheCorpus(t *testing.T) {
	for name, src := range editable(t) {
		before := version(t, string(src))
		ops := []Op{
			{Op: OpAddGraphInput, ID: "zz-new", Type: "json", Required: boolPtr(true)},
			{Op: OpUpdateGraphInput, ID: "zz-new", NewID: "zz-renamed", Type: "string", Required: boolPtr(false)},
		}
		if from := firstOutput(src, t); from != "" {
			ops = append(ops, Op{Op: OpSetGraphOutput, ID: "zz-out", From: from}, Op{Op: OpRemoveGraphOutput, ID: "zz-out"})
		}
		ops = append(ops, Op{Op: OpRemoveGraphInput, ID: "zz-renamed"})
		out := mustApply(t, string(src), ops...)
		if got := version(t, out); got != before {
			t.Errorf("%s: version changed after add and remove", name)
		}
	}
}

func firstOutput(src []byte, t *testing.T) string {
	t.Helper()
	def := parse(t, string(src))
	ids := sortedKeys(def.Nodes)
	for _, id := range ids {
		for _, p := range sortedKeys(def.Nodes[id].Outputs) {
			return id + ".outputs." + p
		}
	}
	return ""
}

// Bind every graph input to every input the validator accepts, then remove it:
// the text round-trips and the binding list parses.
func TestGraphInputBindingsAddAndRemoveOnTheCorpus(t *testing.T) {
	for name, src := range editable(t) {
		def := parse(t, string(src))
		for _, in := range sortedKeys(def.Inputs) {
			for _, target := range inputRefs(def) {
				from := "inputs." + in
				q := queryOf(t, string(src), from, target)
				_, err := Apply(src, []Op{{Op: OpAddGraphInputBinding, From: from, To: target}})
				if q.OK != (err == nil) {
					t.Fatalf("%s: %s -> %s: can_connect ok=%v but add_graph_input_binding err=%v", name, from, target, q.OK, err)
				}
				if err != nil {
					if e := asEditError(t, err); e.Code != CodeCannotConnect {
						t.Fatalf("%s: %s -> %s: code %s", name, from, target, e.Code)
					}
					continue
				}
				out := mustApply(t, string(src), Op{Op: OpAddGraphInputBinding, From: from, To: target}, Op{Op: OpRemoveGraphInputBinding, From: from, To: target})
				if version(t, out) != version(t, string(src)) {
					t.Fatalf("%s: %s -> %s: add then remove changed the version", name, from, target)
				}
			}
		}
	}
}

func asEditError(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error is %T, want *Error: %v", err, err)
	}
	return e
}

func inputRefs(def *graph.Definition) []string {
	var refs []string
	for _, id := range sortedKeys(def.Nodes) {
		for _, p := range sortedKeys(def.Nodes[id].Inputs) {
			refs = append(refs, id+".inputs."+p)
		}
	}
	return refs
}

func outputRefs(def *graph.Definition) []string {
	var refs []string
	for _, id := range sortedKeys(def.Nodes) {
		for _, p := range sortedKeys(def.Nodes[id].Outputs) {
			refs = append(refs, id+".outputs."+p)
		}
	}
	return refs
}

func randomGraphDataOp(rng *rand.Rand, def *graph.Definition, step int) Op {
	ins, outs := inputRefs(def), outputRefs(def)
	inputs := sortedKeys(def.Inputs)
	pick := func(list []string, fallback string) string {
		if len(list) == 0 {
			return fallback
		}
		return list[rng.Intn(len(list))]
	}
	switch rng.Intn(9) {
	case 0:
		return Op{Op: OpAddGraphInput, ID: fmt.Sprintf("g%d", rng.Intn(4)), Type: []string{"string", "number", "json"}[rng.Intn(3)], Required: boolPtr(rng.Intn(2) == 0)}
	case 1:
		return Op{Op: OpUpdateGraphInput, ID: pick(inputs, "gone"), NewID: fmt.Sprintf("r%d", step), Required: boolPtr(rng.Intn(2) == 0)}
	case 2:
		return Op{Op: OpUpdateGraphInput, ID: pick(inputs, "gone"), Type: []string{"string", "number", "json"}[rng.Intn(3)]}
	case 3:
		return Op{Op: OpRemoveGraphInput, ID: pick(inputs, "gone")}
	case 4, 5:
		return Op{Op: OpAddGraphInputBinding, From: "inputs." + pick(inputs, "gone"), To: pick(ins, "x.inputs.y")}
	case 6:
		if len(def.GraphInputBindings) > 0 {
			b := def.GraphInputBindings[rng.Intn(len(def.GraphInputBindings))]
			return Op{Op: OpRemoveGraphInputBinding, From: string(b.From), To: string(b.To)}
		}
		return Op{Op: OpRemoveGraphInputBinding, From: "inputs.gone", To: "x.inputs.y"}
	case 7:
		return Op{Op: OpSetGraphOutput, ID: fmt.Sprintf("o%d", rng.Intn(3)), From: pick(outs, "x.outputs.y")}
	default:
		return Op{Op: OpRemoveGraphOutput, ID: fmt.Sprintf("o%d", rng.Intn(3))}
	}
}

// After any random sequence of the graph data operations the safety net never
// trips and no reference dangles; keys no operation targets survive.
func TestRandomGraphDataWalks(t *testing.T) {
	dangling := func(diags []graph.Diagnostic) []string {
		var out []string
		for _, d := range diags {
			if strings.Contains(d.Code, "NOT_FOUND") || strings.Contains(d.Code, "REFERENCE_INVALID") {
				out = append(out, d.Code+": "+d.Message)
			}
		}
		return out
	}
	all := editable(t)
	names := sortedKeys(all)
	wantValues, wantLines := unknownKeys(t, inspectorGraph)
	for seed := int64(1); seed <= 50; seed++ {
		name := names[int(seed)%len(names)]
		rng := rand.New(rand.NewSource(seed))
		src := string(all[name])
		baseline := dangling(graph.Validate(parse(t, src)).Diagnostics)
		applied := 0
		for step := 0; step < 120 && applied < 25; step++ {
			op := randomGraphDataOp(rng, parse(t, src), step)
			res, err := Apply([]byte(src), []Op{op})
			if err != nil {
				if e := asEditError(t, err); e.Code == CodeSafetyNet {
					t.Fatalf("seed %d (%s) step %d: safety net tripped on %+v: %v\n%s", seed, name, step, op, e, src)
				}
				continue
			}
			applied++
			src = string(res.Text)
			if d := dangling(graph.Validate(parse(t, src)).Diagnostics); len(d) > len(baseline) {
				t.Fatalf("seed %d (%s) step %d: %+v left %v\n%s", seed, name, step, op, d, src)
			}
		}
		if name == "inspectorGraph" {
			values, lines := unknownKeys(t, src)
			if !reflect.DeepEqual(values, wantValues) || !reflect.DeepEqual(lines, wantLines) {
				t.Fatalf("seed %d: unknown keys changed:\n%s", seed, src)
			}
		}
	}
}

func TestListConnectionsFromAnOutput(t *testing.T) {
	q := listOf(t, dataGraph, Op{Op: OpListConnections, From: "a.outputs.out"})
	want := []ConnectionOption{
		{Ref: "a.inputs.count", Type: "number", OK: false},
		{Ref: "a.inputs.text", Type: "string", Required: true, OK: false},
		{Ref: "b.inputs.other", Type: "string", OK: true},
		{Ref: "b.inputs.text", Type: "string", Required: true, OK: false},
	}
	if len(q.Options) != len(want) {
		t.Fatalf("options %+v", q.Options)
	}
	for i, w := range want {
		g := q.Options[i]
		if g.Ref != w.Ref || g.Type != w.Type || g.Required != w.Required || g.OK != w.OK {
			t.Errorf("option %d: %+v, want %+v", i, g, w)
		}
		if !g.OK && (g.Code == "" || g.Message == "") {
			t.Errorf("a refused option explains itself: %+v", g)
		}
		if g.OK && (g.Code != "" || g.Message != "") {
			t.Errorf("an ok option carries no reason: %+v", g)
		}
	}
	if q.Options[0].Code != "E_BINDING_TYPE_MISMATCH" || q.Options[3].Code != "E_INPUT_MULTIPLE_PROVIDERS" {
		t.Errorf("codes %s %s", q.Options[0].Code, q.Options[3].Code)
	}
}

func TestListConnectionsToAnInputPutsGraphInputsFirst(t *testing.T) {
	q := listOf(t, dataGraph, Op{Op: OpListConnections, To: "b.inputs.other"})
	var refs []string
	for _, o := range q.Options {
		refs = append(refs, o.Ref)
	}
	want := []string{"inputs.msg", "inputs.n", "a.outputs.num", "a.outputs.out", "b.outputs.out"}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs %q, want %q", refs, want)
	}
	byRef := map[string]ConnectionOption{}
	for _, o := range q.Options {
		byRef[o.Ref] = o
	}
	for ref, ok := range map[string]bool{"inputs.msg": true, "inputs.n": false, "a.outputs.out": true, "a.outputs.num": false, "b.outputs.out": false} {
		if byRef[ref].OK != ok {
			t.Errorf("%s: ok=%v, want %v (%s)", ref, byRef[ref].OK, ok, byRef[ref].Message)
		}
	}
	if byRef["b.outputs.out"].Code != "E_GRAPH_CYCLE" {
		t.Errorf("same node: %+v", byRef["b.outputs.out"])
	}
}

func TestListConnectionsFromAGraphInput(t *testing.T) {
	q := listOf(t, dataGraph, Op{Op: OpListConnections, From: "inputs.n"})
	ok := map[string]bool{}
	for _, o := range q.Options {
		ok[o.Ref] = o.OK
	}
	if !ok["a.inputs.count"] || ok["a.inputs.text"] || ok["b.inputs.other"] {
		t.Errorf("options %+v", q.Options)
	}
}

func TestListConnectionsRefusals(t *testing.T) {
	for _, op := range []Op{
		{Op: OpListConnections}, {Op: OpListConnections, From: "a.outputs.out", To: "b.inputs.text"},
	} {
		if _, err := Decode(raw([]Op{op})); err == nil {
			t.Errorf("%+v decoded", op)
		}
	}
	for _, op := range []Op{
		{Op: OpListConnections, From: "a.inputs.text"}, {Op: OpListConnections, To: "a.outputs.out"},
		{Op: OpListConnections, From: "zzz.outputs.out"}, {Op: OpListConnections, From: "a.outputs.zzz"},
		{Op: OpListConnections, From: "inputs.zzz"}, {Op: OpListConnections, To: "b.inputs.zzz"},
	} {
		res, err := Apply([]byte(dataGraph), []Op{op})
		if err != nil {
			t.Fatalf("%+v: %v", op, err)
		}
		if q := res.Results[0].Query; q == nil || q.OK || q.Code == "" || len(q.Options) != 0 {
			t.Errorf("%+v: %+v", op, q)
		}
	}
}

func TestListConnectionsIsReadOnlyAndDeterministic(t *testing.T) {
	op := Op{Op: OpListConnections, To: "a.inputs.count"}
	res1, err := Apply([]byte(dataGraph), []Op{op})
	if err != nil {
		t.Fatal(err)
	}
	res2, _ := Apply([]byte(dataGraph), []Op{op})
	if !bytes.Equal(res1.Text, []byte(dataGraph)) || !reflect.DeepEqual(res1.Results, res2.Results) {
		t.Error("a query changed the text or answered differently twice")
	}
}

func listOf(t *testing.T, src string, op Op) *QueryResult {
	t.Helper()
	res, err := Apply([]byte(src), []Op{op})
	if err != nil {
		t.Fatalf("list_connections: %v", err)
	}
	q := res.Results[0].Query
	if q == nil || !q.OK {
		t.Fatalf("list_connections: %+v", q)
	}
	return q
}

// Parity: can_connect and every list_connections entry agree with the validator
// for every (source, target) pair of every graph, graph inputs included.
func TestConnectionAnswersAgreeWithTheValidator(t *testing.T) {
	for name, src := range editable(t) {
		def := parse(t, string(src))
		sources := append(outputRefs(def), graphInputRefs(def)...)
		targets := inputRefs(def)
		verdict := func(from, to string) (bool, string) {
			after := *def
			if strings.HasPrefix(from, "inputs.") {
				after.GraphInputBindings = append(append([]graph.GraphInputBindingDefinition(nil), def.GraphInputBindings...), graph.GraphInputBindingDefinition{From: graph.GraphInputReference(from), To: graph.NodeInputReference(to)})
			} else {
				after.Edges = append(append([]graph.EdgeDefinition(nil), def.Edges...), graph.EdgeDefinition{From: graph.NodeOutputReference(from), To: graph.NodeInputReference(to)})
			}
			added := newErrors(def, &after)
			if len(added) == 0 {
				return true, ""
			}
			return false, added[0].Code
		}
		check := func(what, from, to string, ok bool, code string) {
			wantOK, wantCode := verdict(from, to)
			if ok != wantOK || code != wantCode {
				t.Errorf("%s: %s %s -> %s: ok=%v code=%q, validator ok=%v code=%q", name, what, from, to, ok, code, wantOK, wantCode)
			}
		}
		for _, from := range sources {
			listed := map[string]ConnectionOption{}
			res, err := Apply(src, []Op{{Op: OpListConnections, From: from}})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, o := range res.Results[0].Query.Options {
				listed[o.Ref] = o
			}
			for _, to := range targets {
				q := queryOf(t, string(src), from, to)
				check("can_connect", from, to, q.OK, q.Code)
				o, found := listed[to]
				if !found {
					t.Errorf("%s: list_connections from %s omits %s", name, from, to)
					continue
				}
				check("list from", from, to, o.OK, o.Code)
			}
		}
		for _, to := range targets {
			res, err := Apply(src, []Op{{Op: OpListConnections, To: to}})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			listed := map[string]ConnectionOption{}
			for _, o := range res.Results[0].Query.Options {
				listed[o.Ref] = o
			}
			for _, from := range sources {
				o, found := listed[from]
				if !found {
					t.Errorf("%s: list_connections to %s omits %s", name, to, from)
					continue
				}
				check("list to", from, to, o.OK, o.Code)
			}
			if len(listed) != len(sources) {
				t.Errorf("%s: list_connections to %s has %d options, want %d", name, to, len(listed), len(sources))
			}
		}
	}
}

func graphInputRefs(def *graph.Definition) []string {
	var refs []string
	for _, id := range sortedKeys(def.Inputs) {
		refs = append(refs, "inputs."+id)
	}
	return refs
}

// Entries are sorted by node id then port, graph inputs first.
func TestListConnectionsOrderOnTheCorpus(t *testing.T) {
	for name, src := range editable(t) {
		def := parse(t, string(src))
		ins := inputRefs(def)
		if len(ins) == 0 {
			continue
		}
		res, err := Apply(src, []Op{{Op: OpListConnections, To: ins[0]}})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, o := range res.Results[0].Query.Options {
			got = append(got, o.Ref)
		}
		want := append(graphInputRefs(def), outputRefs(def)...)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: order %q, want %q", name, got, want)
		}
		if !sort.StringsAreSorted(got[len(def.Inputs):]) {
			t.Errorf("%s: outputs not sorted", name)
		}
	}
}

func TestListConnectionsOnALargeGraphIsFast(t *testing.T) {
	if raceDetector {
		t.Skip("wall-clock budget is measured without the race detector")
	}
	var b strings.Builder
	b.WriteString("schema_version: 0\nid: big\ninputs:\n  seed: {type: string}\nnodes:\n")
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
	// Over the limit is accepted when it is in proportion to the time to parse the
	// same document: a loaded machine is slow for every operation (timing_test.go).
	const samples = 5
	parse := parseTime(t, []byte(src), samples)
	for _, op := range []Op{{Op: OpListConnections, From: "n000.outputs.o"}, {Op: OpListConnections, To: "n100.inputs.i1"}, {Op: OpListConnections, From: "inputs.seed"}} {
		var failure error
		best := bestOf(samples, func() {
			res, err := Apply([]byte(src), []Op{op})
			if err != nil {
				failure = err
				return
			}
			if got := len(res.Results[0].Query.Options); got < n {
				failure = fmt.Errorf("%+v: only %d options", op, got)
			}
		})
		if failure != nil {
			t.Fatal(failure)
		}
		const limit = 500 * time.Millisecond
		if !withinBudget(best, limit, parse, listConnectionsParseRatioBound) {
			t.Errorf("list_connections (from %q, to %q) took %v at best of %d (limit %v) and %.1fx the %v it takes to parse the graph (bound %.0fx)",
				op.From, op.To, best, samples, limit, float64(best)/float64(parse), parse, listConnectionsParseRatioBound)
		}
	}
}
