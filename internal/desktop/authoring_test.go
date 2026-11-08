package desktop

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/graph"
)

func TestAuthoringInspectInvalidAndDeterministicFallback(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	view, err := store.Inspect(doc.DocumentID, doc.Revision)
	if err != nil || view.Graph == nil || view.Graph.ID != "local" || view.Layout == nil || view.Layout.Source != "fallback" {
		t.Fatalf("inspect = %#v, %v", view, err)
	}
	if _, err := store.Inspect(doc.DocumentID, doc.Revision+1); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("stale inspect = %v", err)
	}
	invalid, err := store.Update(doc.DocumentID, doc.Revision, "invalid: [")
	if err != nil {
		t.Fatal(err)
	}
	view, err = store.Inspect(doc.DocumentID, invalid.Revision)
	if err != nil || view.Graph != nil || view.Validity != DocumentInvalid || len(view.Diagnostics.Diagnostics) == 0 {
		t.Fatalf("invalid inspect = %#v, %v", view, err)
	}
}

func TestAuthoringPreviewAddLayoutDeleteAndSemanticVersion(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.PreviewEdit(doc.DocumentID, doc.Revision, AuthoringOperation{Kind: "add_node", NodeID: "hello", NodeType: "shell"})
	if err != nil || added.Validity != DocumentValid || !strings.Contains(decodePreview(t, added), "hello:") {
		t.Fatalf("add preview = %#v, %v", added, err)
	}
	if retained, _ := store.Get(doc.DocumentID, 1); retained.Text != doc.Text {
		t.Fatal("preview mutated document")
	}
	updated, err := store.Update(doc.DocumentID, 1, decodePreview(t, added))
	if err != nil || updated.SemanticVersion == doc.SemanticVersion {
		t.Fatalf("added version = %#v, %v", updated, err)
	}
	addedVersion := updated.SemanticVersion
	layout, err := store.PreviewEdit(updated.DocumentID, updated.Revision, AuthoringOperation{Kind: "set_layout", NodeID: "hello", X: 100000, Y: -100000})
	if err != nil || !strings.Contains(decodePreview(t, layout), "agentplane_ui:") {
		t.Fatalf("layout preview = %#v, %v", layout, err)
	}
	updated, err = store.Update(updated.DocumentID, updated.Revision, decodePreview(t, layout))
	if err != nil || updated.SemanticVersion != addedVersion {
		t.Fatalf("layout changed version = %#v, %v", updated, err)
	}
	view, err := store.Inspect(updated.DocumentID, updated.Revision)
	if err != nil || view.Layout == nil || view.Layout.Source != "stored" || view.Layout.Nodes["hello"].X != 100000 {
		t.Fatalf("stored layout = %#v, %v", view.Layout, err)
	}
	if _, err := store.PreviewEdit(updated.DocumentID, updated.Revision, AuthoringOperation{Kind: "set_layout", NodeID: "hello", X: 100001}); err == nil {
		t.Fatal("out-of-range layout accepted")
	}
	deleted, err := store.PreviewEdit(updated.DocumentID, updated.Revision, AuthoringOperation{Kind: "delete_node", NodeID: "hello", RemoveReferences: false})
	if err != nil || strings.Contains(decodePreview(t, deleted), "hello:") {
		t.Fatalf("delete preview = %#v, %v", deleted, err)
	}
	foundLayoutPath := false
	for _, path := range deleted.ChangedPaths {
		if path == "metadata.agentplane_ui.nodes.hello" {
			foundLayoutPath = true
		}
	}
	if !foundLayoutPath {
		t.Fatalf("missing layout deletion in changed paths: %#v", deleted.ChangedPaths)
	}
}

