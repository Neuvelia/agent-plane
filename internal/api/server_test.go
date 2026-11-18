package api

import (
	"agentplane/internal/domain/run"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/store"
)

const token = "test-token"
const runID = "33333333-3333-4333-8333-333333333333"

func newServer(t *testing.T, start StartFunc) (*Server, *httptest.Server) {
	t.Helper()
	s := &Server{Token: token, Store: store.Store{Dir: t.TempDir()}, Hub: NewHub(), Start: start,
		Validate: func(string) (string, []graph.Diagnostic, error) { return "valid", nil, nil }}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func do(t *testing.T, ts *httptest.Server, method, path, body string, auth bool) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestEveryEndpointRequiresTheBearerToken(t *testing.T) {
	_, ts := newServer(t, nil)
	for _, c := range [][2]string{{"GET", "/runs"}, {"POST", "/runs"}, {"GET", "/runs/" + runID}, {"GET", "/runs/" + runID + "/events"}, {"POST", "/runs/" + runID + "/decisions/x"}, {"GET", "/approvals"}, {"POST", "/graphs/validate"}} {
		resp, _ := do(t, ts, c[0], c[1], "{}", false)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s without token: %d", c[0], c[1], resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", ts.URL+"/runs", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token accepted: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts, "GET", "/runs", "", true); resp.StatusCode != http.StatusOK {
		t.Fatalf("valid token rejected: %d", resp.StatusCode)
	}
}

func TestEmptyConfiguredTokenNeverAuthenticates(t *testing.T) {
	s := &Server{Store: store.Store{Dir: t.TempDir()}, Hub: NewHub()}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/runs", nil)
	req.Header.Set("Authorization", "Bearer ")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty token accepted: %d", resp.StatusCode)
	}
}

func TestHostHeaderMustMatchTheLoopbackListener(t *testing.T) {
	s, ts := newServer(t, nil)
	s.ExpectHost("127.0.0.1:1") // the test server listens elsewhere, as a rebinding host would differ
	resp, _ := do(t, ts, "GET", "/runs", "", true)
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("host mismatch status %d", resp.StatusCode)
	}
}

func TestNoCORSHeadersAreEverWritten(t *testing.T) {
	_, ts := newServer(t, nil)
	for _, auth := range []bool{true, false} {
		resp, _ := do(t, ts, "GET", "/runs", "", auth)
		for k := range resp.Header {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Fatalf("CORS header %s written", k)
			}
		}
	}
}

func TestListenBindsLoopbackOnly(t *testing.T) {
	l, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if ip := l.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() || l.Addr().(*net.TCPAddr).Port == 0 {
		t.Fatalf("listening on %v", l.Addr())
	}
}

func TestRequestBodiesAreStrict(t *testing.T) {
	_, ts := newServer(t, nil)
	for _, body := range []string{"not json", `{"unknown":1}`, strings.Repeat("x", 70<<10)} {
		if resp, _ := do(t, ts, "POST", "/runs", body, true); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %.20q: %d", body, resp.StatusCode)
		}
	}
	if resp, _ := do(t, ts, "POST", "/runs", `{"graph_path":""}`, true); resp.StatusCode != http.StatusBadRequest {
		t.Fatal("empty graph_path accepted")
	}
}

