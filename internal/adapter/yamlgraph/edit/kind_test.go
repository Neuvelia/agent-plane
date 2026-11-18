package edit

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/fields"
)

// gateDescriptors mirrors the shape of the Gate's: shared keys, one keyless group
// per kind, and a key (input) that two kinds both declare.
func gateDescriptors(nodeType string, loop bool) []fields.Field {
	if nodeType != "gate" && !loop {
		return nil
	}
	is := func(k string) []fields.Rule { return []fields.Rule{{Key: "kind", Equals: k}} }
	str := func(key string) fields.Field { return fields.Field{Key: key, Kind: fields.String, Required: true} }
	return []fields.Field{
		str("check_id"),
		{Key: "kind", Kind: fields.Enum, Required: true, Enum: []string{"command", "json_predicate", "reported_file_scope"}},
		{Kind: fields.Group, Requires: is("command"), Fields: []fields.Field{str("command"), str("cwd"), str("left_input"), str("right_input")}},
		{Kind: fields.Group, Requires: is("json_predicate"), Fields: []fields.Field{str("input"), str("field"), str("operator"), str("expected"), str("expected_json")}},
		{Kind: fields.Group, Requires: is("reported_file_scope"), Fields: []fields.Field{str("input"), str("allowed_paths")}},
	}
}

func configKeys(t *testing.T, src, node string, loop bool) []string {
	t.Helper()
	def, err := yamlgraph.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	cfg := def.Nodes[node].Config
	if loop {
		cfg = def.Nodes[node].Loop.Gate.Config
	}
	var keys []string
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestChangingAGateKindDropsTheOldKindsKeys(t *testing.T) {
	apply := func(src string, ops ...Op) string {
		res, err := ApplyWith([]byte(src), ops, gateDescriptors)
		if err != nil {
			t.Fatalf("ApplyWith: %v", err)
		}
		return string(res.Text)
	}
	cases := []struct {
		name string
		node string
		loop bool
		op   Op
		want []string
	}{
		{"loop json_predicate to command", "write", true, Op{Op: OpSetLoopConfig, Node: "write", Path: []string{"kind"}, Value: raw("command")},
			[]string{"check_id", "kind", "zz_note"}},
		{"loop json_predicate to reported_file_scope keeps the shared input", "write", true, Op{Op: OpSetLoopConfig, Node: "write", Path: []string{"kind"}, Value: raw("reported_file_scope")},
			[]string{"check_id", "input", "kind", "zz_note"}},
		{"standalone command to json_predicate", "check", false, Op{Op: OpSetConfig, Node: "check", Path: []string{"kind"}, Value: raw("json_predicate")},
			[]string{"check_id", "kind", "zz_note"}},
	}
	for _, c := range cases {
		note := Op{Op: OpSetConfig, Node: c.node, Path: []string{"zz_note"}, Value: raw("keep")}
		if c.loop {
			note.Op = OpSetLoopConfig
		}
		src := apply(inspectorGraph, note)
		out := apply(src, c.op)
		if got := configKeys(t, out, c.node, c.loop); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: keys %q, want %q\n%s", c.name, got, c.want, out)
		}
	}
	// the same kind again changes nothing; a set with no descriptors changes only the kind
	same := apply(inspectorGraph, Op{Op: OpSetLoopConfig, Node: "write", Path: []string{"kind"}, Value: raw("json_predicate")})
	if same != inspectorGraph {
		t.Errorf("the same kind changed the file:\n%s", same)
	}
	plain := mustApply(t, inspectorGraph, Op{Op: OpSetLoopConfig, Node: "write", Path: []string{"kind"}, Value: raw("command")})
	if !strings.Contains(plain, "expected: ok") {
		t.Errorf("Apply without descriptors must not drop keys:\n%s", plain)
	}
}
