package edit

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// loopGraph is inspectorGraph with keys no descriptor lists in the loop Gate's
// config: a float written 1.0, a YAML date and a comment. A targeted loop edit
// must leave them byte-identical.
var loopGraph = strings.Replace(inspectorGraph, "          expected: ok\n",
	"          expected: ok\n          # a ratio no descriptor lists\n          zz_ratio: 1.0 # stays 1.0\n          zz_when: 2026-01-02\n", 1)

func loopKeyLines(src string) []string {
	var out []string
	for _, l := range strings.Split(src, "\n") {
		if strings.Contains(l, "zz_ratio") || strings.Contains(l, "zz_when") || strings.Contains(l, "# a ratio") {
			out = append(out, l)
		}
	}
	return out
}

// Each targeted loop operation changes exactly the lines it names.
func TestTargetedLoopEditsPatchInPlace(t *testing.T) {
	cases := []struct {
		name           string
		ops            []Op
		removed, added []string
	}{
		{"config value", []Op{{Op: OpSetLoopConfig, Node: "write", Path: []string{"expected"}, Value: raw("bad")}},
			[]string{"          expected: ok"}, []string{"          expected: bad"}},
		{"config unset", []Op{{Op: OpUnsetLoopConfig, Node: "write", Path: []string{"operator"}}},
			[]string{"          operator: eq"}, nil},
		{"config new key", []Op{{Op: OpSetLoopConfig, Node: "write", Path: []string{"zz_new"}, Value: raw(2)}},
			nil, []string{"          zz_new: 2"}},
		{"attempts", []Op{{Op: OpSetLoopAttempts, Node: "write", MaxAttempts: intPtr(5)}},
			[]string{"      max_attempts: 2"}, []string{"      max_attempts: 5"}},
		{"attempts default", []Op{{Op: OpSetLoopAttempts, Node: "write"}},
			[]string{"      max_attempts: 2"}, nil},
		{"gate input added and bound", []Op{
			{Op: OpSetLoopGateInput, Node: "write", Port: "text", Type: "string"},
			{Op: OpSetLoopBindings, Node: "write", Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}, {From: "candidate.outputs.stdout", To: "gate.inputs.text"}}},
		}, nil, []string{"          text: {type: string}", "        - {from: candidate.outputs.stdout, to: gate.inputs.text}"}},
		{"gate input required", []Op{{Op: OpSetLoopGateInput, Node: "write", Port: "doc", Type: "json", Required: boolPtr(false)}},
			[]string{"          doc: {type: json, required: true}"}, []string{"          doc: {type: json}"}},
	}
	for _, c := range cases {
		out := mustApply(t, loopGraph, c.ops...)
		removed, added := lineDiff(loopGraph, out)
		if !reflect.DeepEqual(removed, c.removed) || !reflect.DeepEqual(added, c.added) {
			t.Errorf("%s: -%q +%q, want -%q +%q\n%s", c.name, removed, added, c.removed, c.added, out)
		}
		if got := loopKeyLines(out); !reflect.DeepEqual(got, loopKeyLines(loopGraph)) {
			t.Errorf("%s: unknown loop keys changed: %q", c.name, got)
		}
	}
	// adding a gate input and removing it again restores the file
	added := mustApply(t, loopGraph, Op{Op: OpSetLoopGateInput, Node: "write", Port: "text", Type: "string"})
	if back := mustApply(t, added, Op{Op: OpRemoveLoopGateInput, Node: "write", Port: "text"}); back != loopGraph {
		t.Errorf("remove_loop_gate_input did not restore the file:\n%s", back)
	}
}

func TestTargetedLoopEditRefusals(t *testing.T) {
	cases := []struct {
		name string
		op   Op
		code string
	}{
		{"no loop", Op{Op: OpSetLoopConfig, Node: "check", Path: []string{"x"}, Value: raw(1)}, CodeLoopInvalid},
		{"no loop attempts", Op{Op: OpSetLoopAttempts, Node: "approve", MaxAttempts: intPtr(2)}, CodeLoopInvalid},
		{"attempts zero", Op{Op: OpSetLoopAttempts, Node: "write", MaxAttempts: intPtr(0)}, CodeLoopInvalid},
		{"attempts too large", Op{Op: OpSetLoopAttempts, Node: "write", MaxAttempts: intPtr(11)}, CodeLoopInvalid},
		{"config below a scalar", Op{Op: OpSetLoopConfig, Node: "write", Path: []string{"kind", "x"}, Value: raw(1)}, CodeConfigPath},
		{"bound input removed", Op{Op: OpRemoveLoopGateInput, Node: "write", Port: "doc"}, CodeLoopInvalid},
		{"missing input removed", Op{Op: OpRemoveLoopGateInput, Node: "write", Port: "nope"}, CodePortNotFound},
		{"bound input retyped", Op{Op: OpSetLoopGateInput, Node: "write", Port: "doc", Type: "string"}, CodeLoopInvalid},
		{"bad input name", Op{Op: OpSetLoopGateInput, Node: "write", Port: "Bad", Type: "string"}, CodeLoopInvalid},
		{"no candidate binding", Op{Op: OpSetLoopBindings, Node: "write", Bindings: []LoopBinding{}}, CodeLoopInvalid},
		{"unequal binding types", Op{Op: OpSetLoopBindings, Node: "write", Bindings: []LoopBinding{{From: "candidate.outputs.stdout", To: "gate.inputs.doc"}}}, CodeLoopInvalid},
	}
	for _, c := range cases {
		if e := applyErr(t, loopGraph, c.op); e.Code != c.code {
			t.Errorf("%s: code %s, want %s (%v)", c.name, e.Code, c.code, e)
		}
	}
	if e := applyErr(t, loopGraph, Op{Op: OpSetLoopConfig, Node: "write", Path: []string{"kind", "x"}, Value: raw(1)}); e.Path != "nodes.write.loop.gate.config.kind" {
		t.Errorf("loop config path error at %q", e.Path)
	}
}

