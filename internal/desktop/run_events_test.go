package desktop

import (
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

func TestRunEventsPagesAndResumesAfterAppend(t *testing.T) {
	collector := events.NewCollector(replayRunID, "graph", "version")
	appendReplayEvent(t, collector, events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	appendReplayEvent(t, collector, events.Event{Type: events.NodeReady, Payload: events.NodeReadyPayload{}})
	service := replayFixture(t, collector)
	first, err := service.Events(replayRunID, 0, 1)
	if err != nil || first.RunID != replayRunID || first.AfterSequence != 0 ||
		len(first.Events) != 1 || len(first.EventHashes) != 1 || first.Events[0].Sequence != 1 ||
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
		second.Events[0].Sequence != 2 || second.NextAfter != 2 || !second.HasMore || second.Watermark != 3 {
		t.Fatalf("second page = %#v, %v", second, err)
	}
	third, err := service.Events(replayRunID, second.NextAfter, 1)
	if err != nil || len(third.Events) != 1 || third.Events[0].Sequence != 3 ||
		third.NextAfter != 3 || third.HasMore {
		t.Fatalf("third page = %#v, %v", third, err)
	}
	first.Events[0].Payload = events.NodeReadyPayload{}
	again, err := service.Events(replayRunID, 0, 1)
	if err != nil || again.Events[0].Type != events.RunStarted {
		t.Fatalf("mutated retained page = %#v, %v", again, err)
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
	if err != nil || len(first.Events) != 1 || first.Events[0].Sequence != events.MaxRetainedProgressItems ||
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
		final.Events[0].Type != events.ProgressOmitted || final.Events[1].Type != events.RunFinished ||
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
	if err != nil || page.Finished || len(page.Events) != 1 || page.Events[0].Type != events.RunStarted {
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
	service := replayFixture(t, sizedReplayTrace{values: []events.Event{
		sizedReplayEvent(1, 4<<20), sizedReplayEvent(2, 4<<20),
	}})
	first, err := service.Events(replayRunID, 0, 2)
	if err != nil || len(first.Events) != 1 || first.NextAfter != 1 || !first.HasMore || first.Watermark != 2 {
		t.Fatalf("shrunk first page = %d facts, cursor %d, more %t, watermark %d, %v",
			len(first.Events), first.NextAfter, first.HasMore, first.Watermark, err)
	}
	encoded, marshalErr := json.Marshal(Response{ProtocolVersion: ProtocolVersion,
		RequestID: strings.Repeat("r", 128), Result: mustJSON(t, first)})
	if marshalErr != nil || len(encoded) > MaxFrameBytes {
		t.Fatalf("first page frame = %d bytes, %v", len(encoded), marshalErr)
	}
	second, err := service.Events(replayRunID, first.NextAfter, 2)
	if err != nil || len(second.Events) != 1 || second.Events[0].Sequence != 2 || second.HasMore || second.NextAfter != 2 {
		t.Fatalf("second page = %d facts, cursor %d, more %t, %v",
			len(second.Events), second.NextAfter, second.HasMore, err)
	}
}

func TestRunEventsRejectsSingleOversizeFactWithoutZeroProgressPage(t *testing.T) {
	service := replayFixture(t, sizedReplayTrace{values: []events.Event{sizedReplayEvent(1, 8<<20)}})
	if page, err := service.Events(replayRunID, 0, 1); err == nil || err.Code != "response_too_large" ||
		len(page.Events) != 0 {
		t.Fatalf("oversize fact = %#v, %v", page, err)
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
