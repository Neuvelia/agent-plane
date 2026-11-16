package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/api"
	"agentplane/internal/events"
	"agentplane/internal/store"
)

type servedAPI struct {
	hub         *api.Hub
	base, token string
	closeStdin  func()
	exit        chan int
	dir         string
}

func startServe(t *testing.T) *servedAPI {
	t.Helper()
	dir := t.TempDir()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	old := serveStdin
	serveStdin = stdinR
	t.Cleanup(func() { serveStdin = old })
	exit := make(chan int, 1)
	hubc := make(chan *api.Hub, 1)
	oldHook := onServe
	onServe = func(srv *api.Server) { hubc <- srv.Hub }
	t.Cleanup(func() { onServe = oldHook })
	go func() {
		exit <- runCommand(context.Background(), []string{"serve", "--store-dir", dir}, stdoutW, io.Discard)
		stdoutW.Close()
	}()
	line, err := bufio.NewReader(stdoutR).ReadString('\n')
	if err != nil {
		t.Fatalf("no announce line: %v", err)
	}
	var info struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(line), &info); err != nil || info.Port == 0 || len(info.Token) != 64 {
		t.Fatalf("announce %q: %v", line, err)
	}
	closed := false
	s := &servedAPI{hub: <-hubc, base: "http://127.0.0.1:" + itoa(info.Port), token: info.Token, exit: exit, dir: dir, closeStdin: func() {
		if !closed {
			closed = true
			stdinW.Close()
		}
	}}
	t.Cleanup(s.closeStdin)
	return s
}

func (s *servedAPI) call(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, s.base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServeRunsTheDemoThroughTheAPIAndPersistsIt(t *testing.T) {
	requireDemoTools(t)
	s := startServe(t)
	if code, _ := (func() (int, string) {
		resp, err := http.Get(s.base + "/runs")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode, ""
	})(); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request: %d", code)
	}
	graphPath, _ := filepath.Abs(filepath.Join("..", "..", ".agents", "graphs", "demo-vertical.yaml"))
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(graphPath)+`,"inputs":[]}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %s", code, body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	json.Unmarshal([]byte(body), &started)
	waitForApproval(t, s.hub)
	var approval struct {
		Approvals []struct {
			NodeRunID string `json:"node_run_id"`
			NodeID    string `json:"node_id"`
		} `json:"approvals"`
	}
	_, b := s.call(t, "GET", "/approvals", "")
	json.Unmarshal([]byte(b), &approval)
	if len(approval.Approvals) != 1 || approval.Approvals[0].NodeID != "approve" {
		t.Fatalf("no pending approval: %+v", approval)
	}
	path := "/runs/" + started.RunID + "/decisions/" + approval.Approvals[0].NodeRunID
	if code, b := s.call(t, "POST", path, `{"decision":"approved","comment":"looks good"}`); code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	if code, _ := s.call(t, "POST", path, `{"decision":"rejected"}`); code != http.StatusConflict {
		t.Fatalf("repeat decision: %d", code)
	}
	// The event stream ends with run_finished; every event arrives once.
	_, stream := s.call(t, "GET", "/runs/"+started.RunID+"/events", "")
	var seqs []uint64
	for _, line := range strings.Split(stream, "\n") {
		if strings.HasPrefix(line, `data: {"`) {
			var e events.Event
			if err := json.Unmarshal([]byte(line[6:]), &e); err != nil {
				t.Fatal(err)
			}
			seqs = append(seqs, e.Sequence)
		}
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("stream not strictly ordered: %v", seqs)
		}
	}
	if len(seqs) < 20 || !strings.Contains(stream, `"type":"run_finished"`) || !strings.Contains(stream, `"decision":"approved"`) {
		t.Fatalf("incomplete stream (%d events)", len(seqs))
	}
	// Restart survival: the run is in the store, readable without the server.
	s.closeStdin()
	select {
	case code := <-s.exit:
		if code != 0 {
			t.Fatalf("serve exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not exit after stdin closed")
	}
	if c, out, _ := cli(t, "runs", "list", "--store-dir", s.dir, "--json"); c != 0 || !strings.Contains(out, started.RunID) || !strings.Contains(out, `"status":"succeeded"`) {
		t.Fatalf("run missing after server exit: %s", out)
	}
	if _, err := os.Stat(filepath.Join(s.dir, started.RunID, "graph.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestServeRejectsAnInvalidGraphWithDiagnosticsAndStartsNothing(t *testing.T) {
	s := startServe(t)
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(bad, []byte("schema_version: 0\nid: bad\ninputs: {}\nnodes:\n  a:\n    type: human\nedges: []\noutputs: {}\n"), 0o600)
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(bad)+`}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "E_HUMAN_INVALID") {
		t.Fatalf("%d %s", code, body)
	}
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Fatalf("an invalid graph must not create a run: %v", entries)
	}
	if code, _ := s.call(t, "POST", "/runs", `{"graph_path":"/nonexistent/graph.yaml"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("missing graph file: %d", code)
	}
}

func waitForApproval(t *testing.T, hub *api.Hub) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hub.WaitPending(ctx, 1); err != nil {
		t.Fatalf("the run never reached its human node: %v", err)
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

// Stopping the server while a run waits for a human must cancel it and leave a
// complete log (run_finished cancelled), not a half-written run.
func TestServeStopWhileWaitingForApprovalCancelsAndFinishesTheRun(t *testing.T) {
	requireDemoTools(t)
	s := startServe(t)
	graphPath, _ := filepath.Abs(filepath.Join("..", "..", ".agents", "graphs", "demo-vertical.yaml"))
	_, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(graphPath)+`}`)
	var started struct {
		RunID string `json:"run_id"`
	}
	json.Unmarshal([]byte(body), &started)
	waitForApproval(t, s.hub)
	// While it waits, the run is reported as running.
	if _, b := s.call(t, "GET", "/runs", ""); !strings.Contains(b, `"status":"running"`) {
		t.Fatalf("a waiting run must be listed as running: %s", b)
	}
	if _, b := s.call(t, "GET", "/runs/"+started.RunID, ""); !strings.Contains(b, `"status":"running"`) {
		t.Fatalf("run detail must say running: %s", b)
	}
	s.closeStdin()
	select {
	case code := <-s.exit:
		if code != 0 {
			t.Fatalf("serve exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not exit")
	}
	_, out, _ := cli(t, "runs", "list", "--store-dir", s.dir, "--json")
	if !strings.Contains(out, `"status":"cancelled"`) || strings.Contains(out, `"status":"interrupted"`) {
		t.Fatalf("the cancelled run must have a terminal event: %s", out)
	}
}

// With the real starter and a store that cannot be written, the run must not
// execute any node and the failure must be reported, not swallowed.
func TestServeWithAnUnwritableStoreRunsNothingAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()
	old := serveStdin
	serveStdin = stdinR
	defer func() { serveStdin = old }()
	file := filepath.Join(dir, "afile")
	os.WriteFile(file, []byte("x"), 0o600)
	srv := &api.Server{Token: "t", Store: store.Store{Dir: filepath.Join(file, "runs")}, Hub: api.NewHub(), Start: starter(store.Store{Dir: filepath.Join(file, "runs")}, 2)}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	marker := filepath.Join(dir, "executed")
	graph := filepath.Join(dir, "g.yaml")
	os.WriteFile(graph, []byte("schema_version: 0\nid: g\ninputs: {}\nnodes:\n  touch:\n    type: shell\n    config: {command: /usr/bin/touch, args: ["+quote(marker)+"]}\nedges: []\noutputs: {}\n"), 0o600)
	req, _ := http.NewRequest("POST", ts.URL+"/runs", strings.NewReader(`{"graph_path":`+quote(graph)+`}`))
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: the run could not be stored", resp.StatusCode)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a node executed although its run could not be stored")
	}
}

