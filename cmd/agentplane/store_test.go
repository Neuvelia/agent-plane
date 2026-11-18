package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/store"
	"agentplane/internal/testenv"
)

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), args, &out, &errout)
	return code, out.String(), errout.String()
}

func storedRun(t *testing.T, dir string, extra ...string) string {
	t.Helper()
	requireDemoTools(t)
	args := append([]string{"run", demoGraphPath(t), "--approve", "approve", "--store-dir", dir}, extra...)
	if code, out, errout := cli(t, args...); code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errout)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no run stored: %v", err)
	}
	id := ""
	for _, entry := range entries {
		if entry.IsDir() {
			id = entry.Name()
		}
	}
	if id == "" {
		t.Fatal("no run stored")
	}
	return id
}

func TestStoredRunListsAndShowsIdenticallyFromAFreshInvocation(t *testing.T) {
	requireDemoTools(t)
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
	requireDemoTools(t)
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
	requireDemoTools(t)
	testenv.RequireExecutable(t, "/usr/bin/printenv")
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

// The secret check must cover every string in the definition, including map
// keys: a node ID is copied into every event of the run.
func TestSecretHiddenInNodeIDOrPortNameIsRefusedForStorage(t *testing.T) {
	t.Setenv("AP_ID_SECRET", "secretnodeid")
	for name, doc := range map[string]string{
		"escaped node id":     "schema_version: 0\nid: g\ninputs: {}\nnodes:\n  \"\\x73ecretnodeid\":\n    type: shell\n    config: {command: /usr/bin/true}\nedges: []\noutputs: {}\n",
		"escaped output port": "schema_version: 0\nid: g\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: /usr/bin/true}\n    outputs: {\"\\x73ecretnodeid\": {type: string}}\nedges: []\noutputs: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(doc, "secretnodeid") {
				t.Fatal("test graph must hide the secret from a byte search")
			}
			dir := t.TempDir()
			graph := filepath.Join(t.TempDir(), "g.yaml")
			os.WriteFile(graph, []byte(doc), 0o600)
			code, _, errout := cli(t, "run", graph, "--redact-env", "AP_ID_SECRET", "--store-dir", dir)
			if code != 3 || !strings.Contains(errout, "redact-env") {
				t.Fatalf("code=%d stderr=%s", code, errout)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("nothing may be stored: %v", entries)
			}
		})
	}
}

func TestDefinitionHoldsSecretHasNoFalsePositiveOnCleanGraphs(t *testing.T) {
	def, err := readDefinition(demoGraphPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if definitionHoldsSecret(def, "definitely-not-in-the-demo-graph") {
		t.Fatal("false positive")
	}
	if !definitionHoldsSecret(def, "flaky-ready") { // a real value from the demo gate
		t.Fatal("the walk missed a string it must see")
	}
}

// failingPersister succeeds for the first n events, then fails like a full disk.
type failingPersister struct {
	n, seen int
	closed  bool
}

func (f *failingPersister) Append(events.Event) error {
	f.seen++
	if f.seen > f.n {
		return os.ErrClosed
	}
	return nil
}
func (f *failingPersister) Close() error { f.closed = true; return nil }

func withFailingStore(t *testing.T, n int) *failingPersister {
	t.Helper()
	fp := &failingPersister{n: n}
	old := openPersister
	openPersister = func(store.Store, []byte) persister { return fp }
	t.Cleanup(func() { openPersister = old })
	return fp
}

// A write failure mid-run must not hide what the run did: the result is still
// reported, with the run ID and partial log location, and the exit code is 1.
func TestPersistFailureStillReportsTheRunResultAndExitsOne(t *testing.T) {
	requireDemoTools(t)
	fp := withFailingStore(t, 5)
	dir := t.TempDir()
	code, out, _ := cli(t, "run", demoGraphPath(t), "--approve", "approve", "--store-dir", dir, "--json")
	if code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
	var doc struct {
		Status string         `json:"status"`
		RunID  string         `json:"run_id"`
		Events []events.Event `json:"events"`
		Nodes  []struct{}     `json:"nodes"`
		Error  struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if doc.Status != "succeeded" || doc.RunID == "" || len(doc.Events) < 20 || len(doc.Nodes) != 4 {
		t.Fatalf("the run result was hidden: status=%q run=%q events=%d", doc.Status, doc.RunID, len(doc.Events))
	}
	if doc.Error.Kind != "internal" || !strings.Contains(doc.Error.Message, doc.RunID) || !strings.Contains(doc.Error.Message, filepath.Join(dir, doc.RunID)) {
		t.Fatalf("error must name the run and its partial log: %+v", doc.Error)
	}
	if !fp.closed {
		t.Fatal("the writer must be closed after a failure")
	}
}

// Pinned choice: a persistence failure is exit 1 even when the run itself failed
// or was cancelled, because the run cannot be trusted to be on disk. The JSON
// status still says what the run did.
func TestPersistFailureKeepsTheRunStatusButExitsOneForAFailedRun(t *testing.T) {
	requireDemoTools(t)
	withFailingStore(t, 4)
	code, out, _ := cli(t, "run", demoGraphPath(t), "--reject", "approve", "--store-dir", t.TempDir(), "--json")
	if code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
	var doc struct {
		Status  string `json:"status"`
		Failure struct {
			Kind string `json:"kind"`
		} `json:"failure"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Status != "failed" || doc.Failure.Kind != "rejected" || !strings.Contains(doc.Error.Message, "not persisted") {
		t.Fatalf("%+v", doc)
	}
}

func TestPersistFailureInWatchModeFinishesTheWatchAndReports(t *testing.T) {
	requireDemoTools(t)
	withFailingStore(t, 3)
	done := make(chan struct{})
	var code int
	var out, errout string
	go func() {
		code, out, errout = cli(t, "run", demoGraphPath(t), "--approve", "approve", "--store-dir", t.TempDir(), "--watch")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("--watch hung after a persistence failure")
	}
	_ = out
	if code != 1 || !strings.Contains(errout, "was not persisted") || !strings.Contains(errout, "Run succeeded") {
		t.Fatalf("code=%d stderr=%s", code, errout)
	}
}

// The byte-level secret check cannot see a secret that YAML escaping, folding or
// unicode escapes hide in the file; the parsed values must be checked as well.
func TestEscapedOrFoldedSecretInAGraphIsRefusedForStorage(t *testing.T) {
	const secret = `a"b-secret-value`
	t.Setenv("AP_ESC_SECRET", secret)
	for name, args := range map[string]string{
		"quote escape":   `["a\"b-secret-value"]`,
		"unicode escape": `["\u0061\"b-secret-value"]`,
		"folded scalar":  "[\"a\\\"b-secret-\\\n    value\"]",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			graph := filepath.Join(t.TempDir(), "g.yaml")
			doc := "schema_version: 0\nid: esc\ninputs: {}\nnodes:\n  s:\n    type: shell\n    config: {command: /usr/bin/printf, args: " + args + "}\nedges: []\noutputs: {}\n"
			os.WriteFile(graph, []byte(doc), 0o600)
			if strings.Contains(doc, secret) {
				t.Fatal("test graph must not contain the literal secret, or it would not prove anything")
			}
			code, _, errout := cli(t, "run", graph, "--redact-env", "AP_ESC_SECRET", "--store-dir", dir)
			if code != 3 || !strings.Contains(errout, "redact-env") {
				t.Fatalf("code=%d stderr=%s", code, errout)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("nothing may be stored: %v", entries)
			}
		})
	}
}
