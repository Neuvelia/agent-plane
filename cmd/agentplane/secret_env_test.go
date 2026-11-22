package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

// F3.8 / F-1: a graph whose Shell node maps a declared secret into the
// environment is valid; until values are delivered, running it fails the node
// closed without starting the process.

func secretEnvGraph(marker string) string {
	return corpusHead + "secrets:\n  tok: {secret_ref: env/AGENTPLANE_C43_SECRET}\ninputs: {}\nnodes:\n" +
		"  a:\n    type: shell\n    config: {command: /usr/bin/touch, args: [" + marker + "], secret_env: {GH_TOKEN: tok}}\n" +
		"edges: []\noutputs: {}\n"
}

func writeSecretEnvGraph(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(path, []byte(secretEnvGraph(marker)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGraphValidateAcceptsSecretEnvOnShell(t *testing.T) {
	path := writeSecretEnvGraph(t, "/tmp/never")
	exit, out, errout := cli(t, "graph", "validate", path, "--json")
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	var doc struct {
		Status      string           `json:"status"`
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Status != "valid" || len(doc.Diagnostics) != 0 {
		t.Fatalf("status=%q diagnostics=%v", doc.Status, doc.Diagnostics)
	}
}

func TestRunFailsClosedWhenSecretEnvHasNoDelivery(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/touch")
	const hostValue = "sentinel-host-secret-value"
	t.Setenv("AGENTPLANE_C43_SECRET", hostValue)
	marker := filepath.Join(t.TempDir(), "ran")
	path := writeSecretEnvGraph(t, marker)
	exit, out, errout := cli(t, "run", path, "--json")
	if exit == 0 {
		t.Fatalf("run succeeded; stdout=%s", out)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the process ran: %v", err)
	}
	for _, text := range []string{out, errout} {
		if strings.Contains(text, hostValue) || strings.Contains(text, "GH_TOKEN") {
			t.Fatalf("output leaks the secret value or variable name: %s", text)
		}
	}
	if !strings.Contains(out, "config") {
		t.Fatalf("the node failure is not a configuration failure: %s", out)
	}
}

func TestParityCorpusValidSecretGraph(t *testing.T) {
	s := startServe(t)
	defer s.closeStdin()
	text := secretEnvGraph("/tmp/never")
	status, body := s.call(t, "POST", "/graphs/validate", `{"text":`+quote(text)+`}`)
	var doc struct {
		Status      string           `json:"status"`
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil || status != 200 {
		t.Fatalf("API %d %s", status, body)
	}
	if doc.Status != "valid" || len(doc.Diagnostics) != 0 {
		t.Fatalf("API status=%q diagnostics=%v", doc.Status, doc.Diagnostics)
	}
}

// Secret codes whose graph has a valid Shell node must not drag along an
// E_NODE_CONFIG_INVALID: that pair was the F-1 symptom.
func TestParityCorpusSecretCodesAddNoNodeConfigDiagnostic(t *testing.T) {
	for _, code := range []string{
		"E_SECRET_NAME_INVALID", "E_SECRET_ENTRY_INVALID", "E_SECRET_REF_REQUIRED", "E_SECRET_REF_INVALID",
		"E_SECRET_PROVIDER_UNKNOWN", "E_SECRET_PROVIDER_UNAVAILABLE", "E_SECRET_ENV_UNDECLARED", "E_SECRET_AMBIENT_BYPASS",
	} {
		path := filepath.Join(t.TempDir(), "g.yaml")
		if err := os.WriteFile(path, []byte(corpus[code]), 0o600); err != nil {
			t.Fatal(err)
		}
		_, out, _ := cli(t, "graph", "validate", path, "--json")
		if !strings.Contains(out, code) || strings.Contains(out, "E_NODE_CONFIG_INVALID") {
			t.Errorf("%s: diagnostics = %s", code, out)
		}
	}
}