func TestAuthoringPreviewPreservesUnknownConfigAndComments(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := strings.Replace(validGraphText, "nodes: {}", "nodes:\n  one: # node note\n    type: shell\n    config:\n      custom: &custom [a, b]\n      other: *custom", 1)
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated, err)
	}
	preview, err := store.PreviewEdit(doc.DocumentID, 2, AuthoringOperation{Kind: "add_node", NodeID: "two", NodeType: "gate"})
	if err != nil {
		t.Fatal(err)
	}
	got := decodePreview(t, preview)
	if !strings.Contains(got, "custom: &custom") || !strings.Contains(got, "other: *custom") || !strings.Contains(got, "node note") {
		t.Fatalf("lost authored YAML: %s", got)
	}
	if _, err := store.PreviewEdit(doc.DocumentID, 1, AuthoringOperation{Kind: "add_node", NodeID: "three", NodeType: "shell"}); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("stale preview = %v", err)
	}
}

func TestAuthoringDeleteRequiresExplicitReferenceRemoval(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := `schema_version: 0
id: bound
inputs:
  source: {type: string}
nodes:
  first:
    type: shell
    outputs: {stdout: {type: string}}
  second:
    type: shell
    inputs: {value: {type: string}}
edges:
  - {from: first.outputs.stdout, to: second.inputs.value}
graph_input_bindings:
  - {from: inputs.source, to: second.inputs.value}
outputs:
  value: {from: first.outputs.stdout}
`
	// A node input cannot have both an edge and a graph input provider. Keep
	// one binding aimed at first, then explicitly verify all reference classes.
	text = strings.Replace(text, "to: second.inputs.value}\noutputs:", "to: first.inputs.source}\noutputs:", 1)
	text = strings.Replace(text, "    outputs: {stdout: {type: string}}", "    inputs: {source: {type: string}}\n    outputs: {stdout: {type: string}}", 1)
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated.Diagnostics, err)
	}
	if _, err := store.PreviewEdit(doc.DocumentID, 2, AuthoringOperation{Kind: "delete_node", NodeID: "first"}); !errors.Is(err, ErrAuthoringReferences) {
		t.Fatalf("missing confirmation = %v", err)
	}
	preview, err := store.PreviewEdit(doc.DocumentID, 2, AuthoringOperation{Kind: "delete_node", NodeID: "first", RemoveReferences: true})
	if err != nil {
		t.Fatal(err)
	}
	got := decodePreview(t, preview)
	if strings.Contains(got, "first.outputs") || strings.Contains(got, "first.inputs") || strings.Contains(got, "  first:") {
		t.Fatalf("references remain: %s", got)
	}
}

func TestAuthoringUnknownLayoutRequiresReplacementAndInvalidDraftFreezes(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := strings.Replace(validGraphText, "nodes: {}", "nodes:\n  first: {type: shell}", 1)
	text = strings.Replace(text, "inputs: {}", "metadata:\n  agentplane_ui:\n    version: 9\n    nodes: {first: {x: 7, y: 9}}\ninputs: {}", 1)
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated.Diagnostics, err)
	}
	view, err := store.Inspect(doc.DocumentID, 2)
	if err != nil || view.Layout.Source != "unsupported_version" {
		t.Fatalf("layout = %#v, %v", view.Layout, err)
	}
	op := AuthoringOperation{Kind: "set_layout", NodeID: "first", X: 1, Y: 2}
	if _, err := store.PreviewEdit(doc.DocumentID, 2, op); !errors.Is(err, ErrAuthoringUnknownLayout) {
		t.Fatalf("unknown layout = %v", err)
	}
	op.ReplaceUnknownLayout = true
	preview, err := store.PreviewEdit(doc.DocumentID, 2, op)
	if err != nil || !strings.Contains(decodePreview(t, preview), "version: 1") {
		t.Fatalf("replacement = %#v, %v", preview, err)
	}
	invalid, err := store.Update(doc.DocumentID, 2, "schema_version: 0\nid: duplicate\nnodes: {}\nnodes: {}\n")
	if err != nil || invalid.Validity != DocumentInvalid {
		t.Fatalf("invalid = %#v, %v", invalid, err)
	}
	if _, err := store.PreviewEdit(doc.DocumentID, 3, AuthoringOperation{Kind: "add_node", NodeID: "next", NodeType: "shell"}); !errors.Is(err, ErrAuthoringInvalid) {
		t.Fatalf("invalid draft mutation = %v", err)
	}
}

