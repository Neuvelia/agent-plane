package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
	"agentplane/internal/shell"
	"agentplane/internal/testenv"
)

const (
	deliveryEnvName = "AGENTPLANE_C44_SECRET"
	// deliveryValue is long enough that no unrelated text contains it.
	deliveryValue = "c44-Canary_Value+with/chars=9f8e7d6c5b4a"
)

// dumpEnvNode is a Shell node that writes its whole environment to a file
// outside the run store. Tests read names from it; the CLI never sees it.
func dumpEnvNode(id, secretEnv, dumpPath string) string {
	assigned := ""
	if secretEnv != "" {
		assigned = ", secret_env: {" + secretEnv + "}"
	}
	return "  " + id + ":\n    type: shell\n    config: {command: /bin/sh, args: [-c, '/usr/bin/env > \"$0\"', " + dumpPath + "]" + assigned + "}\n"
}

func secretGraphText(nodes string) string {
	return corpusHead + "secrets:\n  tok: {secret_ref: env/" + deliveryEnvName + "}\ninputs: {}\nnodes:\n" + nodes + "edges: []\noutputs: {}\n"
}

func writeGraph(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func parseGraphText(t *testing.T, text string) *graph.Definition {
	t.Helper()
	def, err := yamlgraph.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return def
}

// childEnvironment parses an `env` dump into name -> length.
func childEnvironment(t *testing.T, path string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the node never ran: %v", err)
	}
	names := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, value, _ := strings.Cut(line, "=")
		names[name] = len(value)
	}
	return names
}

func TestShellSeesAssignedSecretSiblingDoesNot(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/env")
	t.Setenv(deliveryEnvName, deliveryValue)
	dir := t.TempDir()
	assignedDump, siblingDump := filepath.Join(dir, "assigned"), filepath.Join(dir, "sibling")
	path := writeGraph(t, secretGraphText(dumpEnvNode("a", "GH_TOKEN: tok", assignedDump)+dumpEnvNode("sibling", "", siblingDump)))
	if exit, out, errout := cli(t, "run", path); exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	if got := childEnvironment(t, assignedDump)["GH_TOKEN"]; got != len(deliveryValue) {
		t.Errorf("assigned node saw GH_TOKEN of length %d, want %d", got, len(deliveryValue))
	}
	for name := range childEnvironment(t, siblingDump) {
		if name == "GH_TOKEN" || name == deliveryEnvName {
			t.Errorf("sibling node saw %s", name)
		}
	}
	for name := range childEnvironment(t, assignedDump) {
		if name == deliveryEnvName {
			t.Errorf("the assigned node saw the runner's own variable %s", name)
		}
	}
}

func TestRunWithUnresolvedSecretFailsBeforeAnyRunIsStored(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/env")
	os.Unsetenv(deliveryEnvName)
	dump := filepath.Join(t.TempDir(), "dump")
	path := writeGraph(t, secretGraphText(dumpEnvNode("a", "GH_TOKEN: tok", dump)))
	store := t.TempDir()
	exit, out, errout := cli(t, "run", path, "--store-dir", store, "--json")
	if exit == 0 || !strings.Contains(out+errout, "secret_unresolved") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	if _, err := os.Stat(dump); err == nil {
		t.Error("the node ran although the secret was unresolved")
	}
	if entries, _ := os.ReadDir(store); len(entries) != 0 {
		t.Errorf("a failed preflight left store entries: %v", entries)
	}
}

func TestExecutorSelectionUsesNodeExecutorOnlyWhenNeeded(t *testing.T) {
	plain := parseGraphText(t, corpusHead+"inputs: {}\nnodes:\n"+nodeShell("a", "")+"edges: []\noutputs: {}\n")
	if _, ok := selectExecutor(plain).(shell.Executor); !ok {
		t.Errorf("a plain shell graph must keep the bare shell executor, got %T", selectExecutor(plain))
	}
	assigned := parseGraphText(t, secretGraphText(dumpEnvNode("a", "GH_TOKEN: tok", "/tmp/never")))
	if _, ok := selectExecutor(assigned).(nodeExecutor); !ok {
		t.Errorf("a shell graph with secret_env needs the node executor, got %T", selectExecutor(assigned))
	}
}
