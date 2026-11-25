// Package fieldstest proves that field descriptors equal the validator that
// owns them (F2.2): every value a descriptor allows is accepted, and every
// value it forbids (missing required key, wrong kind, enum, bounds, format,
// locked value, input reference, cross-field rule, unknown key) is refused.
package fieldstest

import (
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
)

// Spec is one validator and the descriptors it owns.
type Spec struct {
	// Fields are the descriptors under test.
	Fields []fields.Field
	// Base is a valid node that sets the required keys (and selects a Gate
	// kind, when there is one). Only fields active for Base are exercised.
	Base graph.NodeDefinition
	// Samples maps each active descriptor id to a valid value for its key.
	// A missing sample fails the test, so a new descriptor needs a test.
	Samples map[string]any
	// Validate returns nil when the validator accepts the node.
	Validate func(graph.NodeDefinition) error
}

// Run checks s. Names of failing cases say which descriptor and rule broke.
func Run(t *testing.T, s Spec) {
	t.Helper()
	if err := s.Validate(s.Base); err != nil {
		t.Fatalf("base node is refused: %v", err)
	}
	active := activeLeaves(s.Fields, s.Base.Config)
	selectors := selectorKeys(s.Fields)
	for _, f := range active {
		sample, ok := s.Samples[f.ID]
		if !ok {
			t.Errorf("descriptor %s has no sample value: add one so the field is proven", f.ID)
			continue
		}
		for _, c := range cases(f, sample, s.Base, selectors[f.Key]) {
			err := s.Validate(c.node)
			if c.accept && err != nil {
				t.Errorf("%s: %s should be accepted, got %v", f.ID, c.name, err)
			}
			if !c.accept && err == nil {
				t.Errorf("%s: %s should be refused, was accepted", f.ID, c.name)
			}
		}
	}
	unknown := Clone(s.Base)
	unknown.Config["zz_unknown"] = "x"
	if s.Validate(unknown) == nil {
		t.Errorf("a config key without a descriptor is accepted")
	}
	for _, key := range inactiveKeys(s.Fields, s.Base.Config) {
		n := Clone(s.Base)
		n.Config[key] = "x"
		if s.Validate(n) == nil {
			t.Errorf("key %q of an unselected group is accepted", key)
		}
	}
}

type testCase struct {
	name   string
	node   graph.NodeDefinition
	accept bool
}

// cases derives the accepted and refused variants of one field. selector is
// true when the field's value selects a group (a Gate kind), so other enum
// members would need another base.
func cases(f fields.Field, sample any, base graph.NodeDefinition, selector bool) []testCase {
	with := func(v any) graph.NodeDefinition {
		n := Clone(base)
		for _, r := range f.Excludes {
			if r.Key != "" {
				delete(n.Config, r.Key)
			}
			if r.Input != "" {
				delete(n.Inputs, r.Input)
			}
		}
		n.Config[f.Key] = v
		return n
	}
	out := []testCase{{name: "a valid value", node: with(sample), accept: true}}
	if f.Required {
		n := with(sample)
		delete(n.Config, f.Key)
		out = append(out, testCase{name: "omitted although required", node: n})
	}
	if f.Locked == nil && f.Kind != fields.InputName && f.Kind != fields.Enum {
		out = append(out, testCase{name: "a value of the wrong kind", node: with(wrongKind(f.Kind))})
	}
	out = append(out, rules(f, sample, base, with)...)
	out = append(out, bounds(f, with)...)
	out = append(out, formats(f, with)...)
	out = append(out, inputs(f, base, with)...)
	out = append(out, allowed(f, sample, selector, with)...)
	return out
}