func TestDecodeChecksTheTargetedLoopOperations(t *testing.T) {
	bad := []string{
		`[{"op":"set_loop_attempts"}]`,
		`[{"op":"set_loop_config","node":"a","path":["x"]}]`,
		`[{"op":"unset_loop_config","node":"a","path":[]}]`,
		`[{"op":"set_loop_gate_input","node":"a","port":"p"}]`,
		`[{"op":"remove_loop_gate_input","node":"a"}]`,
		`[{"op":"set_loop_bindings","node":"a"}]`,
	}
	for _, b := range bad {
		if _, err := Decode([]byte(b)); err == nil {
			t.Errorf("Decode accepted %s", b)
		}
	}
	good := `[{"op":"set_loop_attempts","node":"a","max_attempts":null},{"op":"set_loop_config","node":"a","path":["x"],"value":1},` +
		`{"op":"set_loop_gate_input","node":"a","port":"p","type":"json"},{"op":"set_loop_bindings","node":"a","bindings":[]}]`
	if _, err := Decode([]byte(good)); err != nil {
		t.Errorf("Decode refused %s: %v", good, err)
	}
}

// Random inspector walks that keep the loop (no set_loop or clear_loop) leave
// the loop Gate's unknown keys byte-identical.
func TestUnknownLoopGateKeysSurviveRandomInspectorEdits(t *testing.T) {
	wantLines := loopKeyLines(loopGraph)
	wantValues := parse(t, loopGraph).Nodes["write"].Loop.Gate.Config
	for seed := int64(1); seed <= 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		src := loopGraph
		applied := 0
		for step := 0; applied < 20 && step < 300; step++ {
			op := randomInspectorOp(rng, step)
			if op.Op == OpSetLoop || op.Op == OpClearLoop {
				continue
			}
			res, err := Apply([]byte(src), []Op{op})
			if err != nil {
				if e := err.(*Error); e.Code == CodeSafetyNet {
					t.Fatalf("seed %d step %d: safety net tripped on %+v: %v\n%s", seed, step, op, e, src)
				}
				continue
			}
			applied++
			src = string(res.Text)
		}
		if applied < 20 {
			t.Fatalf("seed %d: only %d operations applied", seed, applied)
		}
		if lines := loopKeyLines(src); !reflect.DeepEqual(lines, wantLines) {
			t.Fatalf("seed %d: unknown loop keys changed: %q, want %q\n%s", seed, lines, wantLines, src)
		}
		cfg := parse(t, src).Nodes["write"].Loop.Gate.Config
		for _, k := range []string{"zz_ratio", "zz_when"} {
			if !reflect.DeepEqual(cfg[k], wantValues[k]) {
				t.Fatalf("seed %d: %s is %#v, want %#v", seed, k, cfg[k], wantValues[k])
			}
		}
	}
}

// randomLoopOp is a targeted loop operation for randomInspectorOp.
func randomLoopOp(rng *rand.Rand, node string) Op {
	keys := [][]string{{"expected"}, {"operator"}, {"field"}, {"zz_loop"}, {"zz_loop", "inner"}}
	values := []json.RawMessage{raw("x"), raw(rng.Intn(100)), raw(true), raw([]string{"a"}), json.RawMessage(`null`)}
	switch rng.Intn(6) {
	case 0:
		if rng.Intn(3) == 0 {
			return Op{Op: OpSetLoopAttempts, Node: node}
		}
		return Op{Op: OpSetLoopAttempts, Node: node, MaxAttempts: intPtr(rng.Intn(12))}
	case 1:
		return Op{Op: OpSetLoopConfig, Node: node, Path: keys[rng.Intn(len(keys))], Value: values[rng.Intn(len(values))]}
	case 2:
		return Op{Op: OpUnsetLoopConfig, Node: node, Path: keys[rng.Intn(len(keys))]}
	case 3:
		return Op{Op: OpSetLoopGateInput, Node: node, Port: []string{"doc", "text", "extra"}[rng.Intn(3)], Type: []string{"string", "json"}[rng.Intn(2)], Required: boolPtr(rng.Intn(2) == 0)}
	case 4:
		return Op{Op: OpRemoveLoopGateInput, Node: node, Port: []string{"doc", "text", "extra"}[rng.Intn(3)]}
	default:
		bindings := []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}}
		if rng.Intn(2) == 0 {
			bindings = append(bindings, LoopBinding{From: "candidate.outputs.stdout", To: "gate.inputs.text"})
		}
		return Op{Op: OpSetLoopBindings, Node: node, Bindings: bindings}
	}
}