func TestStartErrorsMapToStatusAndDiagnostics(t *testing.T) {
	diag := []graph.Diagnostic{{Severity: graph.SeverityError, Code: "E_X", Message: "bad"}}
	_, ts := newServer(t, func(context.Context, string, []string, func(events.Event) error, runtime.DecisionProvider) error {
		return &StartError{Status: http.StatusUnprocessableEntity, Message: "graph is invalid", Diagnostics: diag}
	})
	resp, body := do(t, ts, "POST", "/runs", `{"graph_path":"/x"}`, true)
	if resp.StatusCode != 422 || !strings.Contains(body, `"E_X"`) || !strings.Contains(body, "graph is invalid") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func fakeEvent(seq uint64, typ events.Type, p events.Payload) events.Event {
	return events.Event{SchemaVersion: events.SchemaVersion, Type: typ, Timestamp: time.Now().UTC(), Sequence: seq, RunID: runID, GraphID: "g", GraphVersion: "v", Payload: p}
}

// Recorded values (ADR-0023) are read through the existing events route: a run
// of an earlier process replays them from the store, behind the same token,
// including a maximal value whose JSON escaping is six times its size.
func TestStoredRecordedValuesReplayThroughTheEventsRoute(t *testing.T) {
	s, ts := newServer(t, nil)
	big := strings.Repeat("\x01", events.MaxValueBytes)
	sum := sha256.Sum256([]byte(big))
	value := fakeEvent(2, events.ValueRecorded, events.ValueRecordedPayload{NodeRunID: "nr", Port: "out", Type: "string", SizeBytes: int64(len(big)), SHA256: hex.EncodeToString(sum[:]), Value: &big})
	value.NodeID, value.NodeRunID = "n", "nr"
	w := s.Store.NewWriter([]byte("schema_version: 0\n"))
	for _, e := range []events.Event{fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}), value, fakeEvent(3, events.RunFinished, events.RunFinishedPayload{Status: "succeeded"})} {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if resp, _ := do(t, ts, "GET", "/runs/"+runID+"/events", "", false); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("values readable without token: %d", resp.StatusCode)
	}
	resp, body := do(t, ts, "GET", "/runs/"+runID+"/events", "", true)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "event: end") {
		t.Fatalf("%d %.200s", resp.StatusCode, body)
	}
	var got *events.ValueRecordedPayload
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: {\"event_schema_version\"") {
			var e events.Event
			if err := json.Unmarshal([]byte(line[6:]), &e); err != nil {
				t.Fatal(err)
			}
			if p, ok := e.Payload.(events.ValueRecordedPayload); ok {
				got = &p
			}
		}
	}
	if got == nil || got.Value == nil || *got.Value != big || got.Truncated {
		t.Fatal("recorded value did not replay intact")
	}
	if _, list := do(t, ts, "GET", "/runs", "", true); !strings.Contains(list, `"graph_version":"v"`) {
		t.Fatalf("summary lacks graph_version: %s", list)
	}
}

// A client that connects mid-run must see every event exactly once, in order.
func TestStreamReplaysThenFollowsWithoutGapsOrDuplicates(t *testing.T) {
	release := make(chan struct{})
	midway := make(chan struct{})
	_, ts := newServer(t, func(ctx context.Context, _ string, _ []string, persist func(events.Event) error, _ runtime.DecisionProvider) error {
		persist(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}))
		persist(fakeEvent(2, events.NodeReady, events.NodeReadyPayload{}))
		close(midway)
		<-release
		persist(fakeEvent(3, events.NodeStarted, events.NodeStartedPayload{}))
		persist(fakeEvent(4, events.RunFinished, events.RunFinishedPayload{Status: "succeeded"}))
		return nil
	})
	_, body := do(t, ts, "POST", "/runs", `{"graph_path":"/x"}`, true)
	var started struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(body), &started); err != nil || started.RunID != runID {
		t.Fatalf("%v %s", err, body)
	}
	<-midway
	req, _ := http.NewRequest("GET", ts.URL+"/runs/"+runID+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	got := make(chan []uint64, 1)
	go func() {
		var seqs []uint64
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: {\"") {
				var e events.Event
				if err := json.Unmarshal([]byte(line[6:]), &e); err == nil {
					seqs = append(seqs, e.Sequence)
				}
			}
		}
		got <- seqs
	}()
	close(release)
	select {
	case seqs := <-got:
		if len(seqs) != 4 {
			t.Fatalf("sequences %v", seqs)
		}
		for i, s := range seqs {
			if s != uint64(i+1) {
				t.Fatalf("gap or duplicate: %v", seqs)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not end after run_finished")
	}
}

func TestStreamServesFinishedRunsFromTheStoreAndUnknownRunsAre404(t *testing.T) {
	s, ts := newServer(t, nil)
	w := s.Store.NewWriter([]byte("schema_version: 0\n"))
	w.Append(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 0}))
	w.Append(fakeEvent(2, events.RunFinished, events.RunFinishedPayload{Status: "succeeded"}))
	w.Close()
	resp, body := do(t, ts, "GET", "/runs/"+runID+"/events", "", true)
	if resp.StatusCode != 200 || strings.Count(body, "event: event") != 2 || !strings.Contains(body, "event: end") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, ts, "GET", "/runs/44444444-4444-4444-8444-444444444444/events", "", true); resp.StatusCode != 404 {
		t.Fatalf("unknown run: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts, "GET", "/runs/..%2f..%2fetc/events", "", true); resp.StatusCode != 404 {
		t.Fatalf("traversal: %d", resp.StatusCode)
	}
}

