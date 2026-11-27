package fields

import (
	"reflect"
	"sort"
	"testing"
)

func sample() []Field {
	return []Field{
		{ID: "t.kind", Key: "kind", Kind: Enum, Enum: []string{"a", "b"}},
		{ID: "t.flag", Key: "flag", Kind: Bool},
		{ID: "t.a", Kind: Group, Requires: []Rule{{Key: "kind", Equals: "a"}}, Fields: []Field{
			{ID: "t.a.x", Key: "x", Kind: String},
			{ID: "t.a.shared", Key: "shared", Kind: String},
		}},
		{ID: "t.b", Kind: Group, Requires: []Rule{{Key: "kind", Equals: "b"}}, Fields: []Field{
			{ID: "t.b.shared", Key: "shared", Kind: String},
		}},
		{ID: "t.on", Kind: Group, Requires: []Rule{{Key: "flag", Equals: "true"}}, Fields: []Field{
			{ID: "t.on.y", Key: "y", Kind: String},
		}},
		{ID: "t.nested", Key: "nested", Kind: Group, Fields: []Field{{ID: "t.nested.z", Key: "z", Kind: String}}},
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k, ok := range m {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func TestAllowedKeysFollowTheSelectedGroups(t *testing.T) {
	cases := []struct {
		config map[string]any
		want   []string
	}{
		{nil, []string{"flag", "kind", "nested"}},
		{map[string]any{"kind": "a"}, []string{"flag", "kind", "nested", "shared", "x"}},
		{map[string]any{"kind": "b"}, []string{"flag", "kind", "nested", "shared"}},
		{map[string]any{"kind": 7}, []string{"flag", "kind", "nested"}},
		{map[string]any{"flag": true}, []string{"flag", "kind", "nested", "y"}},
		{map[string]any{"flag": false}, []string{"flag", "kind", "nested"}},
		{map[string]any{"flag": "true"}, []string{"flag", "kind", "nested", "y"}},
	}
	for _, c := range cases {
		if got := keys(AllowedKeys(sample(), c.config)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("AllowedKeys(%v) = %v, want %v", c.config, got, c.want)
		}
	}
}

func TestKeysListsEveryKeyOnceInOrder(t *testing.T) {
	want := []string{"kind", "flag", "x", "shared", "y", "nested"}
	if got := Keys(sample()); !reflect.DeepEqual(got, want) {
		t.Errorf("Keys = %v, want %v", got, want)
	}
}

func TestActiveReturnsTheSelectedFieldsInOrder(t *testing.T) {
	var ids []string
	for _, f := range Active(sample(), map[string]any{"kind": "a", "flag": true}) {
		ids = append(ids, f.ID)
	}
	want := []string{"t.kind", "t.flag", "t.a.x", "t.a.shared", "t.on.y", "t.nested"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("Active = %v, want %v", ids, want)
	}
}

func TestPresenceRuleNeedsOnlyTheKey(t *testing.T) {
	fs := []Field{{ID: "g", Kind: Group, Requires: []Rule{{Key: "mode"}}, Fields: []Field{{ID: "g.k", Key: "k", Kind: String}}}}
	if AllowedKeys(fs, map[string]any{})["k"] {
		t.Error("k is allowed without mode")
	}
	if !AllowedKeys(fs, map[string]any{"mode": nil})["k"] {
		t.Error("k is refused although mode is present")
	}
}

func TestInputRulesDoNotGateKeys(t *testing.T) {
	fs := []Field{{ID: "g", Kind: Group, Requires: []Rule{{Input: "task"}}, Fields: []Field{{ID: "g.k", Key: "k", Kind: String}}}}
	if !AllowedKeys(fs, nil)["k"] {
		t.Error("an input rule changed which keys are accepted")
	}
}

type namedConfig map[string]any

func TestReadsInputFollowsTheDescriptors(t *testing.T) {
	fs := []Field{
		{ID: "t.env", Key: "env", Kind: Map, InputTypes: []string{"string"}},
		{ID: "t.ctx", Key: "ctx", Kind: List, Item: &Field{ID: "t.ctx.item", Kind: InputName}},
		{ID: "t.in", Key: "in", Kind: InputName},
		{ID: "t.task", Key: "task", Kind: Text, Excludes: []Rule{{Input: "task"}}},
		{ID: "t.plain", Key: "plain", Kind: Map},
	}
	config := map[string]any{"env": namedConfig{"a": "A"}, "ctx": []any{"b"}, "in": "c", "plain": map[string]any{"d": "D"}}
	for input, want := range map[string]bool{"a": true, "b": true, "c": true, "task": true, "d": false, "e": false} {
		if got := ReadsInput(fs, config, input); got != want {
			t.Errorf("ReadsInput(%q) = %v, want %v", input, got, want)
		}
	}
}
