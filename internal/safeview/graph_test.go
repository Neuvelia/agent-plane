package safeview

import (
	"bytes"
	"strings"
	"testing"

	"agentplane/internal/graph"
)

func TestProjectGraphRedactsSemanticConfigWithoutMutatingDefinition(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0, ID: "build", Name: "private plan",
		Metadata: graph.Metadata{"note": "private", "api_key": "private"},
		Inputs:   map[string]graph.InputDefinition{"message": {Type: graph.ValueTypeString}},
		Nodes: map[string]graph.NodeDefinition{"one": {
			Type: "shell", Config: graph.NodeConfig{"command": "printf private", "env": map[string]any{"PUBLIC": "private", "access_token": "private"}},
			Outputs: map[string]graph.PortDefinition{"out": {Type: graph.ValueTypeString}},
		}},
		Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{"result": {From: "one.out"}},
	}
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	projection, complete, err := projector.ProjectGraph(definition, "sha256-v0:abc")
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Fatal("redacted graph was reported complete")
	}
	data := projection.Bytes()
	if bytes.Contains(data, []byte("private")) || bytes.Contains(data, []byte("access_token")) || bytes.Contains(data, []byte("api_key")) {
		t.Fatalf("private graph projection: %s", data)
	}
	if !bytes.Contains(data, []byte("one.out")) || !bytes.Contains(data, []byte("PUBLIC")) {
		t.Fatalf("missing safe graph structure: %s", data)
	}
	if definition.Nodes["one"].Config["command"] != "printf private" {
		t.Fatal("definition was mutated")
	}
}

func TestProjectGraphRejectsUnsafeRequiredIdentityAndBounds(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	base := &graph.Definition{ID: "build", Nodes: map[string]graph.NodeDefinition{"private-node": {Type: "shell"}}}
	if _, _, err := projector.ProjectGraph(base, "sha256-v0:abc"); err == nil {
		t.Fatal("required node identity was not rejected")
	}
	deep := any("leaf")
	for range 70 {
		deep = []any{deep}
	}
	base.Nodes = map[string]graph.NodeDefinition{"one": {Type: "shell", Config: graph.NodeConfig{"deep": deep}}}
	if _, _, err := projector.ProjectGraph(base, "sha256-v0:abc"); err == nil {
		t.Fatal("deep config was not rejected")
	}
	base.Nodes["one"] = graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"huge": strings.Repeat("x", 9<<20)}}
	if _, _, err := projector.ProjectGraph(base, "sha256-v0:abc"); err == nil {
		t.Fatal("oversized config was not rejected")
	}
}

func TestProjectGraphCanonicalAcrossMapInsertionOrder(t *testing.T) {
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	base := &graph.Definition{ID: "build", Inputs: map[string]graph.InputDefinition{"a": {Type: graph.ValueTypeString}, "b": {Type: graph.ValueTypeBoolean}}, Nodes: map[string]graph.NodeDefinition{"x": {Type: "shell", Config: graph.NodeConfig{"a": 1, "b": true}}}}
	left, complete, err := projector.ProjectGraph(base, "sha256-v0:abc")
	if err != nil || !complete {
		t.Fatalf("left complete=%v err=%v", complete, err)
	}
	base.Inputs = map[string]graph.InputDefinition{"b": {Type: graph.ValueTypeBoolean}, "a": {Type: graph.ValueTypeString}}
	base.Nodes = map[string]graph.NodeDefinition{"x": {Type: "shell", Config: graph.NodeConfig{"b": true, "a": 1}}}
	right, complete, err := projector.ProjectGraph(base, "sha256-v0:abc")
	if err != nil || !complete {
		t.Fatalf("right complete=%v err=%v", complete, err)
	}
	if !bytes.Equal(left.Bytes(), right.Bytes()) {
		t.Fatalf("nondeterministic graph projection:\n%s\n%s", left.Bytes(), right.Bytes())
	}
}