func TestDecisionsAreIdempotentScopedToTheRunAndValidated(t *testing.T) {
	// Drive the hub through the HTTP layer with a real pending approval.
	s2 := &Server{Token: token, Store: store.Store{Dir: t.TempDir()}, Hub: NewHub()}
	ts2 := httptest.NewServer(s2.Handler())
	defer ts2.Close()
	decided := make(chan run.Decision, 1)
	go func() {
		d, _ := s2.Hub.Decide(context.Background(), run.ApprovalRequest{RunID: runID, NodeID: "ask", NodeRunID: "nr-1", Prompt: "ship?"})
		decided <- d
	}()
	waitPending(t, s2.Hub, 1)
	if p := s2.Hub.Pending(); len(p) != 1 || p[0].Prompt != "ship?" {
		t.Fatalf("pending %+v", p)
	}
	path := "/runs/" + runID + "/decisions/nr-1"
	if resp, _ := do(t, ts2, "POST", "/runs/55555555-5555-4555-8555-555555555555/decisions/nr-1", `{"decision":"approved"}`, true); resp.StatusCode != 404 {
		t.Fatalf("a different run must not be able to decide: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts2, "POST", path, `{"decision":"maybe"}`, true); resp.StatusCode != 400 {
		t.Fatalf("unknown decision: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts2, "POST", path, `{"decision":"approved","comment":"`+strings.Repeat("c", 3000)+`"}`, true); resp.StatusCode != 400 {
		t.Fatalf("oversized comment: %d", resp.StatusCode)
	}
	if resp, body := do(t, ts2, "POST", path, `{"decision":"approved","comment":"fine"}`, true); resp.StatusCode != 200 || !strings.Contains(body, "approved") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if d := <-decided; !d.Approve || d.Comment != "fine" {
		t.Fatalf("provider received %+v", d)
	}
	resp, body := do(t, ts2, "POST", path, `{"decision":"rejected"}`, true)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, `"decision":"approved"`) {
		t.Fatalf("repeat must return 409 with the original: %d %s", resp.StatusCode, body)
	}
	if len(s2.Hub.Pending()) != 0 {
		t.Fatal("decided approval still pending")
	}
}

func TestCancelledWaitRemovesThePendingApproval(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := h.Decide(ctx, run.ApprovalRequest{RunID: runID, NodeRunID: "nr"}); err == nil {
			t.Error("cancelled wait returned no error")
		}
	}()
	waitPending(t, h, 1)
	cancel()
	wg.Wait()
	if len(h.Pending()) != 0 {
		t.Fatal("cancelled approval is still offered to the user")
	}
	if _, _, err := h.Submit(runID, "nr", true, ""); err != ErrNotPending {
		t.Fatalf("late decision after cancellation: %v", err)
	}
}

