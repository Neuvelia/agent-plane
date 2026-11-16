package edit

import (
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// canonicalOrder is where a top-level key belongs when it has to be inserted.
var canonicalOrder = []string{"schema_version", "id", "name", "metadata", "units", "inputs", "nodes", "edges", "graph_input_bindings", "outputs"}

func orderIndex(key string) int {
	for i, k := range canonicalOrder {
		if k == key {
			return i
		}
	}
	return len(canonicalOrder)
}

func str(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
func intNode(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(n)}
}
func mapNode(flow bool, kv ...*yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: kv}
	if flow {
		n.Style = yaml.FlowStyle
	}
	return n
}

// docMapping returns the top-level mapping of a parsed document.
func docMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

// get returns the value under key and the index of the key in m.Content, or nil, -1.
func get(m *yaml.Node, key string) (*yaml.Node, int) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], i
		}
	}
	return nil, -1
}

func remove(m *yaml.Node, key string) bool {
	if _, i := get(m, key); i >= 0 {
		m.Content = append(m.Content[:i], m.Content[i+2:]...)
		return true
	}
	return false
}

// insertTop adds key to the top-level mapping at its canonical place: before the
// first existing key that belongs later, otherwise at the end.
func insertTop(m *yaml.Node, key string, value *yaml.Node) {
	at := len(m.Content)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if orderIndex(m.Content[i].Value) > orderIndex(key) {
			at = i
			break
		}
	}
	pair := []*yaml.Node{str(key), value}
	m.Content = append(m.Content[:at], append(pair, m.Content[at:]...)...)
}

// ensureMapping returns the mapping under key, creating an empty block mapping
// if the key is absent. A key holding something else is an error.
func ensureMapping(m *yaml.Node, key string) (*yaml.Node, error) {
	v, _ := get(m, key)
	if v == nil {
		v = mapNode(false)
		m.Content = append(m.Content, str(key), v)
		return v, nil
	}
	if v.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s is not a mapping", key)
	}
	if len(v.Content) == 0 {
		v.Style &^= yaml.FlowStyle // an empty flow `{}` becomes a block we can fill
	}
	return v, nil
}

// refNode splits "NODE.outputs.PORT" and friends. Node ids contain no dots.
func refNode(ref string) string {
	if i := strings.IndexByte(ref, '.'); i > 0 {
		return ref[:i]
	}
	return ""
}

func hasNodePrefix(ref, id string) bool { return strings.HasPrefix(ref, id+".") }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ReadOnlyReasons names the constructs that make canvas edits unsafe: anchors,
// aliases and merge keys. An edit made below an anchor can change every node that
// merges or aliases it, so such files are not patched.
func ReadOnlyReasons(doc *yaml.Node) []string {
	seen := map[string]bool{}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.AliasNode {
			seen["alias"] = true
		}
		if n.Anchor != "" {
			seen["anchor"] = true
		}
		if n.Kind == yaml.ScalarNode && n.Value == "<<" && n.Tag == "!!merge" {
			seen["merge_key"] = true
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(doc)
	return sortedKeys(seen)
}
