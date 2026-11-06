package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/runtime"
)

const replayRunID = events.RunID("00000000-0000-4000-8000-000000000099")

func replayFixture(t *testing.T, reader events.TraceReader) *RunService {
	t.Helper()
	service := NewRunService(context.Background(), nil, nil)
	service.byID[replayRunID] = &ownedRun{runID: replayRunID, reader: reader,
		graphID: "graph", graphVersion: "version", state: runtime.RunRunning}
	t.Cleanup(service.Close)
	return service
}

func appendReplayEvent(t *testing.T, collector *events.Collector, event events.Event) {
	t.Helper()
	if _, err := collector.Append(event); err != nil {
		t.Fatal(err)
	}
}

func replayEvent(t *testing.T, raw json.RawMessage) events.Event {
	t.Helper()
	var event events.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func TestRunEventsPagesAndResumesAfterAppend(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	appendReplayEvent(t, collector, events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	appendReplayEvent(t, collector, events.Event{Type: events.NodeReady, Payload: events.NodeReadyPayload{}})
	service := replayFixture(t, collector)
	first, err := service.Events(replayRunID, 0, 1)
	if err != nil || first.RunID != replayRunID || first.AfterSequence != 0 ||
		len(first.Events) != 1 || len(first.EventHashes) != 1 || replayEvent(t, first.Events[0]).Sequence != 1 ||
		first.NextAfter != 1 || !first.HasMore || first.Watermark != 2 || first.Finished {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	resumeAppend := make(chan struct{})
	appendDone := make(chan error, 1)
	go func() {
		<-resumeAppend
		_, err := collector.Append(events.Event{Type: events.NodeStarted, Payload: events.NodeStartedPayload{}})
		appendDone <- err
	}()
	close(resumeAppend)
	if err := <-appendDone; err != nil {
		t.Fatal(err)
	}
	second, err := service.Events(replayRunID, first.NextAfter, 1)
	if err != nil || second.AfterSequence != 1 || len(second.Events) != 1 ||
		replayEvent(t, second.Events[0]).Sequence != 2 || second.NextAfter != 2 || !second.HasMore || second.Watermark != 3 {
		t.Fatalf("second page = %#v, %v", second, err)
	}
	third, err := service.Events(replayRunID, second.NextAfter, 1)
	if err != nil || len(third.Events) != 1 || replayEvent(t, third.Events[0]).Sequence != 3 ||
		third.NextAfter != 3 || third.HasMore {
		t.Fatalf("third page = %#v, %v", third, err)
	}
	first.Events[0] = json.RawMessage(`{}`)
	again, err := service.Events(replayRunID, 0, 1)
	if err != nil || replayEvent(t, again.Events[0]).Type != events.RunStarted {
		t.Fatalf("mutated retained page = %#v, %v", again, err)
	}
}

func TestRunEventsPropagatesSensitiveOmissions(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	appendReplayEvent(t, collector, events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	if _, err := collector.OmitSensitive(); err != nil {
		t.Fatal(err)
	}
	appendReplayEvent(t, collector, events.Event{Type: events.NodeReady, Payload: events.NodeReadyPayload{}})
	service := replayFixture(t, collector)
	first, err := service.Events(replayRunID, 0, 1)
	if err != nil || len(first.Events) != 1 || first.NextAfter != 1 || len(first.Omissions) != 0 {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := service.Events(replayRunID, first.NextAfter, 1)
	want := events.OmissionRange{From: 2, To: 2, Reason: events.OmissionSensitiveIdentity}
	if err != nil || len(second.Events) != 1 || replayEvent(t, second.Events[0]).Sequence != 3 ||
		len(second.Omissions) != 1 || second.Omissions[0] != want || second.NextAfter != 3 {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	encoded, marshalErr := json.Marshal(second)
	if marshalErr != nil || !strings.Contains(string(encoded), `"omissions":[{"from":2,"to":2,"reason":"sensitive_identity"}]`) {
		t.Fatalf("wire page = %s, %v", encoded, marshalErr)
	}
}

func TestRunEventsOmissionAndTerminalSnapshot(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	for i := 0; i <= events.MaxRetainedProgressItems; i++ {
		appendReplayEvent(t, collector, events.Event{Type: events.LogChunk,
			Payload: events.LogChunkPayload{Stream: events.Stdout, Data: []byte("safe")}})
	}
	service := replayFixture(t, collector)
	first, err := service.Events(replayRunID, events.MaxRetainedProgressItems-1, 2)
	if err != nil || len(first.Events) != 1 || replayEvent(t, first.Events[0]).Sequence != events.MaxRetainedProgressItems ||
		first.NextAfter != events.MaxRetainedProgressItems+1 || first.HasMore ||
		first.Stats.RetainedOmittedEvents != 1 || first.Finished {
		t.Fatalf("omission page = %#v, %v", first, err)
	}
	appendReplayEvent(t, collector, events.Event{Type: events.RunFinished,
		Payload: events.RunFinishedPayload{Status: "succeeded"}})
	service.mu.Lock()
	service.byID[replayRunID].state = runtime.RunSucceeded
	service.byID[replayRunID].finished = true
	service.mu.Unlock()
	final, err := service.Events(replayRunID, first.NextAfter, 2)
	if err != nil || !final.Finished || len(final.Events) != 2 ||
		replayEvent(t, final.Events[0]).Type != events.ProgressOmitted || replayEvent(t, final.Events[1]).Type != events.RunFinished ||
		final.HasMore || final.NextAfter != final.Watermark {
		t.Fatalf("final page = %#v, %v", final, err)
	}
	snapshot, snapErr := service.Snapshot(replayRunID)
	if snapErr != nil || !snapshot.Finished || snapshot.State != runtime.RunSucceeded ||
		snapshot.TraceWatermark != final.Watermark ||
		snapshot.DeliveryStats.RetainedOmittedEvents != final.Stats.RetainedOmittedEvents {
		t.Fatalf("terminal snapshot = %#v, %v", snapshot, snapErr)
	}
}

func TestRunEventsUnknownStateDoesNotInventTerminalFact(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	appendReplayEvent(t, collector, events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	service := replayFixture(t, collector)
	service.mu.Lock()
	service.byID[replayRunID].state = "unknown"
	service.mu.Unlock()
	page, err := service.Events(replayRunID, 0, 2)
	if err != nil || page.Finished || len(page.Events) != 1 || replayEvent(t, page.Events[0]).Type != events.RunStarted {
		t.Fatalf("unknown page = %#v, %v", page, err)
	}
	snapshot, err := service.Snapshot(replayRunID)
	if err != nil || snapshot.Finished || snapshot.State != "unknown" {
		t.Fatalf("unknown snapshot = %#v, %v", snapshot, err)
	}
}

func TestRunEventsRejectsUnknownEvictedAndInvalidCursor(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	service := replayFixture(t, collector)
	if _, err := service.Events("00000000-0000-4000-8000-000000000098", 0, 1); err == nil || err.Code != "not_found" {
		t.Fatalf("unknown run = %v", err)
	}
	if _, err := service.Events(replayRunID, 1, 1); err == nil || err.Code != "invalid_request" || strings.Contains(err.Message, "1") {
		t.Fatalf("future cursor = %v", err)
	}
	service.mu.Lock()
	service.byID[replayRunID].reader = nil
	service.mu.Unlock()
	if _, err := service.Events(replayRunID, 0, 1); err == nil || err.Code != "unavailable" {
		t.Fatalf("evicted trace = %v", err)
	}
}

func TestRunEventsHandlerStrictParameters(t *testing.T) {
	handler := NewRunHandlers(replayFixture(t, events.NewCollector(replayRunID, "graph", "version")))["runs.events"]
	for _, raw := range []string{
		`{}`, `{"run_id":"bad","after_sequence":0,"limit":1}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","limit":1}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":-1,"limit":1}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":1.5,"limit":1}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":18446744073709551616,"limit":1}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":0,"limit":0}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":0,"limit":257}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":0,"limit":1,"limit":2}`,
		`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":0,"limit":1,"extra":true}`,
	} {
		if _, err := handler(context.Background(), json.RawMessage(raw)); err == nil || err.Code != "invalid_request" {
			t.Fatalf("malformed events request accepted: %s => %v", raw, err)
		}
	}
	valid := json.RawMessage(`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":0,"limit":1}`)
	result, err := handler(context.Background(), valid)
	if err != nil || result.(RunEventsPage).RunID != replayRunID {
		t.Fatalf("valid events request = %#v, %v", result, err)
	}
}

type sizedReplayTrace struct{ values []events.Event }

func (s sizedReplayTrace) Page(after uint64, limit int) (events.TracePage, error) {
	if after > uint64(len(s.values)) || limit < 1 || limit > events.MaxTracePageItems {
		return events.TracePage{}, fmt.Errorf("invalid cursor or limit")
	}
	start := int(after)
	end := start + limit
	if end > len(s.values) {
		end = len(s.values)
	}
	page := events.TracePage{Events: s.values[start:end],
		Watermark: uint64(len(s.values)), HasMore: end < len(s.values)}
	for _, event := range page.Events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return events.TracePage{}, err
		}
		digest := sha256.Sum256(encoded)
		page.EventHashes = append(page.EventHashes, "sha256:"+hex.EncodeToString(digest[:]))
	}
	if page.HasMore {
		page.NextAfter = page.Events[len(page.Events)-1].Sequence
	} else {
		page.NextAfter = page.Watermark
	}
	return page, nil
}

func (s sizedReplayTrace) PageBounded(after uint64, limit, maxEventBytes int) (events.TracePage, error) {
	page, err := s.Page(after, limit)
	if err != nil {
		return page, err
	}
	used := 0
	for i, event := range page.Events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return events.TracePage{}, err
		}
		next := len(encoded)
		if i > 0 {
			next++
		}
		if next > maxEventBytes-used {
			if i == 0 {
				return events.TracePage{}, events.ErrTraceEventTooLarge
			}
			page.Events = page.Events[:i]
			page.EventHashes = page.EventHashes[:i]
			page.NextAfter = page.Events[i-1].Sequence
			page.HasMore = true
			return page, nil
		}
		used += next
	}
	return page, nil
}

func sizedReplayEvent(sequence uint64, size int) events.Event {
	return events.Event{SchemaVersion: events.SchemaVersion, Type: events.NodeEvidence,
		Sequence: sequence, RunID: replayRunID, GraphID: "graph", GraphVersion: "version",
		Payload: events.NodeEvidencePayload{ChangedFiles: []events.ChangedFile{{Path: strings.Repeat("x", size)}}}}
}

func TestRunEventsShrinksPageWithoutSkippingLargeFacts(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	for i := 0; i < 3; i++ {
		appendReplayEvent(t, collector, events.Event{Type: events.NodeEvidence, Payload: events.NodeEvidencePayload{AdapterID: "adapter", ChangedFiles: []events.ChangedFile{{Path: strings.Repeat("x", 5<<19)}}}})
	}
	service := replayFixture(t, collector)
	first, err := service.Events(replayRunID, 0, 3)
	if err != nil || len(first.Events) != 2 || first.NextAfter != 2 || !first.HasMore || first.Watermark != 3 {
		t.Fatalf("shrunk first page = %d facts, cursor %d, more %t, watermark %d, %v",
			len(first.Events), first.NextAfter, first.HasMore, first.Watermark, err)
	}
	encoded, marshalErr := json.Marshal(Response{ProtocolVersion: ProtocolVersion,
		RequestID: strings.Repeat("r", 128), Result: mustJSON(t, first)})
	if marshalErr != nil || len(encoded) > MaxFrameBytes {
		t.Fatalf("first page frame = %d bytes, %v", len(encoded), marshalErr)
	}
	second, err := service.Events(replayRunID, first.NextAfter, 3)
	if err != nil || len(second.Events) != 1 || replayEvent(t, second.Events[0]).Sequence != 3 || second.HasMore || second.NextAfter != 3 {
		t.Fatalf("second page = %d facts, cursor %d, more %t, %v",
			len(second.Events), second.NextAfter, second.HasMore, err)
	}
}

func TestRunEventsOmitsSingleOversizeFactWithCursorProgress(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	appendReplayEvent(t, collector, events.Event{Type: events.NodeEvidence, Payload: events.NodeEvidencePayload{AdapterID: "adapter", ChangedFiles: []events.ChangedFile{{Path: strings.Repeat("x", 5<<20)}}}})
	service := replayFixture(t, collector)
	page, err := service.Events(replayRunID, 0, 1)
	if err != nil || len(page.Events) != 0 || page.NextAfter != 1 || page.HasMore || len(page.Omissions) != 1 || page.Omissions[0].Reason != events.OmissionNotRetained {
		t.Fatalf("oversize fact = %#v, %v", page, err)
	}
}

func TestRunEventsOlderReaderWithoutBoundedCandidateIsUnavailable(t *testing.T) {
	service := replayFixture(t, sizedReplayTrace{values: []events.Event{sizedReplayEvent(1, 1)}})
	if _, err := service.Events(replayRunID, 0, 1); err == nil || err.Code != "unavailable" {
		t.Fatalf("older reader = %v", err)
	}
}

func TestRunEventsRedactsGateEvidenceAndHashesFramedBytes(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	zero := 0
	appendReplayEvent(t, collector, events.Event{Type: events.GateCheck, NodeID: "node", NodeRunID: "nrid",
		Payload: events.GateCheckPayload{Version: 1, Scope: events.CheckScope{CheckedInputs: []string{"candidate"}}, CheckID: "check", Kind: events.CheckCommand, Verdict: events.CheckGreen, Reason: "private <& é\u2028", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckCommand), Source: events.EvidenceSource(events.CheckCommand), ExitCode: &zero, Stdout: []byte("private <&")}}}})
	service := replayFixture(t, collector)
	service.byID[replayRunID].redactions = []string{"private"}
	page, serviceErr := service.Events(replayRunID, 0, 1)
	if serviceErr != nil || len(page.Events) != 1 || len(page.EventHashes) != 1 || len(page.Omissions) != 0 {
		t.Fatalf("safe Gate page = %+v, %v", page, serviceErr)
	}
	if bytes.Contains(page.Events[0], []byte("private")) || bytes.Contains(page.Events[0], []byte("cHJpdmF0ZQ")) {
		t.Fatal("private Gate value entered wire event")
	}
	var safe events.Event
	if err := json.Unmarshal(page.Events[0], &safe); err != nil {
		t.Fatal(err)
	}
	check := safe.Payload.(events.GateCheckPayload)
	if check.Reason != "*** <& é\u2028" || string(check.Evidence[0].Stdout) != "*** <&" {
		t.Fatalf("Gate content = %+v", check)
	}
	handler := NewRunHandlers(service)["runs.events"]
	handled, serviceErr := handler(context.Background(), json.RawMessage(`{"run_id":"00000000-0000-4000-8000-000000000099","after_sequence":0,"limit":1}`))
	if serviceErr != nil {
		t.Fatalf("runs.events handler = %v", serviceErr)
	}
	response := Response{ProtocolVersion: ProtocolVersion, RequestID: "framed", Result: mustJSON(t, handled)}
	var output bytes.Buffer
	if err := WriteFrame(&output, response); err != nil {
		t.Fatal(err)
	}
	framed, err := ReadFrame(&output)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Result struct {
			Events []json.RawMessage `json:"events"`
		} `json:"result"`
	}
	if err := json.Unmarshal(framed, &wire); err != nil || len(wire.Result.Events) != 1 {
		t.Fatalf("framed event decode: %v", err)
	}
	if !bytes.Equal(wire.Result.Events[0], page.Events[0]) {
		t.Fatalf("frame changed canonical event bytes: page=%s framed=%s", page.Events[0], wire.Result.Events[0])
	}
	sum := sha256.Sum256(wire.Result.Events[0])
	if page.EventHashes[0] != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("hash is not for framed event: %q", page.EventHashes[0])
	}
	raw, err := collector.Page(0, 1)
	if err != nil || raw.EventHashes[0] == page.EventHashes[0] {
		t.Fatal("raw collector hash reused")
	}
}

