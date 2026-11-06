package gate

import (
	"testing"

	"agentplane/internal/graph"
)

func TestFixedCommandValuesSurviveGraphYAMLRoundTrip(t *testing.T) {
	for _, value := range []string{"/usr/bin/diff", "agentplane.diff"} {
		t.Run(value, func(t *testing.T) {
			n := diffNode(t)
			n.Config["command"] = value
			definition := &graph.Definition{
				SchemaVersion: graph.SchemaVersionV0,
				ID:            "diff-round-trip",
				Inputs:        map[string]graph.InputDefinition{},
				Nodes:         map[string]graph.NodeDefinition{"check": n},
				Edges:         []graph.EdgeDefinition{},
				Outputs:       map[string]graph.OutputDefinition{},
			}
			encoded, err := graph.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := graph.Parse(encoded)
			if err != nil {
				t.Fatal(err)
			}
			got := parsed.Nodes["check"]
			if got.Config["command"] != value {
				t.Fatalf("round trip command = %v, want %q", got.Config["command"], value)
			}
			if _, err := parse(got); err != nil {
				t.Fatalf("round trip gate rejected: %v", err)
			}
		})
	}
}