// allowed derives values the descriptor permits, so a descriptor looser than
// its validator fails: every enum member, omission of an optional field,
// every allowed input type, an optional input when one is allowed, and values
// that satisfy only the stated format.
func allowed(f fields.Field, sample any, selector bool, with func(any) graph.NodeDefinition) []testCase {
	var out []testCase
	if f.Kind == fields.Enum && !selector {
		for _, m := range f.Enum {
			out = append(out, testCase{name: fmt.Sprintf("enum member %q", m), node: with(m), accept: true})
		}
	}
	if !f.Required && len(f.Excludes) == 0 {
		n := with(sample)
		delete(n.Config, f.Key)
		out = append(out, testCase{name: "omitted, being optional", node: n, accept: true})
	}
	if f.Kind == fields.InputName {
		for _, t := range f.InputTypes {
			n := with("zz_ok")
			n.Inputs["zz_ok"] = graph.PortDefinition{Type: graph.ValueType(t), Required: true}
			out = append(out, testCase{name: "a required input of type " + t, node: n, accept: true})
			if !f.InputRequired {
				opt := with("zz_ok")
				opt.Inputs["zz_ok"] = graph.PortDefinition{Type: graph.ValueType(t)}
				out = append(out, testCase{name: "an optional input of type " + t, node: opt, accept: true})
			}
		}
	}
	text, _ := sample.(string)
	if (f.Kind == fields.String || f.Kind == fields.Text) && f.Locked == nil {
		for _, good := range goodFormat(f.Format, text) {
			out = append(out, testCase{name: fmt.Sprintf("%q satisfying %q", good, f.Format), node: with(good), accept: true})
		}
	}
	if f.Item != nil && (f.Kind == fields.List || f.Kind == fields.PathList) && f.Item.Kind != fields.InputName {
		for _, good := range goodFormat(f.Item.Format, "") {
			out = append(out, testCase{name: fmt.Sprintf("an item %q satisfying %q", good, f.Item.Format), node: with([]any{good}), accept: true})
		}
	}
	return out
}

// goodFormat lists strings that satisfy format and nothing stricter. sample
// is a valid value of the field, used to build an absolute path.
func goodFormat(format fields.Format, sample string) []string {
	switch format {
	case "":
		return []string{"", "  ", "any text"}
	case fields.FormatNonEmpty:
		return []string{" "}
	case fields.FormatNotBlank:
		return []string{" x ", "relative/dir"}
	case fields.FormatAbsolutePath:
		if sample != "" {
			return []string{sample + "/sub/../x"}
		}
	case fields.FormatEnvName:
		return []string{"lower_case", "1A", "A-B"}
	case fields.FormatRelativePath:
		return []string{"a/b", "dir/"}
	case fields.FormatJSONScalar:
		return []string{"1", "true", "null", `"s"`}
	case fields.FormatSecretName:
		return []string{"a", "tok_1"}
	}
	return nil
}

// selectorKeys lists keys whose value selects a group.
func selectorKeys(fs []fields.Field) map[string]bool {
	keys := map[string]bool{}
	for _, f := range fs {
		if f.Kind == fields.Group {
			for _, r := range f.Requires {
				if r.Key != "" {
					keys[r.Key] = true
				}
			}
		}
	}
	return keys
}

// PinKeys fails unless fs accepts exactly want (in any order). It pins the
// field sets of the F2.2 spec, so a key added to or dropped from a validator
// without the spec and its descriptor changing together fails a test.
func PinKeys(t *testing.T, fs []fields.Field, want ...string) {
	t.Helper()
	got := fields.Keys(fs)
	set := map[string]bool{}
	for _, k := range got {
		set[k] = true
	}
	if len(set) != len(want) {
		t.Errorf("descriptor keys %v, want %v", got, want)
		return
	}
	for _, k := range want {
		if !set[k] {
			t.Errorf("descriptor keys %v lack %q", got, k)
		}
	}
}

