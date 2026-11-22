package edit

import (
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
)

const secretsDocument = `schema_version: 0
id: secrets-edit
# Declared by reference only: the value stays in the runner environment.
secrets:
  # Used by the deploy step.
  gh_token:
    secret_ref: env/GITHUB_TOKEN # never a value
  api_key: {secret_ref: env/API_KEY}
inputs: {}
nodes:
  deploy:
    type: shell
    config:
      command: /bin/true
      secret_env:
        GH_TOKEN: gh_token # granted to this node only
edges: []
outputs: {}
`

func TestSecretsRoundTripKeepsFieldOrderAndComments(t *testing.T) {
	out := mustApply(t, secretsDocument, Op{Op: OpSetConfig, Node: "deploy", Path: []string{"command"}, Value: raw("/bin/echo")})
	removed, added := lineDiff(secretsDocument, out)
	if len(removed) != 1 || len(added) != 1 || !strings.Contains(removed[0], "/bin/true") || !strings.Contains(added[0], "/bin/echo") {
		t.Fatalf("only the edited line may change:\n-%q\n+%q", removed, added)
	}
	definition, err := yamlgraph.Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if definition.Secrets["gh_token"].SecretRef != "env/GITHUB_TOKEN" || definition.Secrets["api_key"].SecretRef != "env/API_KEY" {
		t.Fatalf("secrets lost: %+v", definition.Secrets)
	}
	if result := graph.Validate(definition); !result.Valid() {
		t.Fatalf("edited graph is invalid: %#v", result.Diagnostics)
	}
}

// F3.8 / ADR-0029: the editor is read-only for secret declarations and
// bindings. Before this decision (owner decision 9A) set_config could rewrite
// secret_env through a config path (#138); every operation that would write or
// drop a binding is now refused with CodeSecretsReadOnly and leaves the text
// unchanged. Other config fields stay editable.
func TestEditRefusesSecretBindings(t *testing.T) {
	const config = "nodes.deploy.config."
	const gateConfig = "nodes.deploy.loop.gate.config."
	cases := map[string]struct {
		op   Op
		path string
	}{
		"set a binding":       {Op{Op: OpSetConfig, Node: "deploy", Path: []string{"secret_env", "GH_TOKEN"}, Value: raw("api_key")}, config + "secret_env.GH_TOKEN"},
		"add a binding":       {Op{Op: OpSetConfig, Node: "deploy", Path: []string{"secret_env", "OTHER"}, Value: raw("api_key")}, config + "secret_env.OTHER"},
		"replace the map":     {Op{Op: OpSetConfig, Node: "deploy", Path: []string{"secret_env"}, Value: raw(map[string]any{"X": "api_key"})}, config + "secret_env"},
		"unset a binding":     {Op{Op: OpUnsetConfig, Node: "deploy", Path: []string{"secret_env", "GH_TOKEN"}}, config + "secret_env.GH_TOKEN"},
		"unset the map":       {Op{Op: OpUnsetConfig, Node: "deploy", Path: []string{"secret_env"}}, config + "secret_env"},
		"remove bound node":   {Op{Op: OpRemoveNode, ID: "deploy"}, config + "secret_env"},
		"set loop secret_env": {Op{Op: OpSetLoopConfig, Node: "deploy", Path: []string{"secret_env", "X"}, Value: raw("api_key")}, gateConfig + "secret_env.X"},
		"unset loop binding":  {Op{Op: OpUnsetLoopConfig, Node: "deploy", Path: []string{"secret_env"}}, gateConfig + "secret_env"},
		"set_loop gate":       {Op{Op: OpSetLoop, Node: "deploy", Gate: &LoopGate{Config: raw(map[string]any{"check_id": "c", "kind": "exit_code", "secret_env": map[string]any{"X": "api_key"}})}}, gateConfig + "secret_env"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := applyErr(t, secretsDocument, c.op)
			if e.Code != CodeSecretsReadOnly || e.OpIndex != 0 || e.Node != "deploy" || e.Path != c.path {
				t.Fatalf("want %s at operation 0 for node deploy path %q, got %+v", CodeSecretsReadOnly, c.path, e)
			}
		})
	}
}

func TestEditRefusalKeepsTheWholeBatchAtomic(t *testing.T) {
	_, err := Apply([]byte(secretsDocument), []Op{
		{Op: OpSetConfig, Node: "deploy", Path: []string{"command"}, Value: raw("/bin/echo")},
		{Op: OpSetConfig, Node: "deploy", Path: []string{"secret_env", "GH_TOKEN"}, Value: raw("api_key")},
	})
	e, ok := err.(*Error)
	if !ok || e.Code != CodeSecretsReadOnly || e.OpIndex != 1 {
		t.Fatalf("want secrets_read_only at operation 1, got %v", err)
	}
}

func TestEditStillChangesOtherConfigAndUnboundNodes(t *testing.T) {
	out := mustApply(t, secretsDocument,
		Op{Op: OpAddNode, ID: "plain", Type: "shell"},
		Op{Op: OpSetConfig, Node: "plain", Path: []string{"command"}, Value: raw("/bin/true")},
		Op{Op: OpSetConfig, Node: "deploy", Path: []string{"env", "A"}, Value: raw("b")},
		Op{Op: OpRenameNode, ID: "deploy", NewID: "ship"},
		Op{Op: OpRemoveNode, ID: "plain"},
	)
	for _, want := range []string{"secret_ref: env/GITHUB_TOKEN", "GH_TOKEN: gh_token", "# granted to this node only", "ship:"} {
		if !strings.Contains(out, want) {
			t.Errorf("edited text lost %q:\n%s", want, out)
		}
	}
}
