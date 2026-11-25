package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

// authorMarker is part of every piece of author text these tests type where a
// key, id or variable name belongs. The output must never contain it.
const authorMarker = "Live9f3Q"

func requireNoAuthorText(t *testing.T, where, text string) {
	t.Helper()
	if strings.Contains(strings.ToLower(text), strings.ToLower(authorMarker)) {
		t.Fatalf("%s echoes author text: %s", where, text)
	}
}

func TestGraphValidateNeverEchoesInvalidAuthorText(t *testing.T) {
	text := "schema_version: 0\nid: \"G=sk-" + authorMarker + "\"\ninputs: {}\nnodes:\n" +
		"  \"N=sk-" + authorMarker + "\":\n    type: shell\n    config: {command: /usr/bin/true}\n" +
		"  a:\n    type: shell\n    config: {command: /usr/bin/true, env_allowlist: [\"TOKEN=sk-" + authorMarker + "\"]}\n" +
		"edges: []\noutputs: {}\n"
	path := writeGraph(t, text)
	for _, extra := range [][]string{nil, {"--json"}} {
		exit, out, errout := cli(t, append([]string{"graph", "validate", path}, extra...)...)
		if exit != 3 {
			t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
		}
		for _, code := range []string{"E_GRAPH_ID_INVALID", "E_IDENTIFIER_INVALID", "E_NODE_CONFIG_INVALID", "env_allowlist[0]"} {
			if !strings.Contains(out+errout, code) {
				t.Errorf("output lacks %q: %s%s", code, out, errout)
			}
		}
		requireNoAuthorText(t, "graph validate", out+errout)
	}
}

func TestRunNeverEchoesUndeclaredInputKeyOrRedactEnvName(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	marker := filepath.Join(t.TempDir(), "ran")
	path := writeGraph(t, inputGraph(marker))
	key := "leaked" + strings.ToLower(authorMarker)
	for name, args := range map[string][]string{
		"undeclared input": {"--input", key + "=x"},
		"duplicate input":  {"--input", key + "=x", "--input", key + "=y"},
		"redact-env value": {"--redact-env", "sk-" + authorMarker},
	} {
		for _, extra := range [][]string{nil, {"--json"}} {
			t.Run(name, func(t *testing.T) {
				t.Setenv(deliveryEnvName, deliveryValue)
				exit, out, errout := cli(t, append(append([]string{"run", path}, args...), extra...)...)
				if exit == 0 {
					t.Fatalf("run accepted %v: %s", args, out)
				}
				requireNoAuthorText(t, "run", out+errout)
			})
		}
	}
}

func TestServeNeverEchoesUndeclaredInputKey(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker := filepath.Join(t.TempDir(), "ran")
	s := startServe(t)
	body, _ := json.Marshal(map[string]any{"graph_path": writeGraph(t, inputGraph(marker)), "inputs": []string{"leaked" + strings.ToLower(authorMarker) + "=x"}})
	code, response := s.call(t, "POST", "/runs", string(body))
	if code != http.StatusUnprocessableEntity || !strings.Contains(response, "E_GRAPH_INPUT_VALUE_UNKNOWN") {
		t.Fatalf("%d %s", code, response)
	}
	requireNoAuthorText(t, "API response", response)
}
