package desktop

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"agentplane/internal/agent"
	"agentplane/internal/gate"
	"agentplane/internal/graph"
	"agentplane/internal/shell"
	"go.yaml.in/yaml/v3"
)

const maxAuthoringResultBytes = 6 << 20
const maxLayoutCoordinate = 100000

var ErrAuthoringInvalid = errors.New("visual edit requires a valid graph revision")
var ErrAuthoringOperation = errors.New("invalid visual edit operation")
var ErrAuthoringResultTooLarge = errors.New("authoring result exceeds 6 MiB")
var ErrAuthoringAlias = errors.New("visual edit targets YAML alias; edit YAML directly")
var ErrAuthoringReferences = errors.New("node has dependent references; confirm their removal")
var ErrAuthoringUnknownLayout = errors.New("unknown layout version; confirm replacement")

type AuthoringPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type AuthoringLayout struct {
	Version int                          `json:"version"`
	Source  string                       `json:"source"`
	Nodes   map[string]AuthoringPosition `json:"nodes"`
}

type AuthoringPort struct {
	Name     string          `json:"name"`
	Type     graph.ValueType `json:"type"`
	Required bool            `json:"required"`
}

type AuthoringNode struct {
	ID                 string          `json:"id"`
	Type               string          `json:"type"`
	Inputs             []AuthoringPort `json:"inputs"`
	Outputs            []AuthoringPort `json:"outputs"`
	LoopMaxAttempts    int             `json:"loop_max_attempts,omitempty"`
	NeedsConfiguration bool            `json:"needs_configuration"`
}

type AuthoringEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type AuthoringOutput struct {
	Name string `json:"name"`
	From string `json:"from"`
}

type AuthoringGraph struct {
	SchemaVersion      graph.SchemaVersion `json:"schema_version"`
	ID                 string              `json:"id"`
	Units              []string            `json:"units"`
	Inputs             []AuthoringPort     `json:"inputs"`
	Nodes              []AuthoringNode     `json:"nodes"`
	Edges              []AuthoringEdge     `json:"edges"`
	GraphInputBindings []AuthoringEdge     `json:"graph_input_bindings"`
	Outputs            []AuthoringOutput   `json:"outputs"`
}

type AuthoringInspection struct {
	Revision    uint64            `json:"revision"`
	Validity    DocumentValidity  `json:"validity"`
	Graph       *AuthoringGraph   `json:"graph,omitempty"`
	Layout      *AuthoringLayout  `json:"layout,omitempty"`
	Diagnostics DiagnosticSummary `json:"diagnostics"`
}

type AuthoringOperation struct {
	Kind                 string `json:"kind"`
	NodeID               string `json:"node_id"`
	NodeType             string `json:"node_type,omitempty"`
	RemoveReferences     bool   `json:"remove_references,omitempty"`
	ReplaceUnknownLayout bool   `json:"replace_unknown_layout,omitempty"`
	X                    int    `json:"x,omitempty"`
	Y                    int    `json:"y,omitempty"`
}

type AuthoringPreview struct {
	BaseRevision     uint64            `json:"base_revision"`
	TextBase64       string            `json:"text_base64"`
	Validity         DocumentValidity  `json:"validity"`
	Diagnostics      DiagnosticSummary `json:"diagnostics"`
	ReformatRequired bool              `json:"reformat_required"`
	ChangedPaths     []string          `json:"changed_paths"`
}

// Inspect projects one exact Go-validated revision without exposing node config.
func (s *DocumentStore) Inspect(documentID string, revision uint64) (AuthoringInspection, error) {
	doc, err := s.Get(documentID, revision)
	if err != nil {
		return AuthoringInspection{}, err
	}
	result := AuthoringInspection{Revision: revision, Validity: doc.Validity, Diagnostics: summarizeDiagnostics(doc.Diagnostics)}
	if doc.Validity == DocumentValid {
		definition, err := graph.Parse([]byte(doc.Text))
		if err != nil {
			return AuthoringInspection{}, err
		}
		projected := projectAuthoringGraph(definition)
		result.Graph = &projected
		layout := authoringLayout(definition)
		result.Layout = &layout
	}
	if err := authoringResultBound(result); err != nil {
		return AuthoringInspection{}, err
	}
	return result, nil
}