func TestAuthoringDTOOmitsConfigAndBoundsResults(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := strings.Replace(validGraphText, "nodes: {}", "nodes:\n  first:\n    type: shell\n    config: {secret_value: should-not-appear}", 1)
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated.Diagnostics, err)
	}
	view, err := store.Inspect(doc.DocumentID, 2)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(view)
	if err != nil || strings.Contains(string(data), "should-not-appear") || strings.Contains(string(data), "secret_value") {
		t.Fatalf("unsafe projection = %s, %v", data, err)
	}
	if err := authoringResultBound(strings.Repeat("x", maxAuthoringResultBytes)); !errors.Is(err, ErrAuthoringResultTooLarge) {
		t.Fatalf("oversized result = %v", err)
	}
}

func TestAuthoringRejectsMutationOfAliasedNodesMapping(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := `schema_version: 0
id: aliased
metadata:
  template: &template {}
inputs: {}
nodes: *template
edges: []
outputs: {}
`
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated.Diagnostics, err)
	}
	if _, err := store.PreviewEdit(doc.DocumentID, 2, AuthoringOperation{Kind: "add_node", NodeID: "first", NodeType: "shell"}); !errors.Is(err, ErrAuthoringAlias) {
		t.Fatalf("aliased nodes = %v", err)
	}
}

func TestAuthoringFallbackTopologyAndReformatNotice(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := `schema_version: 0
id: layered
inputs: {}
nodes:
  c: {type: shell}
  b:
    type: shell
    inputs: {value: {type: string}}
  a:
    type: shell
    outputs: {stdout: {type: string}}
edges:
  - {from: a.outputs.stdout, to: b.inputs.value}
outputs: {}
`
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated.Diagnostics, err)
	}
	first, err := store.Inspect(doc.DocumentID, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Inspect(doc.DocumentID, 2)
	if err != nil || first.Layout.Nodes["a"] != second.Layout.Nodes["a"] || first.Layout.Nodes["b"].X <= first.Layout.Nodes["a"].X {
		t.Fatalf("unstable fallback = %#v, %#v, %v", first.Layout, second.Layout, err)
	}
	if first.Graph.Nodes[0].ID != "a" || first.Graph.Nodes[1].ID != "b" || first.Graph.Nodes[2].ID != "c" {
		t.Fatalf("unsorted nodes = %#v", first.Graph.Nodes)
	}
	preview, err := store.PreviewEdit(doc.DocumentID, 2, AuthoringOperation{Kind: "set_layout", NodeID: "a", X: 0, Y: 0})
	if err != nil || !preview.ReformatRequired {
		t.Fatalf("reformat flag = %#v, %v", preview, err)
	}
}

func TestAuthoringRejectsAnchoredNodesReferencedByAlias(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := `schema_version: 0
id: anchored
inputs: {}
nodes: &shared_nodes {}
edges: []
outputs: {}
metadata:
  snapshot: *shared_nodes
`
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated.Diagnostics, err)
	}
	if _, err := store.PreviewEdit(doc.DocumentID, 2, AuthoringOperation{Kind: "add_node", NodeID: "new", NodeType: "shell"}); !errors.Is(err, ErrAuthoringAlias) {
		t.Fatalf("anchored nodes silently mutated alias = %v", err)
	}
}

func TestAuthoringConfigurationUsesNodeValidation(t *testing.T) {
	store, _ := documentFixture(t)
	doc, _ := store.Open("project-1", ".agents/graphs/local.yaml")
	text := strings.Replace(validGraphText, "nodes: {}", `nodes:
  shell_ready: {type: shell, config: {command: printf}}
  gate_missing: {type: gate, config: {kind: json_predicate}}
  agent_missing: {type: agent, config: {adapter: codex}}
  shell_missing: {type: shell, config: {args: []}}`, 1)
	updated, err := store.Update(doc.DocumentID, 1, text)
	if err != nil || updated.Validity != DocumentValid {
		t.Fatalf("fixture = %#v, %v", updated.Diagnostics, err)
	}
	view, err := store.Inspect(doc.DocumentID, 2)
	if err != nil {
		t.Fatal(err)
	}
	needs := map[string]bool{}
	for _, node := range view.Graph.Nodes {
		needs[node.ID] = node.NeedsConfiguration
	}
	if needs["shell_ready"] || !needs["gate_missing"] || !needs["agent_missing"] || !needs["shell_missing"] {
		t.Fatalf("needs configuration = %#v", needs)
	}
}

