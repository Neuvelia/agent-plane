package edit

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"

	"go.yaml.in/yaml/v3"
)

// nodeKeyOrder is where a node key belongs when it has to be inserted; it is
// the order graph.NodeDefinition encodes in.
var nodeKeyOrder = []string{"type", "loop", "inputs", "config", "outputs"}

// insertNodeKey adds key to a node mapping before the first key that belongs
// later, otherwise at the end.
func insertNodeKey(node *yaml.Node, key string, value *yaml.Node) {
	rank := func(k string) int {
		for i, o := range nodeKeyOrder {
			if o == k {
				return i
			}
		}
		return len(nodeKeyOrder)
	}
	at := len(node.Content)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if rank(node.Content[i].Value) > rank(key) {
			at = i
			break
		}
	}
	node.Content = append(node.Content[:at], append([]*yaml.Node{str(key), value}, node.Content[at:]...)...)
}

// nodeTree returns the mapping of node id in the tree.
func (s *session) nodeTree(id string) (*yaml.Node, *Error) {
	nodes, _ := get(s.top, "nodes")
	n, _ := get(nodes, id)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nodeError(CodeParse, id, fmt.Sprintf("node %q is not a mapping in the file", id), "fix the YAML text")
	}
	return n, nil
}

// childMapping returns the mapping under key in m. With create, an absent key
// or a null value becomes an empty block mapping; anything else that is not a
// mapping is reported.
func childMapping(m *yaml.Node, key string, create bool, insert func(*yaml.Node)) (*yaml.Node, bool) {
	v, _ := get(m, key)
	switch {
	case v == nil:
		if !create {
			return nil, true
		}
		v = mapNode(false)
		insert(v)
	case v.Kind == yaml.ScalarNode && v.Tag == "!!null" && create:
		v.Kind, v.Tag, v.Value, v.Style = yaml.MappingNode, "!!map", "", 0
	case v.Kind != yaml.MappingNode:
		return nil, false
	}
	if create && len(v.Content) == 0 {
		v.Style &^= yaml.FlowStyle
	}
	return v, true
}

func (s *session) knownNode(id string) (graph.NodeDefinition, *Error) {
	n, ok := s.want.Nodes[id]
	if !ok {
		return n, nodeError(CodeNodeNotFound, id, fmt.Sprintf("node %q does not exist", id), "")
	}
	return n, nil
}

func pathError(id, root string, path []string, message string) *Error {
	e := nodeError(CodeConfigPath, id, message, "edit this part of the configuration in the YAML text")
	e.Path = strings.Join(append([]string{"nodes", id, root}, path...), ".")
	return e
}

// configRoot names the mapping a config operation edits: the node's config, or
// with loop the config of the node's loop Gate (set_loop_config,
// unset_loop_config). Both use the same code path, so a loop edit patches the
// file in place exactly as a node config edit does.
func configRoot(loop bool) string {
	if loop {
		return "loop.gate.config"
	}
	return "config"
}

// typedConfig returns the typed mapping a config operation edits, creating an
// empty one with create. n's loop is copied before it can change, so the
// definition the session started from is not shared.
func typedConfig(n *graph.NodeDefinition, id string, loop, create bool) (map[string]any, *Error) {
	if !loop {
		if n.Config == nil && create {
			n.Config = graph.NodeConfig{}
		}
		return n.Config, nil
	}
	if n.Loop == nil {
		return nil, loopError(id, fmt.Sprintf("node %q has no loop", id), "add the loop with set_loop first")
	}
	l := *n.Loop
	if l.Gate.Config == nil && create {
		l.Gate.Config = graph.NodeConfig{}
	}
	n.Loop = &l
	return l.Gate.Config, nil
}

// treeConfig returns the mapping a config operation edits in the tree; with
// create, absent mappings on the way are inserted. Without create a missing
// mapping is nil.
func (s *session) treeConfig(id string, loop, create bool) (*yaml.Node, *Error) {
	node, err := s.nodeTree(id)
	if err != nil {
		return nil, err
	}
	notMapping := func(what string) *Error { return pathError(id, configRoot(loop), nil, what+" is not a mapping") }
	if !loop {
		m, ok := childMapping(node, "config", create, func(v *yaml.Node) { insertNodeKey(node, "config", v) })
		if !ok {
			return nil, notMapping("config")
		}
		return m, nil
	}
	l, _ := get(node, "loop")
	if l == nil || l.Kind != yaml.MappingNode {
		return nil, notMapping("loop")
	}
	gate, ok := childMapping(l, "gate", create, func(v *yaml.Node) { l.Content = append(l.Content, str("gate"), v) })
	if !ok {
		return nil, notMapping("loop.gate")
	}
	if gate == nil {
		return nil, nil
	}
	m, ok := childMapping(gate, "config", create, func(v *yaml.Node) { gate.Content = append(gate.Content, str("config"), v) })
	if !ok {
		return nil, notMapping("loop.gate.config")
	}
	return m, nil
}

func (s *session) setConfig(op Op) *Error { return s.setConfigAt(op, false) }

