package desktop

import (
	"strings"
	"testing"

	"agentplane/internal/graph"
)

func TestLaunchRedactionsCoverExplicitStringInputsAndBoundMemory(t *testing.T) {
	environment, err := CaptureLaunchEnvironment(&graph.Definition{Nodes: map[string]graph.NodeDefinition{}})
	if err != nil {
		t.Fatal(err)
	}
	values, err := launchRedactions(environment, map[string]graph.Value{"secret": graph.NewStringValue("SENTINEL")})
	if err != nil || len(values) != 1 || values[0] != "SENTINEL" {
		t.Fatalf("redactions = %#v, %v", values, err)
	}
	if _, err := launchRedactions(environment, map[string]graph.Value{"oversize": graph.NewStringValue(strings.Repeat("x", maxLaunchRedactionBytes+1))}); err == nil {
		t.Fatal("oversized graph input entered runtime without redaction")
	}
	var allASCII []byte
	for i := 0; i < 128; i++ {
		allASCII = append(allASCII, byte(i))
	}
	if _, err := launchRedactions(environment, map[string]graph.Value{"no_marker": graph.NewStringValue(string(allASCII))}); err == nil {
		t.Fatal("redactions with no UTF-8-safe marker entered runtime")
	}
}