func TestAuthoringFallbackCoordinatesStayInV1Range(t *testing.T) {
	def := &graph.Definition{Nodes: map[string]graph.NodeDefinition{}, Edges: []graph.EdgeDefinition{}}
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("n%d", i)
		def.Nodes[id] = graph.NodeDefinition{Type: "shell"}
		if i > 0 {
			def.Edges = append(def.Edges, graph.EdgeDefinition{From: graph.NodeOutputReference(fmt.Sprintf("n%d.outputs.out", i-1)), To: graph.NodeInputReference(id + ".inputs.in")})
		}
	}
	first := fallbackPositions(def)
	second := fallbackPositions(def)
	if first["n499"] != second["n499"] || first["n499"].X > maxLayoutCoordinate || first["n499"].X < 0 {
		t.Fatalf("fallback exceeded v1 range or changed: %#v %#v", first["n499"], second["n499"])
	}
	seen := map[AuthoringPosition]bool{}
	for i := 0; i < 500; i++ {
		position := first[fmt.Sprintf("n%d", i)]
		if seen[position] {
			t.Fatalf("chain node %d overlaps at %#v", i, position)
		}
		seen[position] = true
		if i > 0 {
			previous := first[fmt.Sprintf("n%d", i-1)]
			if position.X-previous.X < 180 || position.Y != previous.Y {
				t.Fatalf("chain nodes not selectable: %d %#v after %#v", i, position, previous)
			}
		}
	}
	wide := &graph.Definition{Nodes: map[string]graph.NodeDefinition{}, Edges: []graph.EdgeDefinition{}}
	for i := 0; i < 700; i++ {
		wide.Nodes[fmt.Sprintf("w%04d", i)] = graph.NodeDefinition{Type: "shell"}
	}
	positions := fallbackPositions(wide)
	seen = map[AuthoringPosition]bool{}
	for i := 0; i < 700; i++ {
		position := positions[fmt.Sprintf("w%04d", i)]
		if seen[position] {
			t.Fatalf("wide node %d overlaps at %#v", i, position)
		}
		seen[position] = true
		if i > 0 {
			previous := positions[fmt.Sprintf("w%04d", i-1)]
			if position.Y-previous.Y < 88 || position.X != previous.X {
				t.Fatalf("wide nodes not selectable: %d %#v after %#v", i, position, previous)
			}
		}
	}
}

func TestAuthoringFallbackGridKeepsExtremeGraphsDistinct(t *testing.T) {
	for _, shape := range []string{"chain", "wide"} {
		def := &graph.Definition{Nodes: map[string]graph.NodeDefinition{}, Edges: []graph.EdgeDefinition{}}
		for i := 0; i < 1900; i++ {
			id := fmt.Sprintf("n%04d", i)
			def.Nodes[id] = graph.NodeDefinition{Type: "shell"}
			if shape == "chain" && i > 0 {
				def.Edges = append(def.Edges, graph.EdgeDefinition{From: graph.NodeOutputReference(fmt.Sprintf("n%04d.outputs.out", i-1)), To: graph.NodeInputReference(id + ".inputs.in")})
			}
		}
		positions := fallbackPositions(def)
		seen := map[AuthoringPosition]bool{}
		for _, position := range positions {
			if position.X < -maxLayoutCoordinate || position.X > maxLayoutCoordinate || position.Y < -maxLayoutCoordinate || position.Y > maxLayoutCoordinate || seen[position] {
				t.Fatalf("%s invalid grid position %#v", shape, position)
			}
			seen[position] = true
		}
		if len(seen) != 1900 {
			t.Fatalf("%s positions = %d", shape, len(seen))
		}
	}
}

func decodePreview(t *testing.T, p AuthoringPreview) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(p.TextBase64)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