func TestProjectGraphParsedYAMLEscapesAndNestedCollections(t *testing.T) {
	definition, err := graph.Parse([]byte(`schema_version: 0
id: build
inputs: {}
nodes:
  one:
    type: shell
    config:
      command: "echo pri\u0076ate"
      env:
        PUBLIC: "pri\u0076ate"
      tags: [one, two]
edges: []
outputs: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	projection, complete, err := projector.ProjectGraph(definition, "sha256-v0:abc")
	if err != nil {
		t.Fatal(err)
	}
	if complete || bytes.Contains(projection.Bytes(), []byte("private")) || !bytes.Contains(projection.Bytes(), []byte("PUBLIC")) {
		t.Fatalf("projection=%s complete=%v", projection.Bytes(), complete)
	}
}

func TestProjectGraphOmitsNumericValuesUsingEncodedJSONSpelling(t *testing.T) {
	projector, err := NewProjector([]string{"1000000", "3"})
	if err != nil {
		t.Fatal(err)
	}
	definition := &graph.Definition{ID: "build", Nodes: map[string]graph.NodeDefinition{"one": {
		Type: "agent", Config: graph.NodeConfig{"number": float64(1e6)},
		Loop: &graph.LoopDefinition{MaxAttempts: 3},
	}}}
	projection, complete, err := projector.ProjectGraph(definition, "sha256-v0:abc")
	if err != nil {
		t.Fatal(err)
	}
	if complete || bytes.Contains(projection.Bytes(), []byte("1000000")) || bytes.Contains(projection.Bytes(), []byte(`,3,`)) {
		t.Fatalf("numeric secret persisted: %s", projection.Bytes())
	}
}

func TestProjectGraphRejectsPointerCycle(t *testing.T) {
	var cycle any
	cycle = &cycle
	definition := &graph.Definition{ID: "build", Metadata: graph.Metadata{"cycle": cycle}}
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := projector.ProjectGraph(definition, "sha256-v0:abc"); err == nil {
		t.Fatal("pointer cycle was accepted")
	}
}

func TestProjectGraphMarksInvalidOptionalTextIncomplete(t *testing.T) {
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := &graph.Definition{ID: "build", Name: string([]byte{0xff}), Nodes: map[string]graph.NodeDefinition{"one": {Type: string([]byte{0xfe})}}}
	projection, complete, err := projector.ProjectGraph(definition, "sha256-v0:abc")
	if err != nil || complete {
		t.Fatalf("projection=%s complete=%v err=%v", projection.Bytes(), complete, err)
	}
	if bytes.Contains(projection.Bytes(), []byte{0xef, 0xbf, 0xbd}) {
		t.Fatalf("replacement rune disguised invalid text: %s", projection.Bytes())
	}
}

func TestProjectGraphHandlesParsedYAMLTimestamp(t *testing.T) {
	definition, err := graph.Parse([]byte(`schema_version: 0
id: build
metadata:
  scheduled: 2025-01-01
inputs: {}
nodes:
  one:
    type: shell
    config:
      scheduled: 2025-01-01
edges: []
outputs: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	projection, complete, err := projector.ProjectGraph(definition, "sha256-v0:abc")
	if err != nil {
		t.Fatal(err)
	}
	if complete || !bytes.Contains(projection.Bytes(), []byte("2025-01-01")) {
		t.Fatalf("timestamp projection=%s complete=%v", projection.Bytes(), complete)
	}
}

func TestProjectGraphOmitsParsedYAMLNonStringMapKey(t *testing.T) {
	definition, err := graph.Parse([]byte(`schema_version: 0
id: build
metadata:
  visible: retained
  nested: {1: private}
inputs: {}
nodes: {}
edges: []
outputs: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	projection, complete, err := projector.ProjectGraph(definition, "sha256-v0:abc")
	if err != nil {
		t.Fatal(err)
	}
	if complete || bytes.Contains(projection.Bytes(), []byte("private")) || bytes.Contains(projection.Bytes(), []byte("nested")) || !bytes.Contains(projection.Bytes(), []byte("retained")) {
		t.Fatalf("projection=%s complete=%v", projection.Bytes(), complete)
	}
}
