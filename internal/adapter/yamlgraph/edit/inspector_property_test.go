package edit

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

// randomInspectorOp returns an inspector operation on inspectorGraph's nodes.
// Some are refused (a port that does not exist yet, an invalid loop); that is
// part of the walk.
func randomInspectorOp(rng *rand.Rand, step int) Op {
	nodes := []string{"write", "check", "approve"}
	node := nodes[rng.Intn(len(nodes))]
	dir := []string{DirectionInput, DirectionOutput}[rng.Intn(2)]
	port := fmt.Sprintf("p%d", rng.Intn(4))
	values := []json.RawMessage{raw("x"), raw(rng.Intn(100)), raw(true), raw([]string{"a", "b"}), json.RawMessage(`{"k": {"n": [1, 2]}}`), json.RawMessage(`null`)}
	keys := [][]string{{"command"}, {"args"}, {"cwd"}, {"env", "MODE"}, {"prompt"}, {"zz_new"}, {"zz_new", "inner"}}
	switch rng.Intn(11) {
	case 9, 10:
		return randomLoopOp(rng, []string{"write", "write", node}[rng.Intn(3)])
	case 0, 1:
		return Op{Op: OpSetConfig, Node: node, Path: keys[rng.Intn(len(keys))], Value: values[rng.Intn(len(values))]}
	case 2:
		return Op{Op: OpUnsetConfig, Node: node, Path: keys[rng.Intn(len(keys))]}
	case 3:
		return Op{Op: OpAddPort, Node: node, Direction: dir, Port: port, Type: []string{"string", "json", "number"}[rng.Intn(3)], Required: boolPtr(rng.Intn(2) == 0)}
	case 4:
		return Op{Op: OpUpdatePort, Node: node, Direction: dir, Port: port, NewPort: fmt.Sprintf("q%d", step), Required: boolPtr(rng.Intn(2) == 0)}
	case 5:
		return Op{Op: OpRemovePort, Node: node, Direction: dir, Port: port}
	case 6:
		return Op{Op: OpSetLoop, Node: "write", MaxAttempts: intPtr(1 + rng.Intn(10)),
			Gate:     &LoopGate{Inputs: map[string]PortSpec{"doc": {Type: "json", Required: true}}, Config: json.RawMessage(`{"check_id":"c","kind":"json_predicate"}`)},
			Bindings: []LoopBinding{{From: "candidate.outputs.json", To: "gate.inputs.doc"}}}
	case 7:
		return Op{Op: OpClearLoop, Node: node}
	default:
		units := []string{}
		for _, n := range nodes {
			if rng.Intn(2) == 0 {
				units = append(units, n)
			}
		}
		return Op{Op: OpSetUnits, Units: units}
	}
}

// unknownKeys is what must survive: the typed values and the exact lines.
func unknownKeys(t *testing.T, src string) (map[string]any, []string) {
	t.Helper()
	def := parse(t, src)
	values := map[string]any{"write": def.Nodes["write"].Config["zz_extra"], "approve": def.Nodes["approve"].Config["zz_note"]}
	var lines []string
	for _, l := range strings.Split(src, "\n") {
		if strings.Contains(l, "zz_extra") || strings.Contains(l, "zz_note") {
			lines = append(lines, l)
		}
	}
	return values, lines
}

func TestUnknownConfigKeysSurviveRandomInspectorEdits(t *testing.T) {
	wantValues, wantLines := unknownKeys(t, inspectorGraph)
	for seed := int64(1); seed <= 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		src := inspectorGraph
		applied := 0
		for step := 0; applied < 20 && step < 200; step++ {
			op := randomInspectorOp(rng, step)
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
		values, lines := unknownKeys(t, src)
		if !reflect.DeepEqual(values, wantValues) || !reflect.DeepEqual(lines, wantLines) {
			t.Fatalf("seed %d: unknown keys changed: %v %q, want %v %q\n%s", seed, values, lines, wantValues, wantLines, src)
		}
	}
}

// The inspector operations never trip the safety net on the shipped graphs.
func TestRandomInspectorWalksOnRepoGraphs(t *testing.T) {
	for name, start := range repoGraphs(t) {
		rng := rand.New(rand.NewSource(int64(len(name)) + 7))
		src := string(start)
		for step := 0; step < 150; step++ {
			def := parse(t, src)
			ids := make([]string, 0, len(def.Nodes))
			for id := range def.Nodes {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			if len(ids) == 0 {
				break
			}
			id := ids[rng.Intn(len(ids))]
			op := walkOp(rng, def.Nodes[id], id, step)
			res, err := Apply([]byte(src), []Op{op})
			if err != nil {
				if e := err.(*Error); e.Code == CodeSafetyNet {
					t.Fatalf("%s step %d: safety net tripped on %+v: %v\n%s", name, step, op, e, src)
				}
				continue
			}
			src = string(res.Text)
			if _, err := graph.Parse(res.Text); err != nil {
				t.Fatalf("%s step %d: result does not parse: %v", name, step, err)
			}
		}
	}
}

func walkOp(rng *rand.Rand, n graph.NodeDefinition, id string, step int) Op {
	pick := func(m map[string]graph.PortDefinition) string {
		names := sortedKeys(m)
		if len(names) == 0 {
			return "none"
		}
		return names[rng.Intn(len(names))]
	}
	switch rng.Intn(6) {
	case 0:
		return Op{Op: OpSetConfig, Node: id, Path: []string{fmt.Sprintf("zz_k%d", rng.Intn(3))}, Value: raw(map[string]any{"v": step})}
	case 1:
		cfg := sortedKeys(n.Config)
		if len(cfg) == 0 {
			return Op{Op: OpClearLoop, Node: id}
		}
		return Op{Op: OpUnsetConfig, Node: id, Path: []string{cfg[rng.Intn(len(cfg))]}}
	case 2:
		return Op{Op: OpAddPort, Node: id, Direction: DirectionOutput, Port: fmt.Sprintf("o%d", step), Type: "string"}
	case 3:
		return Op{Op: OpUpdatePort, Node: id, Direction: DirectionInput, Port: pick(n.Inputs), NewPort: fmt.Sprintf("i%d", step)}
	case 4:
		return Op{Op: OpRemovePort, Node: id, Direction: DirectionOutput, Port: pick(n.Outputs)}
	default:
		return Op{Op: OpSetUnits, Units: []string{}}
	}
}
