package edit

import (
	"fmt"

	"agentplane/internal/domain/graph"

	"go.yaml.in/yaml/v3"
)

// Layout lives in metadata.layout.nodes.<id>: {x, y} (ADR-0022). Reading elsewhere
// is tolerant; writing refuses to overwrite a shape it does not understand
// (a scalar where a mapping is expected) rather than destroy it.

func layoutError(id, message string) *Error {
	return &Error{Code: CodeLayout, Message: message, OpIndex: -1, Node: id, Suggestion: "fix or remove metadata.layout in the YAML text"}
}

func (s *session) setLayout(op Op) *Error {
	if op.Clear {
		typedLayoutClear(s.want)
		s.treeLayoutClear()
		return nil
	}
	ids := sortedKeys(op.Nodes)
	for _, id := range ids {
		if _, ok := s.want.Nodes[id]; !ok {
			return nodeError(CodeNodeNotFound, id, fmt.Sprintf("node %q does not exist", id), "")
		}
		if _, _, ok := op.Nodes[id].round(); !ok {
			return layoutError(id, "layout coordinates must be finite numbers within -100000..100000")
		}
	}
	for _, id := range ids {
		x, y, _ := op.Nodes[id].round()
		if err := s.putLayout(id, x, y); err != nil {
			return err
		}
	}
	return nil
}

// putLayout writes one position, in the typed prediction and in the tree.
func (s *session) putLayout(id string, x, y int) *Error {
	// typed prediction
	nodes, err := typedLayoutNodes(s.want, true)
	if err != nil {
		return err
	}
	pos, _ := asMap(nodes[id])
	if pos == nil {
		pos = map[string]any{}
		nodes[id] = pos
	}
	pos["x"], pos["y"] = x, y

	// tree
	treeNodes, err := s.treeLayoutNodes(true)
	if err != nil {
		return err
	}
	if p, _ := get(treeNodes, id); p != nil && p.Kind == yaml.MappingNode {
		setInt(p, "x", x)
		setInt(p, "y", y)
		return nil
	}
	fresh := mapNode(true, str("x"), intNode(x), str("y"), intNode(y))
	if p, i := get(treeNodes, id); p != nil {
		treeNodes.Content[i+1] = fresh // a malformed entry for this node is replaced
	} else {
		treeNodes.Content = append(treeNodes.Content, str(id), fresh)
	}
	return nil
}

func setInt(m *yaml.Node, key string, n int) {
	if v, i := get(m, key); i >= 0 {
		v.Kind, v.Tag, v.Value, v.Style = yaml.ScalarNode, "!!int", fmt.Sprint(n), 0
		v.Content, v.Alias = nil, nil
		return
	}
	m.Content = append(m.Content, str(key), intNode(n))
}

// ---- typed prediction -------------------------------------------------------

// asMap views a decoded YAML mapping as a map. yaml.v3 decodes nested mappings
// below graph.Metadata into graph.Metadata itself, so both types must be accepted.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case graph.Metadata:
		return m, true
	}
	return nil, false
}

func typedLayoutNodes(d *graph.Definition, create bool) (map[string]any, *Error) {
	if d.Metadata == nil {
		if !create {
			return nil, nil
		}
		d.Metadata = graph.Metadata{}
	}
	layout, ok := d.Metadata["layout"]
	if !ok {
		if !create {
			return nil, nil
		}
		layout = map[string]any{}
		d.Metadata["layout"] = layout
	}
	lm, isMap := asMap(layout)
	if !isMap {
		return nil, layoutError("", "metadata.layout is not a mapping")
	}
	nodes, ok := lm["nodes"]
	if !ok {
		if !create {
			return nil, nil
		}
		nodes = map[string]any{}
		lm["nodes"] = nodes
	}
	nm, isMap := asMap(nodes)
	if !isMap {
		return nil, layoutError("", "metadata.layout.nodes is not a mapping")
	}
	return nm, nil
}

func typedLayoutClear(d *graph.Definition) {
	if d.Metadata != nil {
		delete(d.Metadata, "layout")
		if len(d.Metadata) == 0 {
			d.Metadata = nil
		}
	}
}

func typedLayoutRemove(d *graph.Definition, id string) {
	nodes, err := typedLayoutNodes(d, false)
	if err != nil || nodes == nil {
		return
	}
	if _, ok := nodes[id]; !ok {
		return
	}
	delete(nodes, id)
	if len(nodes) == 0 {
		lm, _ := asMap(d.Metadata["layout"])
		delete(lm, "nodes")
		if len(lm) == 0 {
			typedLayoutClear(d)
		}
	}
}

func typedLayoutRename(d *graph.Definition, from, to string) {
	nodes, err := typedLayoutNodes(d, false)
	if err != nil || nodes == nil {
		return
	}
	if pos, ok := nodes[from]; ok {
		delete(nodes, from)
		nodes[to] = pos
	}
}

// ---- tree -------------------------------------------------------------------

func (s *session) treeLayoutNodes(create bool) (*yaml.Node, *Error) {
	meta, i := get(s.top, "metadata")
	if i < 0 {
		if !create {
			return nil, nil
		}
		meta = mapNode(false)
		insertTop(s.top, "metadata", meta)
	}
	if meta.Kind != yaml.MappingNode {
		return nil, layoutError("", "metadata is not a mapping")
	}
	layout, i := get(meta, "layout")
	if i < 0 {
		if !create {
			return nil, nil
		}
		layout = mapNode(false)
		meta.Content = append(meta.Content, str("layout"), layout)
	}
	if layout.Kind != yaml.MappingNode {
		return nil, layoutError("", "metadata.layout is not a mapping")
	}
	nodes, i := get(layout, "nodes")
	if i < 0 {
		if !create {
			return nil, nil
		}
		nodes = mapNode(false)
		layout.Content = append(layout.Content, str("nodes"), nodes)
	}
	if nodes.Kind != yaml.MappingNode {
		return nil, layoutError("", "metadata.layout.nodes is not a mapping")
	}
	if len(meta.Content) > 0 {
		meta.Style &^= yaml.FlowStyle
	}
	if len(layout.Content) > 0 {
		layout.Style &^= yaml.FlowStyle
	}
	if len(nodes.Content) == 0 {
		nodes.Style &^= yaml.FlowStyle
	}
	return nodes, nil
}

func (s *session) treeLayoutClear() {
	meta, i := get(s.top, "metadata")
	if i < 0 || meta.Kind != yaml.MappingNode {
		return
	}
	remove(meta, "layout")
	if len(meta.Content) == 0 {
		remove(s.top, "metadata")
	}
}

func (s *session) treeLayoutRemove(id string) {
	nodes, err := s.treeLayoutNodes(false)
	if err != nil || nodes == nil {
		return
	}
	if !remove(nodes, id) {
		return
	}
	if len(nodes.Content) == 0 {
		meta, _ := get(s.top, "metadata")
		layout, _ := get(meta, "layout")
		remove(layout, "nodes")
		if len(layout.Content) == 0 {
			s.treeLayoutClear()
		}
	}
}

func (s *session) treeLayoutRename(from, to string) {
	nodes, err := s.treeLayoutNodes(false)
	if err != nil || nodes == nil {
		return
	}
	for i := 0; i+1 < len(nodes.Content); i += 2 {
		if nodes.Content[i].Value == from {
			nodes.Content[i].Value = to
		}
	}
}
