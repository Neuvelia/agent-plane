package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

// startSecretRun posts the graph to serve and waits for run_finished by reading
// the event stream; it returns the run ID and every response body it saw.
func startSecretRun(t *testing.T, s *servedAPI, graphPath string) (string, []string) {
	t.Helper()
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(graphPath)+`,"inputs":[]}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %s", code, body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(body), &started); err != nil {
		t.Fatal(err)
	}
	bodies := []string{body}
	for _, path := range []string{"/runs/" + started.RunID + "/events", "/runs/" + started.RunID, "/runs", "/approvals"} {
		code, text := s.call(t, "GET", path, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, code, text)
		}
		bodies = append(bodies, text)
	}
	return started.RunID, bodies
}

func TestServeRunDeliversSecretFromItsOwnEnvironment(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/env")
	t.Setenv(deliveryEnvName, deliveryValue)
	dir := t.TempDir()
	assignedDump, siblingDump := filepath.Join(dir, "assigned"), filepath.Join(dir, "sibling")
	path := writeGraph(t, secretGraphText(dumpEnvNode("a", "GH_TOKEN: tok", assignedDump)+dumpEnvNode("sibling", "", siblingDump)))
	s := startServe(t)
	startSecretRun(t, s, path)
	if got := childEnvironment(t, assignedDump)["GH_TOKEN"]; got != len(deliveryValue) {
		t.Errorf("assigned node saw GH_TOKEN of length %d, want %d", got, len(deliveryValue))
	}
	for name := range childEnvironment(t, siblingDump) {
		if name == "GH_TOKEN" || name == deliveryEnvName {
			t.Errorf("sibling node saw %s", name)
		}
	}
}

func TestServeNeverReturnsSecretValues(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/base64")
	t.Setenv(deliveryEnvName, deliveryValue)
	path := writeGraph(t, secretGraphText(leakNode))
	s := startServe(t)
	_, bodies := startSecretRun(t, s, path)
	for _, body := range bodies {
		testenv.AssertNoSecretText(t, "API response", body, deliveryValue)
	}
	s.closeStdin()
	testenv.AssertNoSecretBytes(t, s.dir, deliveryValue)
}

func TestServeUnresolvedSecretFailsPreflightWithoutStoringARun(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/env")
	t.Setenv(deliveryEnvName, "")
	path := writeGraph(t, secretGraphText(dumpEnvNode("a", "GH_TOKEN: tok", filepath.Join(t.TempDir(), "dump"))))
	s := startServe(t)
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(path)+`}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "secret_empty") {
		t.Fatalf("%d %s", code, body)
	}
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Fatalf("a failed preflight left store entries: %v", entries)
	}
}

func TestServeWithUnavailableSecretProviderStartsNothing(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/env")
	dump := filepath.Join(t.TempDir(), "dump")
	path := writeGraph(t, localProviderGraph(dump))
	s := startServe(t)
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(path)+`}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "E_SECRET_PROVIDER_UNAVAILABLE") {
		t.Fatalf("%d %s", code, body)
	}
	if _, err := os.Stat(dump); err == nil {
		t.Error("the node ran although its secret provider is unavailable")
	}
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Errorf("a rejected graph left store entries: %v", entries)
	}
}
