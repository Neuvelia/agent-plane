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

func TestSecretEnvIsEditableThroughConfigPaths(t *testing.T) {
	out := mustApply(t, secretsDocument, Op{Op: OpSetConfig, Node: "deploy", Path: []string{"secret_env", "GH_TOKEN"}, Value: raw("api_key")})
	definition, err := yamlgraph.Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	assigned, _ := definition.Nodes["deploy"].Config["secret_env"].(graph.NodeConfig)
	if assigned["GH_TOKEN"] != "api_key" || !strings.Contains(out, "# granted to this node only") {
		t.Fatalf("secret_env edit lost or dropped its comment:\n%s", out)
	}
}
