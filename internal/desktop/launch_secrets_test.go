package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/graph"
)

func TestInputStringLeavesBoundLargeRepeatedJSONArray(t *testing.T) {
	raw := []byte("[" + strings.Repeat(`"REPEATED_PRIVATE_VALUE",`, 100000) + `"REPEATED_PRIVATE_VALUE"]`)
	value, err := graph.NewJSONValue(raw)
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := inputStringLeaves(map[string]graph.Value{"payload": value})
	if err != nil || len(leaves) != 1 || leaves[0] != "REPEATED_PRIVATE_VALUE" {
		t.Fatalf("repeated JSON retained %d strings, error %v", len(leaves), err)
	}
}

func TestInputStringLeavesStopsAtUniqueValueBudget(t *testing.T) {
	items := make([]string, maxLaunchRedactionValues+1)
	for i := range items {
		items[i] = fmt.Sprintf("\"unique-%d\"", i)
	}
	value, err := graph.NewJSONValue([]byte("[" + strings.Join(items, ",") + "]"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inputStringLeaves(map[string]graph.Value{"payload": value}); !errors.Is(err, errLaunchRedactions) {
		t.Fatalf("over-budget input scan error = %v", err)
	}
}

func preflightSecretGraph(t *testing.T, text string, inputs map[string]graph.Value) (PreflightResult, *PreflightService, Document, WorkspaceRecord) {
	t.Helper()
	service, doc, workspace, _ := preflightFixture(t, text)
	result, err := service.Preflight(context.Background(), PreflightRequest{
		DocumentID: doc.DocumentID, Revision: doc.Revision, WorkspaceID: workspace.WorkspaceID,
		Inputs: inputs, MaxConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result, service, doc, workspace
}

func expectSecretPreflightRejected(t *testing.T, result PreflightResult, wantPath string) {
	t.Helper()
	if !result.HasErrors || result.PreflightToken != "" {
		t.Fatalf("secret-bearing launch received token: %+v", result)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "E_EMBEDDED_SECRET" {
			if diagnostic.Message != "Graph or launch inputs contain a credential-shaped key" || diagnostic.Path != wantPath {
				t.Fatalf("secret diagnostic is not actionable: %+v", diagnostic)
			}
			return
		}
	}
	t.Fatalf("missing embedded-secret diagnostic: %+v", result)
}

func TestPreflightRejectsCredentialShapedShellEnvKey(t *testing.T) {
	text := `schema_version: 0
id: secret-shell
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /bin/echo
      env: {api_Key: private-value}
    outputs: {}
edges: []
outputs: {}
`
	result, _, _, _ := preflightSecretGraph(t, text, nil)
	expectSecretPreflightRejected(t, result, "nodes.step.config.env")
}

func TestMalformedShellEnvReportsNodeConfigInsteadOfCredentialKey(t *testing.T) {
	text := `schema_version: 0
id: malformed-env
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /bin/echo
      env: [wrong-shape]
    outputs: {}
edges: []
outputs: {}
`
	result, _, _, _ := preflightSecretGraph(t, text, nil)
	if !result.HasErrors || result.PreflightToken != "" {
		t.Fatalf("malformed env accepted: %+v", result)
	}
	var nodeConfig, secret bool
	for _, diagnostic := range result.Diagnostics {
		nodeConfig = nodeConfig || diagnostic.Code == "E_NODE_CONFIG"
		secret = secret || diagnostic.Code == "E_EMBEDDED_SECRET"
	}
	if !nodeConfig || secret {
		t.Fatalf("wrong malformed-env diagnostic: %+v", result.Diagnostics)
	}
}

func TestPreflightRejectsNestedCredentialJSONInputKey(t *testing.T) {
	text := `schema_version: 0
id: secret-input
inputs:
  payload: {type: json, required: true}
nodes: {}
edges: []
outputs: {}
`
	value, err := graph.NewJSONValue([]byte(`{"nested":[{"access-Token":"private-value"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, _, _, _ := preflightSecretGraph(t, text, map[string]graph.Value{"payload": value})
	expectSecretPreflightRejected(t, result, "inputs")
}

func TestPreflightAllowsOwnOrdinaryShellEnv(t *testing.T) {
	base := `schema_version: 0
id: own-literal
name: %s
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /bin/echo
      env: {MESSAGE: OWN_LITERAL_VALUE}
    outputs: {}
edges: []
outputs: {}
`
	allowed, _, _, _ := preflightSecretGraph(t, fmt.Sprintf(base, "ordinary"), nil)
	if allowed.HasErrors || allowed.PreflightToken == "" {
		t.Fatalf("own ordinary env literal rejected: %+v", allowed)
	}
}

func TestNestedJSONStringsEnterRedactionsAndBoundPreflight(t *testing.T) {
	text := `schema_version: 0
id: json-input
inputs:
  payload: {type: json, required: true}
nodes: {}
edges: []
outputs: {}
`
	value, err := graph.NewJSONValue([]byte(`{"items":["FIRST_PRIVATE_VALUE",{"value":"SECOND_PRIVATE_VALUE"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, service, doc, workspace := preflightSecretGraph(t, text, map[string]graph.Value{"payload": value})
	if result.HasErrors || result.PreflightToken == "" {
		t.Fatalf("valid JSON input preflight = %+v", result)
	}
	prepared, serviceErr := service.PrepareStart(context.Background(), RunStartRequest{
		PreflightToken: result.PreflightToken, PayloadDigest: result.PayloadDigest,
		DocumentID: doc.DocumentID, Revision: doc.Revision, WorkspaceID: workspace.WorkspaceID,
		Inputs: map[string]graph.Value{"payload": value}, MaxConcurrency: 1,
	})
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	got := make(map[string]bool)
	for _, text := range prepared.Options.LogRedactions {
		got[text] = true
	}
	if !got["FIRST_PRIVATE_VALUE"] || !got["SECOND_PRIVATE_VALUE"] {
		t.Fatalf("nested JSON strings missing from frozen redactions: %v", prepared.Options.LogRedactions)
	}
	items := make([]string, maxLaunchRedactionValues+1)
	for i := range items {
		items[i] = fmt.Sprintf("\"private-%d\"", i)
	}
	tooMany, err := graph.NewJSONValue([]byte("[" + strings.Join(items, ",") + "]"))
	if err != nil {
		t.Fatal(err)
	}
	limited, _, _, _ := preflightSecretGraph(t, text, map[string]graph.Value{"payload": tooMany})
	if !limited.HasErrors || limited.PreflightToken != "" {
		t.Fatalf("over-budget nested JSON received token: %+v", limited)
	}
	if len(limited.Diagnostics) != 1 || limited.Diagnostics[0].Code != "E_REDACTION_LIMIT" {
		t.Fatalf("over-budget nested JSON received wrong diagnostic: %+v", limited.Diagnostics)
	}
}
