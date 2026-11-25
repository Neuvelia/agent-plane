package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const secretBoundGraph = `schema_version: 0
id: bound
secrets:
  gh_token:
    secret_ref: env/GITHUB_TOKEN
inputs: {}
nodes:
  deploy:
    type: shell
    config:
      command: "true"
      secret_env:
        GH_TOKEN: gh_token
edges: []
outputs: {}
`

// F3.8 / ADR-0029 (owner decision 9A): POST /graphs/edit refuses to change
// secret bindings with a stable code and leaves the held text untouched.
// PUT /graphs/save stays a plain text write (ADR-0021); see its test below.
func TestEditDocRefusesSecretBindingsAsReadOnly(t *testing.T) {
	_, ts, dir := docServer(t)
	path := writeGraph(t, dir, "bound.yaml", secretBoundGraph)
	g := grantFor(t, ts, path, "open")
	_, doc := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	hash := doc["hash"].(string)

	ops := json.RawMessage(`[{"op":"set_config","node":"deploy","path":["secret_env","GH_TOKEN"],"value":"other"}]`)
	status, out := call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": hash, "ops_version": 1, "ops": ops})
	if status != 422 || errCode(out) != "secrets_read_only" {
		t.Fatalf("secret_env edit: %d %v", status, out)
	}
	detail, _ := out["detail"].(map[string]any)
	if detail["node"] != "deploy" || !strings.Contains(detail["path"].(string), "secret_env") {
		t.Errorf("refusal must name the node and path: %v", detail)
	}
	if disk, _ := os.ReadFile(path); string(disk) != secretBoundGraph {
		t.Errorf("a refused edit changed the file")
	}

	// The held text is unchanged: the same base hash still applies, and other
	// config fields of the same node stay editable.
	ops = json.RawMessage(`[{"op":"set_config","node":"deploy","path":["command"],"value":"false"}]`)
	status, out = call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": hash, "ops_version": 1, "ops": ops})
	text, _ := out["text"].(string)
	if status != 200 || !strings.Contains(text, "GH_TOKEN: gh_token") || !strings.Contains(text, "secret_ref: env/GITHUB_TOKEN") {
		t.Fatalf("other config edit: %d %v", status, out)
	}
}

// PUT /graphs/save writes the client's text as it is (ADR-0021): it is not an
// edit operation, so the read-only rule of POST /graphs/edit does not apply.
// This pins the documented limitation rather than endorsing it.
func TestSaveStillWritesSecretDeclarationsAsText(t *testing.T) {
	_, ts, dir := docServer(t)
	path := writeGraph(t, dir, "bound.yaml", secretBoundGraph)
	g := grantFor(t, ts, path, "open")
	_, doc := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	edited := strings.Replace(secretBoundGraph, "env/GITHUB_TOKEN", "env/OTHER_TOKEN", 1)
	status, out := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": doc["hash"], "text": edited})
	if status != 200 {
		t.Fatalf("save: %d %v", status, out)
	}
	if disk, _ := os.ReadFile(path); string(disk) != edited {
		t.Errorf("save did not write the text as sent")
	}
}
