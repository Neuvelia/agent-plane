package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/events"
)

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), args, &out, &errout)
	return code, out.String(), errout.String()
}

func storedRun(t *testing.T, dir string, extra ...string) string {
	t.Helper()
	args := append([]string{"run", demoGraphPath(t), "--approve", "approve", "--store-dir", dir}, extra...)
	if code, out, errout := cli(t, args...); code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errout)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no run stored: %v", err)
	}
	return entries[len(entries)-1].Name()
}

func TestStoredRunListsAndShowsIdenticallyFromAFreshInvocation(t *testing.T) {
	dir := t.TempDir()
	// Live output of the same run, captured as JSON, is the reference.
	var live struct {
		RunID  string         `json:"run_id"`
		Events []events.Event `json:"events"`
	}
	code, out, errout := cli(t, "run", demoGraphPath(t), "--approve", "approve", "--store-dir", dir, "--json")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errout)
	}
	if err := json.Unmarshal([]byte(out), &live); err != nil {
		t.Fatal(err)
	}
	_, listOut, _ := cli(t, "runs", "list", "--store-dir", dir, "--json")
	var list struct {
		Runs []struct {
			RunID  string `json:"run_id"`
			Status string `json:"status"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(listOut), &list); err != nil || len(list.Runs) != 1 || list.Runs[0].RunID != live.RunID || list.Runs[0].Status != "succeeded" {
		t.Fatalf("list: %v %s", err, listOut)
	}
	_, showOut, _ := cli(t, "runs", "show", live.RunID, "--store-dir", dir, "--json")
	var shown struct {
		Events []events.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(showOut), &shown); err != nil {
		t.Fatal(err)
	}
	if len(shown.Events) != len(live.Events) {
		t.Fatalf("stored %d events, live %d", len(shown.Events), len(live.Events))
	}
	for i := range live.Events {
		a, _ := json.Marshal(live.Events[i])
		b, _ := json.Marshal(shown.Events[i])
		if string(a) != string(b) {
			t.Fatalf("event %d differs after storage:\n%s\n%s", i, a, b)
		}
		if i > 0 && shown.Events[i].Sequence <= shown.Events[i-1].Sequence {
			t.Fatal("stored sequence is not monotonic")
		}
	}
	if snapshot, err := os.ReadFile(filepath.Join(dir, live.RunID, "graph.yaml")); err != nil || !strings.Contains(string(snapshot), "id: demo-vertical") {
		t.Fatalf("graph snapshot: %v", err)
	}
}

func TestKilledRunIsInterruptedAndEarlierEventsStayReadable(t *testing.T) {
	dir := t.TempDir()
	id := storedRun(t, dir)
	path := filepath.Join(dir, id, "events.jsonl")
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// Simulate SIGKILL after the first approval request: drop everything later
	// and leave a half-written line behind.
	keep := 0
	for i, l := range lines {
		if strings.Contains(l, `"type":"approval_requested"`) {
			keep = i + 1
		}
	}
	cut := strings.Join(lines[:keep], "\n") + "\n" + lines[keep][:20]
	if err := os.WriteFile(path, []byte(cut), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, _ := cli(t, "runs", "list", "--store-dir", dir, "--json")
	if !strings.Contains(out, `"status":"interrupted"`) || !strings.Contains(out, `"truncated":true`) {
		t.Fatalf("%s", out)
	}
	_, shown, _ := cli(t, "runs", "show", id, "--store-dir", dir, "--json")
	if !strings.Contains(shown, `"type":"approval_requested"`) || strings.Contains(shown, `"type":"run_finished"`) {
		t.Fatal("earlier events must remain and no terminal event may be invented")
	}
}

func TestRunWithoutStoreDirWritesNothing(t *testing.T) {
	cwd := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)
	if code, _, _ := cli(t, "run", filepath.Join(prev, "..", "..", ".agents", "graphs", "demo-vertical.yaml"), "--approve", "approve"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("history must be opt-in, found %v", entries)
	}
}

func TestUnwritableStoreFailsBeforeAnyNodeRuns(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("x"), 0o600)
	code, out, _ := cli(t, "run", demoGraphPath(t), "--approve", "approve", "--store-dir", filepath.Join(file, "runs"), "--json")
	if code != 1 || strings.Contains(out, "node_started") || strings.Contains(out, "fast branch done") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestStoredFilesNeverContainARedactedSecret(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AP_DEMO_SECRET", "hunter2-super-secret")
	graph := filepath.Join(t.TempDir(), "g.yaml")
	os.WriteFile(graph, []byte(`schema_version: 0
id: leaky
inputs: {}
nodes:
  echo:
    type: shell
    config: {command: /usr/bin/printenv, args: [AP_DEMO_SECRET], env_allowlist: [AP_DEMO_SECRET]}
edges: []
outputs: {}
`), 0o600)
	if code, _, errout := cli(t, "run", graph, "--redact-env", "AP_DEMO_SECRET", "--store-dir", dir); code != 0 {
		t.Fatalf("code=%d %s", code, errout)
	}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			data, _ := os.ReadFile(p)
			if bytes.Contains(data, []byte("hunter2-super-secret")) {
				t.Fatalf("secret reached %s", p)
			}
			// log chunks are base64: check the decoded bytes too.
			if strings.HasSuffix(p, ".jsonl") {
				for _, line := range strings.Split(string(data), "\n") {
					var e events.Event
					if json.Unmarshal([]byte(line), &e) == nil {
						if c, ok := e.Payload.(events.LogChunkPayload); ok && bytes.Contains(c.Data, []byte("hunter2-super-secret")) {
							t.Fatal("decoded log chunk contains the secret")
						}
					}
				}
			}
		}
		return nil
	})
}

func TestRunsShowRejectsTraversalAndUnknownRuns(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"../x", "nope", "11111111-1111-4111-8111-111111111111"} {
		if code, _, _ := cli(t, "runs", "show", id, "--store-dir", dir); code == 0 {
			t.Fatalf("show %q succeeded", id)
		}
	}
	if code, _, _ := cli(t, "runs", "show"); code != 2 {
		t.Fatalf("missing run ID must be a usage error, got %d", code)
	}
}

func TestGraphEmbeddingAConfiguredSecretIsNotStored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AP_DEMO_SECRET", "hunter2-super-secret")
	graph := filepath.Join(t.TempDir(), "g.yaml")
	os.WriteFile(graph, []byte(`schema_version: 0
id: leaky
inputs: {}
nodes:
  echo:
    type: shell
    config: {command: /usr/bin/printf, args: ["token=hunter2-super-secret"]}
edges: []
outputs: {}
`), 0o600)
	code, _, _ := cli(t, "run", graph, "--redact-env", "AP_DEMO_SECRET", "--store-dir", dir)
	if code != 3 {
		t.Fatalf("code=%d, want graph error 3", code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("nothing may be stored, found %v", entries)
	}
}