func TestRunDetailIncludesGraphSummaryFromTheSnapshot(t *testing.T) {
	s, ts := newServer(t, nil)
	source := "schema_version: 0\nid: g\ninputs: {}\nnodes:\n  a: {type: shell, outputs: {stdout: {type: string}}}\n  b: {type: shell, inputs: {x: {type: string}}, outputs: {stdout: {type: string}}}\n  c: {type: shell, inputs: {x: {type: string}, y: {type: string}}}\nedges:\n  - {from: a.outputs.stdout, to: b.inputs.x}\n  - {from: a.outputs.stdout, to: c.inputs.x}\n  - {from: b.outputs.stdout, to: c.inputs.y}\noutputs: {}\n"
	w := s.Store.NewWriter([]byte(source))
	w.Append(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 3}))
	w.Close()
	resp, body := do(t, ts, "GET", "/runs/"+runID, "", true)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var detail struct {
		Summary GraphSummary `json:"graph_summary"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Summary.Nodes) != 3 || len(detail.Summary.Edges) != 3 || detail.Summary.Edges[0] != (GraphEdge{From: "a", To: "b"}) {
		t.Fatalf("%+v", detail.Summary)
	}
	if got := summarizeGraph([]byte("not: [valid")); len(got.Nodes) != 0 || got.Edges == nil {
		t.Fatalf("an unreadable snapshot must yield an empty summary, got %+v", got)
	}
}

func waitPending(t *testing.T, h *Hub, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.WaitPending(ctx, n); err != nil {
		t.Fatalf("approval never became pending: %v", err)
	}
}

// Submit may be accepted at the very moment the run is cancelled. Both select
// cases are then ready; the accepted decision must still reach the run, because
// the client was told it was recorded (finding: decision lost on cancel race).
func TestDecisionAcceptedDuringCancellationIsNotLost(t *testing.T) {
	for i := 0; i < 200; i++ {
		h := NewHub()
		ctx, cancel := context.WithCancel(context.Background())
		h.afterRegister = func() {
			if _, _, err := h.Submit(runID, "nr", true, "yes"); err != nil {
				t.Errorf("submit: %v", err)
			}
			cancel() // both the decision and the cancellation are now ready
		}
		d, err := h.Decide(ctx, run.ApprovalRequest{RunID: runID, NodeID: "ask", NodeRunID: "nr"})
		if err != nil || !d.Approve || d.Comment != "yes" {
			t.Fatalf("iteration %d: decision lost: %+v %v", i, d, err)
		}
	}
}

func TestDecisionForAnotherRunNeverRevealsAnEarlierDecision(t *testing.T) {
	h := NewHub()
	go h.Decide(context.Background(), run.ApprovalRequest{RunID: runID, NodeID: "ask", NodeRunID: "nr-9"})
	waitPending(t, h, 1)
	if _, already, err := h.Submit(runID, "nr-9", true, "secret comment"); err != nil || already {
		t.Fatalf("%v %v", err, already)
	}
	rec, already, err := h.Submit("55555555-5555-4555-8555-555555555555", "nr-9", false, "")
	if err != ErrNotPending || already || rec.Comment != "" {
		t.Fatalf("another run saw %+v already=%v err=%v", rec, already, err)
	}
	if rec, already, err := h.Submit(runID, "nr-9", false, ""); err != nil || !already || rec.Comment != "secret comment" {
		t.Fatalf("the owning run must still get the original: %+v %v %v", rec, already, err)
	}
}

// A run that cannot be stored is stopped and flagged; a stream must end with an
// error event, never with a normal end (the UI shows "connection lost").
func TestUnstorableRunEndsStreamWithErrorAndIsFlagged(t *testing.T) {
	s, ts := newServer(t, func(_ context.Context, _ string, _ []string, persist func(events.Event) error, _ runtime.DecisionProvider) error {
		persist(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}))
		return &StorageError{Err: errors.New("disk full")}
	})
	w := s.Store.NewWriter([]byte("schema_version: 0\n")) // the store holds the partial log
	w.Append(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}))
	w.Close()
	if resp, body := do(t, ts, "POST", "/runs", `{"graph_path":"/x"}`, true); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	live, ok := s.Hub.Live(runID)
	if !ok {
		t.Fatal("run not registered")
	}
	deadline := time.After(5 * time.Second)
	for live.Err() == "" {
		_, _, changed := live.From(1 << 20)
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("run was never flagged")
		}
	}
	resp, body := do(t, ts, "GET", "/runs/"+runID+"/events", "", true)
	if resp.StatusCode != 200 || !strings.Contains(body, "event: error") || strings.Contains(body, "event: end") {
		t.Fatalf("stream must end with an error event:\n%s", body)
	}
	if _, body := do(t, ts, "GET", "/runs", "", true); !strings.Contains(body, "could not be stored") {
		t.Fatalf("runs list must flag the run: %s", body)
	}
}

func TestListShowsCorruptRunsInsteadOfDroppingThem(t *testing.T) {
	s, ts := newServer(t, nil)
	dir := s.Store.Dir + "/" + runID
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(dir+"/graph.yaml", []byte("x"), 0o600)
	os.WriteFile(dir+"/events.jsonl", []byte("garbage\n{}\n"), 0o600)
	_, body := do(t, ts, "GET", "/runs", "", true)
	if !strings.Contains(body, `"status":"corrupt"`) || !strings.Contains(body, runID) {
		t.Fatalf("%s", body)
	}
}

// Only a storage failure may claim "could not be stored"; any other error after
// a run began is not mislabelled.
func TestNonStorageFailureAfterStartIsNotFlaggedAsUnstored(t *testing.T) {
	s, ts := newServer(t, func(_ context.Context, _ string, _ []string, persist func(events.Event) error, _ runtime.DecisionProvider) error {
		persist(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}))
		persist(fakeEvent(2, events.RunFinished, events.RunFinishedPayload{Status: "failed"}))
		return errors.New("something else broke")
	})
	if resp, body := do(t, ts, "POST", "/runs", `{"graph_path":"/x"}`, true); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	live, _ := s.Hub.Live(runID)
	<-time.After(0)
	if live.Err() != "" {
		t.Fatalf("a non-storage error was reported as a storage failure: %q", live.Err())
	}
}

func TestNoNewRunIsAdmittedOnceShutdownBegan(t *testing.T) {
	started := false
	s, ts := newServer(t, func(context.Context, string, []string, func(events.Event) error, runtime.DecisionProvider) error {
		started = true
		return nil
	})
	s.mu.Lock()
	s.draining = true
	s.mu.Unlock()
	if resp, body := do(t, ts, "POST", "/runs", `{"graph_path":"/x"}`, true); resp.StatusCode != http.StatusServiceUnavailable || started {
		t.Fatalf("%d %s started=%v", resp.StatusCode, body, started)
	}
}
