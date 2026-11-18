package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
)

// F3.2 task 8: the API only surfaces what the store derives.

type listedRun struct {
	RunID                 string `json:"run_id"`
	Status                string `json:"status"`
	InterruptedAtSequence uint64 `json:"interrupted_at_sequence"`
	Error                 string `json:"error"`
}

func listedRuns(t *testing.T, ts *httptest.Server) []listedRun {
	t.Helper()
	resp, body := do(t, ts, "GET", "/runs", "", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var doc struct {
		Runs []listedRun `json:"runs"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Runs
}

func TestRunSummaryCarriesInterruptedAtSequence(t *testing.T) {
	s, ts := newServer(t, nil)
	w := s.Store.NewWriter([]byte("schema_version: 0\n"))
	for _, e := range []events.Event{fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}), fakeEvent(2, events.NodeReady, events.NodeReadyPayload{}), fakeEvent(5, events.NodeStarted, events.NodeStartedPayload{})} {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil { // the writer is gone and no terminal event was written
		t.Fatal(err)
	}
	runs := listedRuns(t, ts)
	if len(runs) != 1 || runs[0].Status != "interrupted" || runs[0].InterruptedAtSequence != 5 {
		t.Fatalf("list: %+v", runs)
	}
	resp, body := do(t, ts, "GET", "/runs/"+runID, "", true)
	var detail struct {
		Run listedRun `json:"run"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil || resp.StatusCode != 200 || detail.Run.Status != "interrupted" || detail.Run.InterruptedAtSequence != 5 {
		t.Fatalf("get: %d %s (%v)", resp.StatusCode, body, err)
	}
}

func TestUnsupportedRunIsListedAndGetReportsIt(t *testing.T) {
	s, ts := newServer(t, nil)
	w := s.Store.NewWriter([]byte("schema_version: 0\n"))
	if err := w.Append(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1})); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Store.Dir, runID, "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	newer := strings.ReplaceAll(string(data), fmt.Sprintf(`"event_schema_version":%d`, events.SchemaVersion), fmt.Sprintf(`"event_schema_version":%d`, events.SchemaVersion+1))
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}

	runs := listedRuns(t, ts)
	if len(runs) != 1 || runs[0].Status != "unsupported" || !strings.Contains(runs[0].Error, fmt.Sprint(events.SchemaVersion+1)) {
		t.Fatalf("list: %+v", runs)
	}
	for _, route := range []string{"/runs/" + runID, "/runs/" + runID + "/events"} {
		resp, body := do(t, ts, "GET", route, "", true)
		var failure struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal([]byte(body), &failure); err != nil {
			t.Fatalf("%s: %s", route, body)
		}
		if resp.StatusCode != http.StatusUnprocessableEntity || failure.Error.Code != "unsupported_schema" || !strings.Contains(failure.Error.Message, "upgrade") {
			t.Fatalf("%s: %d %s", route, resp.StatusCode, body)
		}
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != newer {
		t.Fatal("an unsupported run must never be modified")
	}
	// an unknown run is still a plain 404
	if resp, _ := do(t, ts, "GET", "/runs/44444444-4444-4444-8444-444444444444", "", true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown run: %d", resp.StatusCode)
	}
}

func TestHubOverlayDoesNotMaskARunLiveInAnotherProcess(t *testing.T) {
	s, ts := newServer(t, nil)
	// A writer holding the run lock stands for another process: this server's
	// hub has never heard of the run, only the store knows it is live.
	w := s.Store.NewWriter([]byte("schema_version: 0\n"))
	if err := w.Append(fakeEvent(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1})); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, known := s.Hub.Live(runID); known {
		t.Fatal("the hub must not know a run of another process")
	}
	runs := listedRuns(t, ts)
	if len(runs) != 1 || runs[0].Status != "running" || runs[0].InterruptedAtSequence != 0 {
		t.Fatalf("a run live in another process reads running, never interrupted: %+v", runs)
	}
	_, body := do(t, ts, "GET", "/runs/"+runID, "", true)
	if !strings.Contains(body, `"status":"running"`) || strings.Contains(body, "interrupted") {
		t.Fatalf("%s", body)
	}
}
