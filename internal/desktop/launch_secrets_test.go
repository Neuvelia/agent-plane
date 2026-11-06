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

func TestPreflightRejectsOwnShellLiteralMatchingRequiredIdentity(t *testing.T) {
	text := `schema_version: 0
id: ownliteral
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /bin/echo
      env: {MESSAGE: ownliteral}
    outputs: {}
edges: []
outputs: {}
`
	result, _, _, _ := preflightSecretGraph(t, text, nil)
	if !result.HasErrors || result.PreflightToken != "" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "E_UNSAFE_IDENTITY" || result.Diagnostics[0].GraphID != "" || result.Diagnostics[0].Message != "Required graph identity intersects a private launch value" {
		t.Fatalf("unsafe identity received token or leaked its value: %+v", result)
	}
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

func TestPreflightRejectsInputStringPresentInSavedGraph(t *testing.T) {
	text := `schema_version: 0
id: embedded-input
name: ordinary-title
inputs:
  query: {type: string, required: true}
nodes: {}
edges: []
outputs: {}
# HIDDEN_INPUT_VALUE
`
	result, _, _, _ := preflightSecretGraph(t, text, map[string]graph.Value{"query": graph.NewStringValue("HIDDEN_INPUT_VALUE")})
	if !result.HasErrors || result.PreflightToken != "" {
		t.Fatalf("embedded input received token: %+v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "E_EMBEDDED_VALUE" || result.Diagnostics[0].Path != "document_id" {
		t.Fatalf("embedded input diagnostic = %+v", result.Diagnostics)
	}
	if result.Diagnostics[0].Message != "Saved graph contains a launch input or environment value" || result.Diagnostics[0].Suggestion == "" {
		t.Fatalf("embedded input lacks actionable safe text: %+v", result.Diagnostics[0])
	}
}

func TestPreflightRejectsYAMLEscapedInputString(t *testing.T) {
	text := `schema_version: 0
id: escaped-input
name: "PRIVATE\u005fINPUT_VALUE"
inputs:
  query: {type: string, required: true}
nodes: {}
edges: []
outputs: {}
`
	result, _, _, _ := preflightSecretGraph(t, text, map[string]graph.Value{"query": graph.NewStringValue("PRIVATE_INPUT_VALUE")})
	if !result.HasErrors || result.PreflightToken != "" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "E_EMBEDDED_VALUE" {
		t.Fatalf("escaped embedded input preflight = %+v", result)
	}
}

func TestPrepareStartRechecksEmbeddedInputWithLegacyTicket(t *testing.T) {
	text := `schema_version: 0
id: legacy-ticket
inputs:
  query: {type: string, required: true}
nodes: {}
edges: []
outputs: {}
# LEGACY_PRIVATE_VALUE
`
	service, doc, workspace, _ := preflightFixture(t, text)
	ctx := context.Background()
	inputs := map[string]graph.Value{"query": graph.NewStringValue("LEGACY_PRIVATE_VALUE")}
	snapshot, err := service.documents.SavedSnapshot(doc.DocumentID, doc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := service.registry.ResolveWorkspace(workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := service.registry.Settings()
	if err != nil {
		t.Fatal(err)
	}
	definition, err := graph.Parse([]byte(snapshot.Text))
	if err != nil {
		t.Fatal(err)
	}
	capabilityHash, _, findings := service.checkNodesWithEnvironment(ctx, definition, resolved, settings)
	if (graph.ValidationResult{Diagnostics: findings}).HasErrors() {
		t.Fatalf("unexpected capability findings: %+v", findings)
	}
	encodedInputs, err := encodeTypedInputsExact(inputs)
	if err != nil {
		t.Fatal(err)
	}
	digest := payloadDigest(snapshot, resolved, settings, 1, encodedInputs, capabilityHash)
	token, err := service.storeToken(PreflightTicket{Document: snapshot, Workspace: resolved, Inputs: inputs,
		MaxConcurrency: 1, Settings: settings, CapabilityHash: capabilityHash, PayloadDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	_, serviceErr := service.PrepareStart(ctx, RunStartRequest{PreflightToken: token, PayloadDigest: digest,
		DocumentID: doc.DocumentID, Revision: doc.Revision, WorkspaceID: workspace.WorkspaceID,
		Inputs: inputs, MaxConcurrency: 1})
	if serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("legacy ticket with embedded input Start error = %+v", serviceErr)
	}
}

func TestPreflightRejectsNestedJSONStringPresentInSavedGraph(t *testing.T) {
	text := `schema_version: 0
id: embedded-json-input
inputs:
  payload: {type: json, required: true}
nodes: {}
edges: []
outputs: {}
# PRIVATE_JSON_LEAF
`
	value, err := graph.NewJSONValue([]byte(`{"items":["PRIVATE_JSON_LEAF"]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, _, _, _ := preflightSecretGraph(t, text, map[string]graph.Value{"payload": value})
	if !result.HasErrors || result.PreflightToken != "" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "E_EMBEDDED_VALUE" {
		t.Fatalf("embedded JSON leaf preflight = %+v", result)
	}
}

func TestPreflightRejectsFrozenEnvironmentValuePresentInSavedGraph(t *testing.T) {
	t.Setenv("AP_F2_FROZEN_VALUE", "PRIVATE_ENV_VALUE")
	text := `schema_version: 0
id: embedded-environment
name: PRIVATE_ENV_VALUE
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /bin/echo
      env_allowlist: [AP_F2_FROZEN_VALUE]
    outputs: {}
edges: []
outputs: {}
`
	result, _, _, _ := preflightSecretGraph(t, text, nil)
	if !result.HasErrors || result.PreflightToken != "" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "E_EMBEDDED_VALUE" {
		t.Fatalf("embedded frozen environment preflight = %+v", result)
	}
}

func TestPreflightEmbeddedValueDiagnosticDoesNotEchoPrivateGraphID(t *testing.T) {
	t.Setenv("AP_F2_PRIVATE_GRAPH_ID", "privatevalue")
	text := `schema_version: 0
id: privatevalue
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /bin/echo
      env_allowlist: [AP_F2_PRIVATE_GRAPH_ID]
    outputs: {}
edges: []
outputs: {}
`
	result, _, _, _ := preflightSecretGraph(t, text, nil)
	if !result.HasErrors || result.PreflightToken != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "E_EMBEDDED_VALUE" || result.Diagnostics[0].GraphID != "" || strings.Contains(fmt.Sprint(result.Diagnostics), "privatevalue") {
		t.Fatalf("embedded-value diagnostic leaked private graph identity: %+v", result)
	}
}

func TestEarlyPreflightErrorDoesNotEchoPrivateIdentity(t *testing.T) {
	t.Setenv("AP_F2_EARLY_PRIVATE_ID", "privatevalue")
	text := `schema_version: 0
id: privatevalue
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /bin/echo
      env_allowlist: [AP_F2_EARLY_PRIVATE_ID]
    outputs: {}
edges: []
outputs: {}
`
	service, doc, workspace, _ := preflightFixture(t, text)
	result, err := service.Preflight(context.Background(), PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision, WorkspaceID: workspace.WorkspaceID, MaxConcurrency: 0})
	if err != nil || !result.HasErrors || result.PreflightToken != "" || strings.Contains(fmt.Sprint(result.Diagnostics), "privatevalue") {
		t.Fatalf("early preflight diagnostic leaked required identity: result=%+v err=%v", result, err)
	}
}

func TestCapabilityPreflightErrorDoesNotEchoLiteralIdentity(t *testing.T) {
	text := `schema_version: 0
id: privatevalue
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: ""
      env: {MESSAGE: privatevalue}
    outputs: {}
edges: []
outputs: {}
`
	service, doc, workspace, _ := preflightFixture(t, text)
	result, err := service.Preflight(context.Background(), PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision, WorkspaceID: workspace.WorkspaceID, MaxConcurrency: 1})
	if err != nil || !result.HasErrors || result.PreflightToken != "" || strings.Contains(fmt.Sprint(result.Diagnostics), "privatevalue") {
		t.Fatalf("capability diagnostic leaked required identity: result=%+v err=%v", result, err)
	}
}

func TestGraphContainsExternalValueExemptsOnlyOwnShellLiteral(t *testing.T) {
	text := `config: {env: {MESSAGE: OWN_LITERAL_VALUE}}`
	if graphContainsExternalValue(text, LaunchEnvironment{literals: []string{"OWN_LITERAL_VALUE"}}, nil) {
		t.Fatal("own ordinary Shell literal was rejected")
	}
	if !graphContainsExternalValue(text, LaunchEnvironment{shell: map[string]string{"NAMED": "OWN_LITERAL_VALUE"}}, nil) {
		t.Fatal("frozen external value was accepted")
	}
	if !graphContainsExternalValue(text, LaunchEnvironment{literals: []string{"OWN_LITERAL_VALUE"}}, []string{"OWN_LITERAL_VALUE"}) {
		t.Fatal("input value matching an own literal was accepted")
	}
	if graphContainsExternalValue("name: ordinary", LaunchEnvironment{}, []string{""}) {
		t.Fatal("empty input value matched the graph")
	}
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