// The real server, end to end: grant, open, edit, save, and a run from the grant.
func TestServeEditsAndSavesAGraphThroughGrants(t *testing.T) {
	s := startServe(t)
	defer s.closeStdin()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "g.yaml")
	text := "schema_version: 0\nid: g\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	decode := func(body string) map[string]any {
		var out map[string]any
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("not JSON: %s", body)
		}
		return out
	}
	status, body := s.call(t, "POST", "/graphs/grants", `{"path":`+quote(path)+`,"mode":"open"}`)
	if status != http.StatusCreated {
		t.Fatalf("grant: %d %s", status, body)
	}
	grant := decode(body)["grant"].(string)
	status, body = s.call(t, "POST", "/graphs/open", `{"grant":`+quote(grant)+`}`)
	doc := decode(body)
	if status != 200 || doc["definition"] == nil {
		t.Fatalf("open: %d %s", status, body)
	}
	// A starter shell node has no command yet: the editor must be told, before any run.
	status, body = s.call(t, "POST", "/graphs/edit", `{"grant":`+quote(grant)+`,"base_hash":`+quote(doc["hash"].(string))+`,"ops_version":1,"ops":[{"op":"add_node","id":"work","type":"shell"}]}`)
	edited := decode(body)
	if status != 200 {
		t.Fatalf("edit: %d %s", status, body)
	}
	var config bool
	for _, d := range edited["diagnostics"].([]any) {
		if d.(map[string]any)["code"] == nodeConfigCode {
			config = true
		}
	}
	if !config {
		t.Errorf("the API did not report the starter node's missing command: %v", edited["diagnostics"])
	}
	status, body = s.call(t, "PUT", "/graphs/save", `{"grant":`+quote(grant)+`,"base_hash":`+quote(doc["hash"].(string))+`,"text":`+quote(edited["text"].(string))+`}`)
	if status != 200 {
		t.Fatalf("save: %d %s", status, body)
	}
	if disk, _ := os.ReadFile(path); string(disk) != edited["text"].(string) {
		t.Errorf("the saved file is not the edited text:\n%s", disk)
	}
	// Running from the grant reads the saved bytes: the stored snapshot is the saved file.
	// (Run start keeps graph.Validate, as `agentplane run` does; the empty command
	// fails when the node executes, not at start.)
	status, body = s.call(t, "POST", "/runs", `{"graph_grant":`+quote(grant)+`}`)
	if status != http.StatusAccepted {
		t.Fatalf("run from a grant: %d %s", status, body)
	}
	runID := decode(body)["run_id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, body = s.call(t, "GET", "/runs/"+runID, "")
		if status == 200 && decode(body)["graph"] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never became readable: %d %s", runID, status, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := decode(body)["graph"].(string); got != edited["text"].(string) {
		t.Errorf("the run executed %q, not the saved file", got)
	}
}
