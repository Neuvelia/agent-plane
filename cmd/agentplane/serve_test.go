package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/events"
)

type servedAPI struct {
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
	s := &servedAPI{base: "http://127.0.0.1:" + itoa(info.Port), token: info.Token, exit: exit, dir: dir, closeStdin: func() {
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
	var approval struct {
		Approvals []struct {
			NodeRunID string `json:"node_run_id"`
			NodeID    string `json:"node_id"`
		} `json:"approvals"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(approval.Approvals) == 0 && time.Now().Before(deadline) {
		_, b := s.call(t, "GET", "/approvals", "")
		json.Unmarshal([]byte(b), &approval)
		time.Sleep(10 * time.Millisecond)
	}
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

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

// Stopping the server while a run waits for a human must cancel it and leave a
// complete log (run_finished cancelled), not a half-written run.
func TestServeStopWhileWaitingForApprovalCancelsAndFinishesTheRun(t *testing.T) {
	s := startServe(t)
	graphPath, _ := filepath.Abs(filepath.Join("..", "..", ".agents", "graphs", "demo-vertical.yaml"))
	_, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(graphPath)+`}`)
	var started struct {
		RunID string `json:"run_id"`
	}
	json.Unmarshal([]byte(body), &started)
	// Wait for the human node to be pending, using the API's own approvals list.
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, b := s.call(t, "GET", "/approvals", "")
		if strings.Contains(b, started.RunID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run never reached its human node")
		}
		time.Sleep(10 * time.Millisecond)
	}
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
