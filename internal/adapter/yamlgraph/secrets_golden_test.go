package yamlgraph_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
)

// Pinned on main before the additive secrets fields existed. A graph without
// them must keep its normalized bytes and its semantic hash exactly.
var secretsAbsentGolden = map[string]struct{ Marshal, Semantic string }{
	"demo-vertical.yaml":  {"0b1e58205741205b9565759b21ac3d8a2bb9b71791f8271c7c0349c4d8bbb00e", "sha256-v0:27fe87371c77b373a16f0ff07310c3812ede2ed3d9e4e8ab8b7f13a78a885c08"},
	"hello-parallel.yaml": {"3ef09602ec5f741f85b944f0de6d42196dbffdebf2e6422e3706eb5976844a58", "sha256-v0:397ec99bf8ba80835955967cd7470b4227f56a8cdb46a7ed049f012034529010"},
	"phase0-cli.yaml":     {"ec805370a519e9af0aab0a06a39b765101a5416afd43a16a87f23151bc6a7661", "sha256-v0:4c2397b3eb18f5efb48c7962f26eb5046399cbbee8f55eb2e0f55edf1fa1e4bb"},
}

func TestGraphWithoutSecretsOrPolicyIsByteIdentical(t *testing.T) {
	paths, err := filepath.Glob("../../../.agents/graphs/*.yaml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no example graphs: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		definition, err := yamlgraph.Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		marshaled, err := yamlgraph.Marshal(definition)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		semantic, err := yamlgraph.SemanticVersion(definition)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		got := struct{ Marshal, Semantic string }{fmt.Sprintf("%x", sha256.Sum256(marshaled)), semantic}
		name := filepath.Base(path)
		if want, ok := secretsAbsentGolden[name]; !ok || want != got {
			t.Errorf("%s: normalized bytes or semantic hash changed: got %+v, want %+v", name, got, want)
		}
	}
}