func (s *session) unsetConfig(op Op) *Error { return s.unsetConfigAt(op, false) }

// ConfigFields returns the config descriptors of a node type: the node's own
// config, or with loop the config of its embedded Gate. Nil means unknown.
type ConfigFields func(nodeType string, loop bool) []fields.Field

// setConfigAt sets one config value. Changing `kind` also removes the keys the
// old kind declared and the new one does not, so the file never keeps keys the
// engine would reject; keys no descriptor lists are never touched.
func (s *session) setConfigAt(op Op, loop bool) *Error {
	var fs []fields.Field
	var old map[string]any
	if s.fields != nil && len(op.Path) == 1 && op.Path[0] == "kind" {
		if n, ok := s.want.Nodes[op.Node]; ok {
			if cfg, err := typedConfig(&n, op.Node, loop, false); err == nil {
				fs, old = s.fields(n.Type, loop), maps.Clone(cfg)
			}
		}
	}
	if err := s.setConfigValue(op, loop); err != nil {
		return err
	}
	if fs == nil {
		return nil
	}
	n := s.want.Nodes[op.Node]
	cfg, err := typedConfig(&n, op.Node, loop, false)
	if err != nil {
		return err
	}
	kept := fields.AllowedKeys(fs, cfg)
	var stale []string
	for k := range fields.AllowedKeys(fs, old) {
		if _, present := cfg[k]; present && !kept[k] {
			stale = append(stale, k)
		}
	}
	slices.Sort(stale)
	for _, k := range stale {
		if err := s.unsetConfigAt(Op{Op: op.Op, Node: op.Node, Path: []string{k}}, loop); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) setConfigValue(op Op, loop bool) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	root := configRoot(loop)
	value, typed, verr := decodeValue(op.Value)
	if verr != nil {
		e := nodeError(CodeValue, op.Node, verr.Error(), "send a JSON string, number, boolean, null, list or object")
		e.Path = strings.Join(append([]string{"nodes", op.Node, root}, op.Path...), ".")
		return e
	}
	// typed prediction
	parent, err := typedConfig(&n, op.Node, loop, true)
	if err != nil {
		return err
	}
	for i, k := range op.Path[:len(op.Path)-1] {
		next, present := parent[k]
		if !present || next == nil {
			// an absent key and a null one are both replaced by a new mapping,
			// as childMapping does in the tree
			created := graph.NodeConfig{}
			parent[k], parent = created, created
			continue
		}
		m, ok := asMapping(next)
		if !ok {
			return pathError(op.Node, root, op.Path[:i+1], fmt.Sprintf("%s %s is not a mapping, so nothing can be set below it", root, strings.Join(op.Path[:i+1], ".")))
		}
		parent = m
	}
	last := op.Path[len(op.Path)-1]
	if current, present := parent[last]; present && reflect.DeepEqual(current, typed) {
		return nil // the same value: the file stays as it is
	}
	parent[last] = typed
	s.want.Nodes[op.Node] = n
	// tree
	m, err := s.treeConfig(op.Node, loop, true)
	if err != nil {
		return err
	}
	for i, k := range op.Path[:len(op.Path)-1] {
		parent := m
		var ok bool
		if m, ok = childMapping(parent, k, true, func(v *yaml.Node) { parent.Content = append(parent.Content, str(k), v) }); !ok {
			return pathError(op.Node, root, op.Path[:i+1], fmt.Sprintf("%s %s is not a mapping", root, strings.Join(op.Path[:i+1], ".")))
		}
	}
	if old, i := get(m, last); i >= 0 {
		adoptValue(old, value)
		m.Content[i+1] = value
		return nil
	}
	m.Content = append(m.Content, str(last), value)
	return nil
}

func (s *session) unsetConfigAt(op Op, loop bool) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	// typed prediction: walk down, remove, then drop parents left empty
	// (never the config mapping itself).
	top, err := typedConfig(&n, op.Node, loop, false)
	if err != nil {
		return err
	}
	chain := []map[string]any{top}
	for _, k := range op.Path[:len(op.Path)-1] {
		m, ok := asMapping(chain[len(chain)-1][k])
		if !ok {
			return nil // nothing is set at this path
		}
		chain = append(chain, m)
	}
	last := op.Path[len(op.Path)-1]
	if _, present := chain[len(chain)-1][last]; !present {
		return nil
	}
	delete(chain[len(chain)-1], last)
	for i := len(chain) - 1; i > 0 && len(chain[i]) == 0; i-- {
		delete(chain[i-1], op.Path[i-1])
	}
	s.want.Nodes[op.Node] = n
	// tree
	m, err := s.treeConfig(op.Node, loop, false)
	if err != nil {
		return err
	}
	trees := []*yaml.Node{}
	for _, k := range op.Path[:len(op.Path)-1] {
		trees = append(trees, m)
		m, _ = get(m, k)
	}
	trees = append(trees, m)
	remove(m, last)
	for i := len(trees) - 1; i > 0 && len(trees[i].Content) == 0; i-- {
		remove(trees[i-1], op.Path[i-1])
	}
	return nil
}