func rules(f fields.Field, sample any, base graph.NodeDefinition, with func(any) graph.NodeDefinition) []testCase {
	var out []testCase
	if f.Locked != nil {
		out = append(out, testCase{name: "a value other than the locked one", node: with("/zz/other")})
	}
	if f.Kind == fields.Enum {
		out = append(out, testCase{name: "a value outside the enum", node: with("zz_not_a_member")})
	}
	for _, r := range f.Excludes {
		n := with(sample)
		switch {
		case r.Key != "":
			n.Config[r.Key] = base.Config[r.Key]
			if _, ok := base.Config[r.Key]; !ok {
				n.Config[r.Key] = sample
			}
		case r.Input != "":
			n.Inputs[r.Input] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
		}
		out = append(out, testCase{name: fmt.Sprintf("together with its excluded alternative %+v", r), node: n})
		if f.Required {
			neither := with(sample)
			delete(neither.Config, f.Key)
			out = append(out, testCase{name: "neither it nor its alternative", node: neither})
		}
	}
	return out
}

func bounds(f fields.Field, with func(any) graph.NodeDefinition) []testCase {
	var out []testCase
	if f.MaxBytes > 0 {
		out = append(out,
			testCase{name: "exactly max_bytes", node: with(strings.Repeat("a", f.MaxBytes)), accept: true},
			testCase{name: "one byte over max_bytes", node: with(strings.Repeat("a", f.MaxBytes+1))})
	}
	if f.MaxItems > 0 && f.Item != nil && f.Item.Kind == fields.InputName {
		n := with([]any{})
		items := make([]any, 0, f.MaxItems+1)
		for i := 0; i <= f.MaxItems; i++ {
			name := fmt.Sprintf("zz_in%d", i)
			n.Inputs[name] = graph.PortDefinition{Type: graph.ValueType(f.Item.InputTypes[0]), Required: true}
			items = append(items, name)
		}
		n.Config[f.Key] = items[:f.MaxItems]
		out = append(out, testCase{name: "exactly max_items", node: n, accept: true})
		over := Clone(n)
		over.Config[f.Key] = items
		out = append(out, testCase{name: "one item over max_items", node: over})
	}
	if f.Item != nil && (f.Kind == fields.List || f.Kind == fields.PathList) {
		out = append(out, testCase{name: "an item of the wrong kind", node: with([]any{wrongKind(f.Item.Kind)})})
		for _, bad := range badFormat(f.Item.Format) {
			out = append(out, testCase{name: fmt.Sprintf("an item %q breaking %s", bad, f.Item.Format), node: with([]any{bad})})
		}
		if f.Item.Kind == fields.InputName {
			out = append(out, testCase{name: "an item naming an undeclared input", node: with([]any{"zz_missing"})})
		}
	}
	return out
}

func formats(f fields.Field, with func(any) graph.NodeDefinition) []testCase {
	var out []testCase
	if f.Kind == fields.String || f.Kind == fields.Text {
		for _, bad := range badFormat(f.Format) {
			out = append(out, testCase{name: fmt.Sprintf("%q breaking %s", bad, f.Format), node: with(bad)})
		}
	}
	if f.Kind == fields.Scalar {
		out = append(out, testCase{name: "a floating-point number", node: with(1.5)})
	}
	return out
}

func inputs(f fields.Field, base graph.NodeDefinition, with func(any) graph.NodeDefinition) []testCase {
	var out []testCase
	switch {
	case f.Kind == fields.InputName:
		out = append(out, testCase{name: "an undeclared input", node: with("zz_missing")})
		if wrong, ok := otherType(f.InputTypes); ok {
			n := with("zz_wrong")
			n.Inputs["zz_wrong"] = graph.PortDefinition{Type: wrong, Required: true}
			out = append(out, testCase{name: "an input of another type", node: n})
		}
		if f.InputRequired {
			n := with("zz_optional")
			n.Inputs["zz_optional"] = graph.PortDefinition{Type: graph.ValueType(f.InputTypes[0])}
			out = append(out, testCase{name: "an optional input", node: n})
		}
	case f.Kind == fields.Map && len(f.InputTypes) > 0:
		out = append(out, testCase{name: "a key naming an undeclared input", node: with(map[string]any{"zz_missing": "ZZ_VAR"})})
		if wrong, ok := otherType(f.InputTypes); ok {
			n := with(map[string]any{"zz_wrong": "ZZ_VAR"})
			n.Inputs["zz_wrong"] = graph.PortDefinition{Type: wrong, Required: true}
			out = append(out, testCase{name: "a key naming an input of another type", node: n})
		}
	}
	if f.Kind == fields.Map && f.Item != nil {
		for name, port := range base.Inputs {
			if len(f.InputTypes) > 0 && !contains(f.InputTypes, string(port.Type)) {
				continue
			}
			for _, bad := range badFormat(f.Item.Format) {
				out = append(out, testCase{name: fmt.Sprintf("a value %q breaking %s", bad, f.Item.Format), node: with(map[string]any{name: bad})})
			}
			break
		}
	}
	return out
}

