package edit

import (
	"fmt"
	"reflect"
	"strings"

	"agentplane/internal/graph"

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

func pathError(id string, path []string, message string) *Error {
	e := nodeError(CodeConfigPath, id, message, "edit this part of the configuration in the YAML text")
	e.Path = "nodes." + id + ".config." + strings.Join(path, ".")
	return e
}

func (s *session) setConfig(op Op) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	value, typed, verr := decodeValue(op.Value)
	if verr != nil {
		e := nodeError(CodeValue, op.Node, verr.Error(), "send a JSON string, number, boolean, null, list or object")
		e.Path = "nodes." + op.Node + ".config." + strings.Join(op.Path, ".")
		return e
	}
	// typed prediction
	if n.Config == nil {
		n.Config = graph.NodeConfig{}
	}
	parent := map[string]any(n.Config)
	for i, k := range op.Path[:len(op.Path)-1] {
		next, present := parent[k]
		if !present {
			created := graph.NodeConfig{}
			parent[k], parent = created, created
			continue
		}
		m, ok := asMapping(next)
		if !ok {
			return pathError(op.Node, op.Path[:i+1], fmt.Sprintf("config %s is not a mapping, so nothing can be set below it", strings.Join(op.Path[:i+1], ".")))
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
	return s.treeSetConfig(op, value)
}

func (s *session) treeSetConfig(op Op, value *yaml.Node) *Error {
	node, err := s.nodeTree(op.Node)
	if err != nil {
		return err
	}
	m, ok := childMapping(node, "config", true, func(v *yaml.Node) { insertNodeKey(node, "config", v) })
	if !ok {
		return pathError(op.Node, nil, "config is not a mapping")
	}
	for i, k := range op.Path[:len(op.Path)-1] {
		parent := m
		if m, ok = childMapping(parent, k, true, func(v *yaml.Node) { parent.Content = append(parent.Content, str(k), v) }); !ok {
			return pathError(op.Node, op.Path[:i+1], fmt.Sprintf("config %s is not a mapping", strings.Join(op.Path[:i+1], ".")))
		}
	}
	last := op.Path[len(op.Path)-1]
	if old, i := get(m, last); i >= 0 {
		adoptValue(old, value)
		m.Content[i+1] = value
		return nil
	}
	m.Content = append(m.Content, str(last), value)
	return nil
}

func (s *session) unsetConfig(op Op) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	// typed prediction: walk down, remove, then drop parents left empty
	// (never the config mapping itself).
	chain := []map[string]any{map[string]any(n.Config)}
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
	// tree
	node, err := s.nodeTree(op.Node)
	if err != nil {
		return err
	}
	trees := []*yaml.Node{}
	m, _ := get(node, "config")
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
