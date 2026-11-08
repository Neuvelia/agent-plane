package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/history"
)

const historicalRunID = "11111111-1111-4111-8111-111111111111"

func historyHandlerStore(t *testing.T) *history.Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func historyJSON(t *testing.T, source string) history.SafeJSON {
	t.Helper()
	value, err := history.ParseSafeJSON([]byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func historyRequest(t *testing.T, handler Handler, source string) (map[string]json.RawMessage, *ServiceError) {
	t.Helper()
	result, serviceErr := handler(context.Background(), json.RawMessage(source))
	if serviceErr != nil {
		return nil, serviceErr
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded, nil
}

func archiveHistoryFixture(t *testing.T, store *history.Store) {
	t.Helper()
	ctx := context.Background()
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	marker := history.LaunchRecord{RunID: historicalRunID, ProjectID: "22222222-2222-4222-8222-222222222222", WorkspaceID: "33333333-3333-4333-8333-333333333333", DocumentID: "44444444-4444-4444-8444-444444444444", GraphID: "graph", GraphHash: "sha256:graph", StartedAt: started,
		Graph: historyJSON(t, `[1,"graph"]`), Launch: historyJSON(t, `[1,[]]`), Completeness: historyJSON(t, `[1,true,true]`)}
	if err := store.Launch(ctx, marker); err != nil {
		t.Fatal(err)
	}
	content, err := history.ParseSafeEvidence([]byte("safe diff"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureEvidence(ctx, history.EvidenceCapture{EvidenceID: "evidence-1", RunID: historicalRunID, AttemptID: "attempt-1", NodeRunID: "node-run-1", Kind: "diff", SourcePath: "file.txt", CaptureGroup: "group-1", CreatedAt: started, Content: &content}); err != nil {
		t.Fatal(err)
	}
	final := history.Finalization{RunID: historicalRunID, EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 3,
		Result: historyJSON(t, `[1,"result"]`), Completeness: historyJSON(t, `[1,true,true,true,false,true]`),
		Events:         []history.Event{{Sequence: 1, Projection: historyJSON(t, `{"type":"run_started"}`)}, {Sequence: 3, Projection: historyJSON(t, `{"type":"run_finished"}`)}},
		OmissionRanges: []history.OmissionRange{{From: 2, To: 2, Reason: "not_retained"}},
		Nodes:          []history.NodeOutcome{{NodeRunID: "node-run-1", NodeID: "node", Status: "succeeded", Projection: historyJSON(t, `[1,"node"]`)}},
		Attempts:       []history.AttemptOutcome{{AttemptID: "attempt-1", NodeRunID: "node-run-1", Status: "accepted", Projection: historyJSON(t, `[1,"attempt"]`)}}}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryHandlersExposeTypedSafePages(t *testing.T) {
	store := historyHandlerStore(t)
	archiveHistoryFixture(t, store)
	handlers := NewHistoryHandlers(store, nil, nil)
	list, serviceErr := historyRequest(t, handlers["history.list"], `{"limit":1}`)
	if serviceErr != nil || !bytes.Contains(list["runs"], []byte(historicalRunID)) {
		t.Fatalf("list=%v error=%v", list, serviceErr)
	}
	if !bytes.Contains(list["runs"], []byte(`"completeness":[1,true,true,true,false,true]`)) || !bytes.Contains(list["runs"], []byte(`"completeness_digest":"`)) {
		t.Fatalf("list completeness missing: %v", list)
	}
	get, serviceErr := historyRequest(t, handlers["history.get"], `{"run_id":"`+historicalRunID+`"}`)
	if serviceErr != nil || string(get["graph"]) != `[1,"graph"]` || string(get["result"]) != `[1,"result"]` {
		t.Fatalf("get=%v error=%v", get, serviceErr)
	}
	if !bytes.Contains(get["run"], []byte(`"workspace_id":"33333333-3333-4333-8333-333333333333"`)) || get["workspace_id"] != nil {
		t.Fatalf("run metadata is not nested: %v", get)
	}
	sum := sha256.Sum256(get["graph"])
	if string(get["graph_digest"]) != `"`+hex.EncodeToString(sum[:])+`"` || bytes.Contains(get["graph"], []byte("base64")) {
		t.Fatalf("graph digest/raw=%v", get)
	}
	outcomes, serviceErr := historyRequest(t, handlers["history.outcomes"], `{"run_id":"`+historicalRunID+`","limit":1}`)
	if serviceErr != nil || !bytes.Contains(outcomes["nodes"], []byte(`"projection":[1,"node"]`)) || !bytes.Contains(outcomes["attempts"], []byte(`"projection":[1,"attempt"]`)) {
		t.Fatalf("outcomes=%v error=%v", outcomes, serviceErr)
	}
	events, serviceErr := historyRequest(t, handlers["history.events"], `{"run_id":"`+historicalRunID+`","after_sequence":0,"limit":3}`)
	if serviceErr != nil || !bytes.Contains(events["omissions"], []byte(`"from":2`)) || !bytes.Contains(events["events"], []byte(`"projection":{"type":"run_started"}`)) || string(events["complete"]) != "false" || len(events["completeness_digest"]) < 66 {
		t.Fatalf("events=%v error=%v", events, serviceErr)
	}
	evidence, serviceErr := historyRequest(t, handlers["history.evidence"], `{"evidence_id":"evidence-1","offset":0,"limit":100}`)
	if serviceErr != nil || string(evidence["bytes_base64"]) != `"c2FmZSBkaWZm"` || bytes.Contains(mustJSON(t, evidence), []byte("evidence/")) {
		t.Fatalf("evidence=%v error=%v", evidence, serviceErr)
	}
	if _, serviceErr := historyRequest(t, handlers["history.evidence"], `{"evidence_id":"evidence-1","offset":100,"limit":1}`); serviceErr == nil || serviceErr.Code != "invalid_request" {
		t.Fatalf("past-end evidence offset: %v", serviceErr)
	}
	descriptors, serviceErr := historyRequest(t, handlers["history.evidence.list"], `{"run_id":"`+historicalRunID+`","limit":1}`)
	if serviceErr != nil || !bytes.Contains(descriptors["descriptors"], []byte(`"evidence_id":"evidence-1"`)) || !bytes.Contains(descriptors["descriptors"], []byte(`"source_path":"file.txt"`)) || !bytes.Contains(descriptors["descriptors"], []byte(`"capture_group":"group-1"`)) || bytes.Contains(mustJSON(t, descriptors), []byte("relative_path")) {
		t.Fatalf("descriptors=%v error=%v", descriptors, serviceErr)
	}
	pin, serviceErr := historyRequest(t, handlers["history.pin"], `{"run_id":"`+historicalRunID+`","expected_revision":1,"pinned":true}`)
	if serviceErr != nil || string(pin["revision"]) != "2" || string(pin["pinned"]) != "true" {
		t.Fatalf("pin=%v error=%v", pin, serviceErr)
	}
}

func TestHistoryHandlersRejectUnknownAndUnsafeRequests(t *testing.T) {
	store := historyHandlerStore(t)
	handlers := NewHistoryHandlers(store, nil, nil)
	if empty, serviceErr := historyRequest(t, handlers["history.list"], `{"limit":1}`); serviceErr != nil || string(empty["runs"]) != `[]` {
		t.Fatalf("empty list=%v error=%v", empty, serviceErr)
	}
	for _, tc := range []struct{ method, params string }{
		{"history.list", `{"limit":1,"extra":true}`},
		{"history.list", `{"limit":1,"limit":2}`},
		{"history.list", `{"limit":0}`},
		{"history.list", `{"limit":1,"started_from":"tomorrow"}`},
		{"history.get", `{"run_id":"../../private"}`},
		{"history.events", `{"run_id":"` + historicalRunID + `","after_sequence":-1,"limit":1}`},
		{"history.evidence", `{"evidence_id":"evidence-1","offset":0,"limit":1048577}`},
		{"history.evidence.list", `{"run_id":"` + historicalRunID + `","limit":0}`},
		{"history.pin", `{"run_id":"` + historicalRunID + `","expected_revision":0,"pinned":"yes"}`},
	} {
		_, err := historyRequest(t, handlers[tc.method], tc.params)
		if err == nil || err.Code != "invalid_request" {
			t.Fatalf("%s %s accepted: %v", tc.method, tc.params, err)
		}
	}
	unavailable := NewHistoryHandlers(nil, nil, nil)
	_, err := historyRequest(t, unavailable["history.list"], `{"limit":1}`)
	if err == nil || err.Code != "history_unavailable" {
		t.Fatalf("missing store: %v", err)
	}
	newer := NewHistoryHandlers(nil, &RunService{historyOpenErr: history.ErrNewerSchema}, nil)
	_, err = historyRequest(t, newer["history.list"], `{"limit":1}`)
	if err == nil || err.Code != "history_version_unsupported" {
		t.Fatalf("newer store: %v", err)
	}
}

func TestHistoryPruneUsesRevisionedSettingsAndKeepsInterrupted(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, 0, -40)
	completeID := "55555555-5555-4555-8555-555555555555"
	interruptedID := "66666666-6666-4666-8666-666666666666"
	for _, id := range []string{completeID, interruptedID} {
		if err := store.Launch(context.Background(), history.LaunchRecord{RunID: id, ProjectID: "22222222-2222-4222-8222-222222222222", WorkspaceID: "33333333-3333-4333-8333-333333333333", DocumentID: "44444444-4444-4444-8444-444444444444", GraphID: "graph", GraphHash: "hash", StartedAt: old, Graph: historyJSON(t, `[1]`), Launch: historyJSON(t, `[1]`), Completeness: historyJSON(t, `[1]`)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Finalize(context.Background(), history.Finalization{RunID: completeID, EndedAt: old.Add(time.Hour), RuntimeState: "succeeded", Watermark: 1, Result: historyJSON(t, `[1]`), Completeness: historyJSON(t, `[1]`), Events: []history.Event{{Sequence: 1, Projection: historyJSON(t, `{"type":"run_finished"}`)}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, _, _ := registryFixture(t)
	handlers := NewHistoryHandlers(store, nil, registry)
	keep, serviceErr := historyRequest(t, handlers["history.prune"], `{"keep_all":true}`)
	if serviceErr != nil || string(keep["removed_runs"]) != "0" {
		t.Fatalf("keep-all=%v error=%v", keep, serviceErr)
	}
	pruned, serviceErr := historyRequest(t, handlers["history.prune"], `{}`)
	if serviceErr != nil || string(pruned["removed_runs"]) != "1" || string(pruned["skipped_interrupted"]) != "1" {
		t.Fatalf("prune=%v error=%v", pruned, serviceErr)
	}
	if _, err := store.Get(context.Background(), completeID); err != history.ErrNotFound {
		t.Fatalf("complete run retained: %v", err)
	}
	if item, err := store.Get(context.Background(), interruptedID); err != nil || item.StorageState != history.Interrupted {
		t.Fatalf("interrupted run=%+v err=%v", item, err)
	}
	for _, bad := range []string{`{"retention_days":0}`, `{"retention_days":30,"keep_all":true}`, `{"keep_all":false}`, `{"retention_days":"30"}`} {
		_, serviceErr := historyRequest(t, handlers["history.prune"], bad)
		if serviceErr == nil || serviceErr.Code != "invalid_request" {
			t.Fatalf("bad prune %s: %v", bad, serviceErr)
		}
	}
}

func TestHistoryGetRejectsTamperedStoredProjection(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	archiveHistoryFixture(t, store)
	db, err := sql.Open("sqlite", filepath.Join(directory, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE runs SET safe_graph_json=? WHERE run_id=?", `[1,"modified"]`, historicalRunID); err != nil {
		t.Fatal(err)
	}
	_, serviceErr := historyRequest(t, NewHistoryHandlers(store, nil, nil)["history.get"], `{"run_id":"`+historicalRunID+`"}`)
	if serviceErr == nil || serviceErr.Code != "history_unavailable" {
		t.Fatalf("tampered projection shown: %v", serviceErr)
	}
	if _, err := db.Exec("UPDATE runs SET completeness_json=? WHERE run_id=?", `[1,"tampered"]`, historicalRunID); err != nil {
		t.Fatal(err)
	}
	_, serviceErr = historyRequest(t, NewHistoryHandlers(store, nil, nil)["history.list"], `{"limit":1}`)
	if serviceErr == nil || serviceErr.Code != "history_unavailable" {
		t.Fatalf("tampered list completeness shown: %v", serviceErr)
	}
}

func TestHistoryListBoundsCombinedCompletenessBeforeFraming(t *testing.T) {
	store := historyHandlerStore(t)
	large := historyJSON(t, `[1,"`+strings.Repeat("x", 2<<20)+`"]`)
	for _, id := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"} {
		if err := store.Launch(context.Background(), history.LaunchRecord{RunID: id, ProjectID: "44444444-4444-4444-8444-444444444444", WorkspaceID: "55555555-5555-4555-8555-555555555555", DocumentID: "66666666-6666-4666-8666-666666666666", GraphID: "graph", GraphHash: "hash", StartedAt: time.Now().UTC(), Graph: historyJSON(t, `[1]`), Launch: historyJSON(t, `[1]`), Completeness: large}); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHistoryHandlers(store, nil, nil)["history.list"]
	if _, serviceErr := historyRequest(t, handler, `{"limit":3}`); serviceErr == nil || serviceErr.Code != "page_too_large" {
		t.Fatalf("oversized list: %v", serviceErr)
	}
	result, serviceErr := handler(context.Background(), json.RawMessage(`{"limit":1}`))
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	var frame bytes.Buffer
	if err := WriteFrame(&frame, Response{ProtocolVersion: ProtocolVersion, RequestID: "history", Result: mustJSON(t, result)}); err != nil {
		t.Fatalf("limit-one list not frameable: %v", err)
	}
}

func TestHistoryEvidenceListUsesActiveFailureIndexWhenWriteFailed(t *testing.T) {
	store := historyHandlerStore(t)
	controller := &captureController{descriptors: []history.EvidenceDescriptor{{EvidenceID: "active-failure", RunID: historicalRunID, AttemptID: "attempt", Kind: "diff", Availability: "unavailable", OmissionReason: "capture_failed", CreatedAt: time.Now().UTC()}}, uncommitted: map[string]bool{"active-failure": true}, generation: 1, complete: false, failed: true}
	runs := &RunService{byID: map[events.RunID]*ownedRun{events.RunID(historicalRunID): {runID: events.RunID(historicalRunID), capture: controller, historyAvailability: HistoryUnavailable}}}
	result, serviceErr := historyRequest(t, NewHistoryHandlers(store, runs, nil)["history.evidence.list"], `{"run_id":"`+historicalRunID+`","limit":1}`)
	if serviceErr != nil || string(result["complete"]) != "false" || !bytes.Contains(result["descriptors"], []byte(`"omission_reason":"capture_failed"`)) {
		t.Fatalf("active failure descriptor=%v error=%v", result, serviceErr)
	}
	chunk, serviceErr := historyRequest(t, NewHistoryHandlers(store, runs, nil)["history.evidence"], `{"evidence_id":"active-failure","offset":0,"limit":1}`)
	if serviceErr != nil || string(chunk["availability"]) != `"unavailable"` || string(chunk["omission_reason"]) != `"capture_failed"` || string(chunk["total_length"]) != "0" || string(chunk["bytes_base64"]) != `""` || string(chunk["end"]) != "true" {
		t.Fatalf("active failure chunk=%v error=%v", chunk, serviceErr)
	}
	if _, serviceErr := historyRequest(t, NewHistoryHandlers(store, nil, nil)["history.evidence"], `{"evidence_id":"active-failure","offset":0,"limit":1}`); serviceErr == nil || serviceErr.Code != "not_found" {
		t.Fatalf("uncommitted descriptor survived owner loss: %v", serviceErr)
	}
}