// wrongKind is a value no field of kind k accepts.
func wrongKind(k fields.Kind) any {
	switch k {
	case fields.String, fields.Text, fields.InputName:
		return []any{"x"}
	case fields.Bool:
		return "yes"
	case fields.Int:
		return "3"
	case fields.List, fields.PathList, fields.Map:
		return "x"
	case fields.Scalar:
		return map[string]any{"x": "y"}
	}
	return struct{}{}
}

// badFormat lists strings that break format.
func badFormat(format fields.Format) []string {
	switch format {
	case fields.FormatNonEmpty:
		return []string{""}
	case fields.FormatNotBlank:
		return []string{"", "   "}
	case fields.FormatAbsolutePath:
		return []string{"", "relative/dir"}
	case fields.FormatCleanAbsolutePath:
		return []string{"", "relative/dir", "/tmp/a/../b"}
	case fields.FormatEnvName:
		return []string{"", "A=B", "A\x00B", "agentplane_attempt"}
	case fields.FormatRelativePath:
		return []string{"", "/abs/file", "../outside"}
	case fields.FormatJSONScalar:
		return []string{"", "{", "[1]", "{\"a\":1}"}
	case fields.FormatSecretName:
		return []string{"", "Bad-Name", "1abc", "A", strings.Repeat("a", 65)}
	}
	return nil
}

func otherType(allowed []string) (graph.ValueType, bool) {
	for _, t := range []graph.ValueType{graph.ValueTypeString, graph.ValueTypeJSON, graph.ValueTypeNumber, graph.ValueTypeBoolean} {
		if !contains(allowed, string(t)) {
			return t, true
		}
	}
	return "", false
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// activeLeaves lists the keyed fields that apply to config, groups expanded.
func activeLeaves(fs []fields.Field, config map[string]any) []fields.Field {
	allowed := fields.AllowedKeys(fs, config)
	var out []fields.Field
	var walk func([]fields.Field)
	walk = func(fs []fields.Field) {
		for _, f := range fs {
			if f.Kind == fields.Group && f.Key == "" {
				if groupActive(f, config) {
					walk(f.Fields)
				}
				continue
			}
			if allowed[f.Key] {
				out = append(out, f)
			}
		}
	}
	walk(fs)
	return out
}

func groupActive(g fields.Field, config map[string]any) bool {
	keys := fields.AllowedKeys([]fields.Field{g}, config)
	return len(keys) > 0
}

// inactiveKeys lists keys some group accepts that config's selection does not.
func inactiveKeys(fs []fields.Field, config map[string]any) []string {
	allowed := fields.AllowedKeys(fs, config)
	var out []string
	for _, k := range fields.Keys(fs) {
		if !allowed[k] {
			out = append(out, k)
		}
	}
	return out
}

// Clone copies a node deeply enough that cases cannot share maps.
func Clone(n graph.NodeDefinition) graph.NodeDefinition {
	c := graph.NodeDefinition{Type: n.Type, Loop: n.Loop, Config: graph.NodeConfig{}, Inputs: map[string]graph.PortDefinition{}, Outputs: map[string]graph.PortDefinition{}}
	for k, v := range n.Config {
		c.Config[k] = v
	}
	for k, v := range n.Inputs {
		c.Inputs[k] = v
	}
	for k, v := range n.Outputs {
		c.Outputs[k] = v
	}
	return c
}