func TestRunEventsAllOmissionsFirstPageAdvancesCursor(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	for i := 0; i < 2; i++ {
		if _, err := collector.OmitSensitive(); err != nil {
			t.Fatal(err)
		}
	}
	service := replayFixture(t, collector)
	page, err := service.Events(replayRunID, 0, 1)
	if err != nil || len(page.Events) != 0 || len(page.Omissions) != 1 || page.Omissions[0] != (events.OmissionRange{From: 1, To: 2, Reason: events.OmissionSensitiveIdentity}) || page.NextAfter != 2 || page.HasMore {
		t.Fatalf("all omission page = %+v, %v", page, err)
	}
}

type appendingReplayReader struct {
	*events.Collector
	appended bool
}

func (r *appendingReplayReader) NextBounded(after uint64, maxRawBytes int) (events.BoundedCandidate, error) {
	candidate, err := r.Collector.NextBounded(after, maxRawBytes)
	if err == nil && !r.appended {
		r.appended = true
		_, err = r.Collector.Append(events.Event{Type: events.NodeStarted, Payload: events.NodeStartedPayload{}})
	}
	return candidate, err
}

func TestRunEventsFreezesFirstWatermarkWhileFactsAppend(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	appendReplayEvent(t, collector, events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	appendReplayEvent(t, collector, events.Event{Type: events.NodeReady, Payload: events.NodeReadyPayload{}})
	service := replayFixture(t, &appendingReplayReader{Collector: collector})
	page, err := service.Events(replayRunID, 0, 3)
	if err != nil || len(page.Events) != 2 || page.Watermark != 2 || page.NextAfter != 2 || page.HasMore || replayEvent(t, page.Events[1]).Sequence != 2 {
		t.Fatalf("first snapshot drifted with append: %+v, %v", page, err)
	}
	next, err := service.Events(replayRunID, page.NextAfter, 3)
	if err != nil || len(next.Events) != 1 || next.Watermark != 3 || replayEvent(t, next.Events[0]).Sequence != 3 {
		t.Fatalf("new snapshot missed appended fact: %+v, %v", next, err)
	}
}

func TestRunEventsClipsLaterCandidateToFrozenOmissionWatermark(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	appendReplayEvent(t, collector, events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	if _, err := collector.OmitSensitive(); err != nil {
		t.Fatal(err)
	}
	service := replayFixture(t, &appendingReplayReader{Collector: collector})
	page, err := service.Events(replayRunID, 0, 3)
	if err != nil || len(page.Events) != 1 || len(page.Omissions) != 1 || page.Omissions[0] != (events.OmissionRange{From: 2, To: 2, Reason: events.OmissionSensitiveIdentity}) || page.Watermark != 2 || page.NextAfter != 2 || page.HasMore {
		t.Fatalf("later candidate crossed frozen watermark: %+v, %v", page, err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
