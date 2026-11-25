package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

type documentOutput struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Value    json.RawMessage `json:"value"`
	Redacted bool            `json:"redacted"`
}

func decodeOutputs(t *testing.T, stdout string) []documentOutput {
	t.Helper()
	var doc struct {
		SchemaVersion int              `json:"cli_schema_version"`
		Outputs       []documentOutput `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != 1 {
		t.Fatalf("cli_schema_version = %d, want 1", doc.SchemaVersion)
	}
	return doc.Outputs
}

// TestRunMasksNamedEnvironmentValueInGraphOutputs closes the gap the sibling
// test above leaves open on purpose: it removes the graph outputs, this one
// keeps them.
func TestRunMasksNamedEnvironmentValueInGraphOutputs(t *testing.T) {
	t.Setenv("AGENTPLANE_TEST_SECRET", "secret-value-482")
	graphText := strings.Replace(oneNode, `"echo"]`, `"env"]`, 1)
	graphText = strings.Replace(graphText, `env: {AGENTPLANE_CLI_HELPER: "1"}`, `env: {AGENTPLANE_CLI_HELPER: "1"}
      env_allowlist: [AGENTPLANE_TEST_SECRET]`, 1)
	path := graphFile(t, graphText)
	code, stdout, stderr := invoke(t, context.Background(), "run", path, "--redact-env", "AGENTPLANE_TEST_SECRET", "--json")
	if code != 0 || stderr != "" || strings.Contains(stdout, "secret-value-482") {
		t.Fatalf("redacted JSON=%d %q %q", code, stdout, stderr)
	}
	outputs := decodeOutputs(t, stdout)
	if len(outputs) != 1 || string(outputs[0].Value) != `"***"` || !outputs[0].Redacted || outputs[0].Type != "string" {
		t.Fatalf("outputs = %#v", outputs)
	}
}

func TestRunOutputsCarryNoRedactedFieldWithoutRedactions(t *testing.T) {
	code, stdout, stderr := invoke(t, context.Background(), "run", graphFile(t, oneNode), "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("run=%d %q %q", code, stdout, stderr)
	}
	if strings.Contains(stdout, `"redacted"`) {
		t.Fatalf("a run without redactions reports redacted: %s", stdout)
	}
}

func TestDocumentReportsWithheldOutputTypeAndNullValue(t *testing.T) {
	doc := documentForRun(run.Result{Outputs: []run.PortValue{
		{Name: "doc", Redacted: true, Withheld: graph.ValueTypeJSON},
		{Name: "text", Redacted: true, Value: graph.NewStringValue("token=***")},
	}}, nil)
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	outputs := decodeOutputs(t, strings.Replace(string(encoded), `"cli_schema_version":0`, `"cli_schema_version":1`, 1))
	if len(outputs) != 2 || outputs[0].Type != "json" || string(outputs[0].Value) != "null" || !outputs[0].Redacted {
		t.Fatalf("withheld output = %#v", outputs)
	}
	if outputs[1].Type != "string" || string(outputs[1].Value) != `"token=***"` || !outputs[1].Redacted {
		t.Fatalf("masked output = %#v", outputs[1])
	}
}