func projectAuthoringGraph(def *graph.Definition) AuthoringGraph {
	result := AuthoringGraph{SchemaVersion: def.SchemaVersion, ID: def.ID,
		Units: append([]string{}, def.Units...), Inputs: []AuthoringPort{}, Nodes: []AuthoringNode{},
		Edges: []AuthoringEdge{}, GraphInputBindings: []AuthoringEdge{}, Outputs: []AuthoringOutput{}}
	sort.Strings(result.Units)
	for _, name := range sortedMapKeys(def.Inputs) {
		p := def.Inputs[name]
		result.Inputs = append(result.Inputs, AuthoringPort{Name: name, Type: p.Type, Required: p.Required})
	}
	for _, id := range sortedMapKeys(def.Nodes) {
		n := def.Nodes[id]
		projected := AuthoringNode{ID: id, Type: n.Type, Inputs: []AuthoringPort{}, Outputs: []AuthoringPort{}, NeedsConfiguration: nodeNeedsConfiguration(id, n)}
		if n.Loop != nil {
			projected.LoopMaxAttempts = n.Loop.Attempts()
		}
		for _, name := range sortedMapKeys(n.Inputs) {
			p := n.Inputs[name]
			projected.Inputs = append(projected.Inputs, AuthoringPort{Name: name, Type: p.Type, Required: p.Required})
		}
		for _, name := range sortedMapKeys(n.Outputs) {
			p := n.Outputs[name]
			projected.Outputs = append(projected.Outputs, AuthoringPort{Name: name, Type: p.Type, Required: p.Required})
		}
		result.Nodes = append(result.Nodes, projected)
	}
	for _, e := range def.Edges {
		result.Edges = append(result.Edges, AuthoringEdge{From: string(e.From), To: string(e.To)})
	}
	sort.Slice(result.Edges, func(i, j int) bool {
		if result.Edges[i].From == result.Edges[j].From {
			return result.Edges[i].To < result.Edges[j].To
		}
		return result.Edges[i].From < result.Edges[j].From
	})
	for _, e := range def.GraphInputBindings {
		result.GraphInputBindings = append(result.GraphInputBindings, AuthoringEdge{From: string(e.From), To: string(e.To)})
	}
	sort.Slice(result.GraphInputBindings, func(i, j int) bool {
		if result.GraphInputBindings[i].From == result.GraphInputBindings[j].From {
			return result.GraphInputBindings[i].To < result.GraphInputBindings[j].To
		}
		return result.GraphInputBindings[i].From < result.GraphInputBindings[j].From
	})
	for _, name := range sortedMapKeys(def.Outputs) {
		result.Outputs = append(result.Outputs, AuthoringOutput{Name: name, From: string(def.Outputs[name].From)})
	}
	return result
}

func nodeNeedsConfiguration(id string, n graph.NodeDefinition) bool {
	switch n.Type {
	case "shell":
		return (shell.Executor{}).ValidateNodeConfig(id, n) != nil
	case "gate":
		return (gate.Executor{}).ValidateNodeConfig(id, n) != nil
	case "agent":
		return (agent.Executor{}).ValidateNodeConfig(id, n) != nil || n.Config["adapter"] != "codex"
	default:
		return true
	}
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func authoringLayout(def *graph.Definition) AuthoringLayout {
	fallback := AuthoringLayout{Version: 1, Source: "fallback", Nodes: fallbackPositions(def)}
	raw, ok := def.Metadata["agentplane_ui"]
	if !ok {
		return fallback
	}
	ui, ok := yamlStringMap(raw)
	if !ok {
		fallback.Source = "unsupported_version"
		return fallback
	}
	version, ok := yamlInt(ui["version"])
	if !ok || version != 1 {
		fallback.Source = "unsupported_version"
		return fallback
	}
	nodes, ok := yamlStringMap(ui["nodes"])
	if !ok {
		return fallback
	}
	fallback.Source = "stored"
	for id, rawPosition := range nodes {
		if _, known := def.Nodes[id]; !known {
			continue
		}
		position, ok := yamlStringMap(rawPosition)
		if !ok {
			continue
		}
		x, xok := yamlInt(position["x"])
		y, yok := yamlInt(position["y"])
		if xok && yok && x >= -maxLayoutCoordinate && x <= maxLayoutCoordinate && y >= -maxLayoutCoordinate && y <= maxLayoutCoordinate {
			fallback.Nodes[id] = AuthoringPosition{X: x, Y: y}
		}
	}
	return fallback
}

func yamlInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), int64(int(x)) == x
	default:
		return 0, false
	}
}

func yamlStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case graph.Metadata:
		return map[string]any(m), true
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for key, value := range m {
			name, ok := key.(string)
			if !ok {
				return nil, false
			}
			out[name] = value
		}
		return out, true
	default:
		return nil, false
	}
}

func fallbackPositions(def *graph.Definition) map[string]AuthoringPosition {
	const nodeStepX, nodeStepY = 200, 112 // canvas nodes are 180×88
	const maxColumns = (2*maxLayoutCoordinate)/nodeStepX + 1
	const maxRows = (2*maxLayoutCoordinate)/nodeStepY + 1
	ids := sortedMapKeys(def.Nodes)
	layers := make(map[string]int, len(ids))
	indegree := make(map[string]int, len(ids))
	adjacent := make(map[string][]string, len(ids))
	for _, id := range ids {
		indegree[id] = 0
	}
	for _, e := range def.Edges {
		from, to := strings.SplitN(string(e.From), ".", 2)[0], strings.SplitN(string(e.To), ".", 2)[0]
		adjacent[from] = append(adjacent[from], to)
		indegree[to]++
	}
	queue := make([]string, 0, len(ids))
	for _, id := range ids {
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	// Validation guarantees a DAG. Each node and edge is visited once; the
	// final per-layer Y order is sorted by ID independently of edge YAML order.
	for head := 0; head < len(queue); head++ {
		from := queue[head]
		for _, to := range adjacent[from] {
			if layers[to] < layers[from]+1 {
				layers[to] = layers[from] + 1
			}
			indegree[to]--
			if indegree[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	counts := map[int]int{}
	maxLayer, maxRowCount := 0, 0
	for _, id := range ids {
		if layers[id] > maxLayer {
			maxLayer = layers[id]
		}
		counts[layers[id]]++
		if counts[layers[id]] > maxRowCount {
			maxRowCount = counts[layers[id]]
		}
	}
	result := make(map[string]AuthoringPosition, len(ids))
	if maxLayer+1 > maxColumns || maxRowCount > maxRows {
		// Very long or very wide graphs use a bounded grid with enough space
		// for the rendered node boxes. Order remains layer then stable node ID.
		sort.Slice(ids, func(i, j int) bool {
			if layers[ids[i]] != layers[ids[j]] {
				return layers[ids[i]] < layers[ids[j]]
			}
			return ids[i] < ids[j]
		})
		for ordinal, id := range ids {
			result[id] = AuthoringPosition{X: -maxLayoutCoordinate + (ordinal%maxColumns)*nodeStepX,
				Y: -maxLayoutCoordinate + (ordinal/maxColumns)*nodeStepY}
		}
		return result
	}
	counts = map[int]int{}
	for _, id := range ids {
		layer := layers[id]
		result[id] = AuthoringPosition{X: fallbackAxis(layer, maxLayer+1, 260), Y: fallbackAxis(counts[layer], maxRowCount, 160)}
		counts[layer]++
	}
	return result
}

func fallbackAxis(index, count, preferredStep int) int {
	if count <= 1 {
		return 0
	}
	if int64(count-1)*int64(preferredStep) <= maxLayoutCoordinate {
		return index * preferredStep
	}
	return -maxLayoutCoordinate + int(int64(index)*int64(2*maxLayoutCoordinate)/int64(count-1))
}

// PreviewEdit never changes the store. The returned base revision must still be
// supplied to Update; Update is the compare-and-swap that accepts the preview.
func (s *DocumentStore) PreviewEdit(documentID string, revision uint64, operation AuthoringOperation) (AuthoringPreview, error) {
	doc, err := s.Get(documentID, revision)
	if err != nil {
		return AuthoringPreview{}, err
	}
	if doc.Validity != DocumentValid {
		return AuthoringPreview{}, ErrAuthoringInvalid
	}
	definition, err := graph.Parse([]byte(doc.Text))
	if err != nil {
		return AuthoringPreview{}, ErrAuthoringInvalid
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(doc.Text), &document); err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return AuthoringPreview{}, ErrAuthoringInvalid
	}
	root := document.Content[0]
	original, err := yaml.Marshal(&document)
	if err != nil {
		return AuthoringPreview{}, err
	}
	changed, err := applyAuthoringOperation(root, definition, operation)
	if err != nil {
		return AuthoringPreview{}, err
	}
	data, err := yaml.Marshal(&document)
	if err != nil {
		return AuthoringPreview{}, err
	}
	if len(data) > MaxDocumentBytes {
		return AuthoringPreview{}, ErrDocumentTooLarge
	}
	newDefinition, err := graph.Parse(data)
	if err != nil || graph.Validate(newDefinition).HasErrors() {
		return AuthoringPreview{}, ErrAuthoringOperation
	}
	result := AuthoringPreview{BaseRevision: revision, TextBase64: base64.StdEncoding.EncodeToString(data), Validity: DocumentValid,
		Diagnostics: summarizeDiagnostics(graph.Validate(newDefinition).Diagnostics), ReformatRequired: !bytes.Equal(original, []byte(doc.Text)), ChangedPaths: changed}
	if err := authoringResultBound(result); err != nil {
		return AuthoringPreview{}, err
	}
	return result, nil
}

func authoringResultBound(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > maxAuthoringResultBytes {
		return ErrAuthoringResultTooLarge
	}
	return nil
}

func applyAuthoringOperation(root *yaml.Node, def *graph.Definition, op AuthoringOperation) ([]string, error) {
	if !validGraphIdentifier(op.NodeID) {
		return nil, ErrAuthoringOperation
	}
	nodes := mappingValue(root, "nodes")
	if nodes == nil || nodes.Kind != yaml.MappingNode {
		return nil, ErrAuthoringAlias
	}
	if aliasesNode(root, nodes) || aliasesNode(root, root) {
		return nil, ErrAuthoringAlias
	}
	switch op.Kind {
	case "add_node":
		if op.NodeType != "shell" && op.NodeType != "agent" && op.NodeType != "gate" {
			return nil, ErrAuthoringOperation
		}
		if _, exists := def.Nodes[op.NodeID]; exists {
			return nil, ErrDocumentConflict
		}
		value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mappingSet(value, "type", scalar(op.NodeType))
		mappingSet(value, "inputs", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		mappingSet(value, "config", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		mappingSet(value, "outputs", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		mappingSet(nodes, op.NodeID, value)
		return []string{"nodes." + op.NodeID}, nil
	case "delete_node":
		if _, exists := def.Nodes[op.NodeID]; !exists {
			return nil, ErrAuthoringOperation
		}
		selected := mappingValue(nodes, op.NodeID)
		if selected == nil || aliasesSubtree(root, selected) {
			return nil, ErrAuthoringAlias
		}
		refs := nodeReferences(def, op.NodeID)
		if len(refs) > 0 && !op.RemoveReferences {
			return nil, ErrAuthoringReferences
		}
		mappingDelete(nodes, op.NodeID)
		changed := []string{"nodes." + op.NodeID}
		if len(refs) > 0 {
			if err := deleteNodeReferences(root, op.NodeID); err != nil {
				return nil, err
			}
			changed = append(changed, refs...)
		}
		if err := removeNodeLayout(root, op.NodeID, false); err != nil && !errors.Is(err, ErrAuthoringUnknownLayout) {
			return nil, err
		}
		if layoutHasNode(def, op.NodeID) {
			changed = append(changed, "metadata.agentplane_ui.nodes."+op.NodeID)
		}
		return changed, nil
	case "set_layout", "remove_layout":
		if _, exists := def.Nodes[op.NodeID]; !exists {
			return nil, ErrAuthoringOperation
		}
		if op.X < -maxLayoutCoordinate || op.X > maxLayoutCoordinate || op.Y < -maxLayoutCoordinate || op.Y > maxLayoutCoordinate {
			return nil, ErrAuthoringOperation
		}
		if op.Kind == "remove_layout" {
			if err := removeNodeLayout(root, op.NodeID, op.ReplaceUnknownLayout); err != nil {
				return nil, err
			}
		} else if err := setNodeLayout(root, op.NodeID, op.X, op.Y, op.ReplaceUnknownLayout); err != nil {
			return nil, err
		}
		return []string{"metadata.agentplane_ui.nodes." + op.NodeID}, nil
	default:
		return nil, ErrAuthoringOperation
	}
}

func nodeReferences(def *graph.Definition, id string) []string {
	var paths []string
	for i, unit := range def.Units {
		if unit == id {
			paths = append(paths, fmt.Sprintf("units[%d]", i))
		}
	}
	for i, e := range def.Edges {
		if hasNodePrefix(string(e.From), id) || hasNodePrefix(string(e.To), id) {
			paths = append(paths, fmt.Sprintf("edges[%d]", i))
		}
	}
	for i, b := range def.GraphInputBindings {
		if hasNodePrefix(string(b.To), id) {
			paths = append(paths, fmt.Sprintf("graph_input_bindings[%d]", i))
		}
	}
	for name, out := range def.Outputs {
		if hasNodePrefix(string(out.From), id) {
			paths = append(paths, "outputs."+name)
		}
	}
	sort.Strings(paths)
	return paths
}

func hasNodePrefix(reference, id string) bool { return strings.HasPrefix(reference, id+".") }

func aliasesNode(root, target *yaml.Node) bool {
	if target == nil {
		return false
	}
	var visit func(*yaml.Node) bool
	visit = func(n *yaml.Node) bool {
		if n.Kind == yaml.AliasNode && n.Alias == target {
			return true
		}
		for _, child := range n.Content {
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(root)
}

func aliasesSubtree(root, subtree *yaml.Node) bool {
	targets := map[*yaml.Node]bool{}
	var mark func(*yaml.Node)
	mark = func(n *yaml.Node) {
		targets[n] = true
		for _, child := range n.Content {
			mark(child)
		}
	}
	mark(subtree)
	var visit func(*yaml.Node) bool
	visit = func(n *yaml.Node) bool {
		if n.Kind == yaml.AliasNode && targets[n.Alias] {
			return true
		}
		for _, child := range n.Content {
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(root)
}

func layoutHasNode(def *graph.Definition, id string) bool {
	ui, ok := yamlStringMap(def.Metadata["agentplane_ui"])
	if !ok {
		return false
	}
	version, ok := yamlInt(ui["version"])
	if !ok || version != 1 {
		return false
	}
	nodes, ok := yamlStringMap(ui["nodes"])
	if !ok {
		return false
	}
	_, ok = nodes[id]
	return ok
}

func deleteNodeReferences(root *yaml.Node, id string) error {
	units := mappingValue(root, "units")
	if units != nil {
		if units.Kind != yaml.SequenceNode || aliasesNode(root, units) {
			return ErrAuthoringAlias
		}
		kept := units.Content[:0]
		for _, entry := range units.Content {
			if entry.Kind != yaml.ScalarNode {
				return ErrAuthoringAlias
			}
			if entry.Value != id {
				kept = append(kept, entry)
			} else if aliasesSubtree(root, entry) {
				return ErrAuthoringAlias
			}
		}
		if len(kept) == 0 {
			mappingDelete(root, "units")
		} else {
			units.Content = kept
		}
	}
	for _, key := range []string{"edges", "graph_input_bindings"} {
		seq := mappingValue(root, key)
		if seq == nil {
			continue
		}
		if seq.Kind != yaml.SequenceNode || aliasesNode(root, seq) {
			return ErrAuthoringAlias
		}
		kept := seq.Content[:0]
		for _, entry := range seq.Content {
			if entry.Kind != yaml.MappingNode {
				return ErrAuthoringAlias
			}
			from, to := mappingValue(entry, "from"), mappingValue(entry, "to")
			if from == nil || to == nil || from.Kind != yaml.ScalarNode || to.Kind != yaml.ScalarNode {
				return ErrAuthoringAlias
			}
			if !hasNodePrefix(from.Value, id) && !hasNodePrefix(to.Value, id) {
				kept = append(kept, entry)
			} else if aliasesSubtree(root, entry) {
				return ErrAuthoringAlias
			}
		}
		seq.Content = kept
	}
	outputs := mappingValue(root, "outputs")
	if outputs == nil || outputs.Kind != yaml.MappingNode || aliasesNode(root, outputs) {
		return ErrAuthoringAlias
	}
	for i := len(outputs.Content) - 2; i >= 0; i -= 2 {
		entry := outputs.Content[i+1]
		if entry.Kind != yaml.MappingNode {
			return ErrAuthoringAlias
		}
		from := mappingValue(entry, "from")
		if from == nil || from.Kind != yaml.ScalarNode {
			return ErrAuthoringAlias
		}
		if hasNodePrefix(from.Value, id) {
			if aliasesSubtree(root, entry) {
				return ErrAuthoringAlias
			}
			outputs.Content = append(outputs.Content[:i], outputs.Content[i+2:]...)
		}
	}
	return nil
}

func setNodeLayout(root *yaml.Node, id string, x, y int, replace bool) error {
	nodes, err := layoutNodes(root, replace)
	if err != nil {
		return err
	}
	if selected := mappingValue(nodes, id); selected != nil && aliasesSubtree(root, selected) {
		return ErrAuthoringAlias
	}
	position := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mappingSet(position, "x", integerScalar(x))
	mappingSet(position, "y", integerScalar(y))
	mappingSet(nodes, id, position)
	return nil
}

func removeNodeLayout(root *yaml.Node, id string, replace bool) error {
	metadata := mappingValue(root, "metadata")
	if metadata == nil {
		return nil
	}
	if metadata.Kind != yaml.MappingNode || aliasesNode(root, metadata) {
		return ErrAuthoringAlias
	}
	ui := mappingValue(metadata, "agentplane_ui")
	if ui == nil {
		return nil
	}
	if ui.Kind != yaml.MappingNode || aliasesNode(root, ui) {
		return ErrAuthoringAlias
	}
	version := mappingValue(ui, "version")
	if version == nil || version.Value != "1" {
		if !replace {
			return ErrAuthoringUnknownLayout
		}
		nodes, err := layoutNodes(root, true)
		if err != nil {
			return err
		}
		mappingDelete(nodes, id)
		return nil
	}
	nodes := mappingValue(ui, "nodes")
	if nodes == nil {
		return nil
	}
	if nodes.Kind != yaml.MappingNode || aliasesNode(root, nodes) {
		return ErrAuthoringAlias
	}
	if selected := mappingValue(nodes, id); selected != nil && aliasesSubtree(root, selected) {
		return ErrAuthoringAlias
	}
	mappingDelete(nodes, id)
	return nil
}

func layoutNodes(root *yaml.Node, replace bool) (*yaml.Node, error) {
	metadata := mappingValue(root, "metadata")
	if metadata == nil {
		metadata = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mappingSet(root, "metadata", metadata)
	}
	if metadata.Kind != yaml.MappingNode || aliasesNode(root, metadata) {
		return nil, ErrAuthoringAlias
	}
	ui := mappingValue(metadata, "agentplane_ui")
	if ui != nil {
		if ui.Kind != yaml.MappingNode || aliasesNode(root, ui) {
			return nil, ErrAuthoringAlias
		}
		version := mappingValue(ui, "version")
		if version == nil || version.Value != "1" {
			if !replace {
				return nil, ErrAuthoringUnknownLayout
			}
			if aliasesSubtree(root, ui) {
				return nil, ErrAuthoringAlias
			}
			ui = nil
		}
	}
	if ui == nil {
		ui = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mappingSet(ui, "version", integerScalar(1))
		mappingSet(ui, "nodes", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		mappingSet(metadata, "agentplane_ui", ui)
	}
	nodes := mappingValue(ui, "nodes")
	if nodes == nil || nodes.Kind != yaml.MappingNode || aliasesNode(root, nodes) {
		return nil, ErrAuthoringAlias
	}
	return nodes, nil
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func mappingSet(n *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = value
			return
		}
	}
	n.Content = append(n.Content, scalar(key), value)
}

func mappingDelete(n *yaml.Node, key string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
func integerScalar(v int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}
}
