package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

// fakeMaterial is a hand-written StoredMaterial: it offers fixed texts per
// location and reports the locations whose text satisfies the predicate.
type fakeMaterial map[string][]string

func (m fakeMaterial) Locations(holds func(string) bool) []string {
	var found []string
	for _, location := range []string{"inputs.token", "graph", "inputs"} {
		for _, text := range m[location] {
			if holds(text) {
				found = append(found, location)
				break
			}
		}
	}
	return found
}

func materialOptions(sink *persisted, material StoredMaterial) Options {
	options := sink.options(ghResolver())
	options.StoredMaterial = material
	return options
}

func TestPreflightRefusesResolvedValueHeldByStoredMaterial(t *testing.T) {
	material := fakeMaterial{"graph": {"x " + secretCanary + " y"}, "inputs.token": {secretCanary}}
	exec := newReceiver()
	sink := &persisted{}
	_, err := Run(context.Background(), ghDefinition(), nil, exec, materialOptions(sink, material))
	diagnostics := preflightDiagnostics(t, err)
	if got := strings.Join(diagnosticCodes(diagnostics), ","); got != "secret_value_in_graph,secret_value_in_input" {
		t.Fatalf("codes = %s", got)
	}
	for _, d := range diagnostics {
		if !strings.Contains(d.Message, `"gh_token"`) || d.Suggestion == "" {
			t.Errorf("diagnostic must name the secret and suggest a fix: %+v", d)
		}
	}
	if diagnostics[0].Location.Path != "graph" || diagnostics[1].Location.Path != "inputs.token" {
		t.Errorf("locations = %q, %q", diagnostics[0].Location.Path, diagnostics[1].Location.Path)
	}
	encoded, _ := json.Marshal(diagnostics)
	if strings.Contains(string(encoded)+err.Error(), secretCanary) || strings.Contains(string(encoded), "GITHUB_TOKEN") {
		t.Errorf("diagnostics leak the value or the variable: %s", encoded)
	}
	if sink.n.Load() != 0 || exec.runCount() != 0 {
		t.Errorf("events=%d runs=%d, want none before preflight failed", sink.n.Load(), exec.runCount())
	}
}

func TestPreflightKeepsAnInputKeyOutOfItsDiagnostic(t *testing.T) {
	_, err := Run(context.Background(), ghDefinition(), nil, newReceiver(), materialOptions(&persisted{}, fakeMaterial{"inputs": {secretCanary}}))
	diagnostics := preflightDiagnostics(t, err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "secret_value_in_input" || diagnostics[0].Location.Path != "inputs" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestPreflightAcceptsStoredMaterialWithoutTheValue(t *testing.T) {
	material := fakeMaterial{"graph": {"nothing here"}, "inputs.token": {"other"}}
	if _, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), materialOptions(&persisted{}, material)); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightWithoutSecretsNeverAsksStoredMaterial(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{})
	options := (&persisted{}).options(nil)
	options.StoredMaterial = fakeMaterial{"graph": {secretCanary}}
	if _, err := preflightRun(context.Background(), def, nil, newReceiver(), options); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightResolutionFailureComesBeforeTheStoredMaterialGuard(t *testing.T) {
	options := materialOptions(&persisted{}, fakeMaterial{"graph": {secretCanary}})
	options.SecretResolver = &fakeResolver{}
	_, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), options)
	if got := strings.Join(diagnosticCodes(preflightDiagnostics(t, err)), ","); got != "secret_unresolved" {
		t.Errorf("codes = %s, want only secret_unresolved", got)
	}
}
