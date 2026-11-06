package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestLiveEventsRemainPrefixAfterCancellationDropsAnEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	live := make(chan Event, 1)
	collector := NewCollectorWithLiveEvents(ctx, "run", "graph", "version", live)
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if got := (<-live).Type; got != RunStarted {
		t.Fatalf("first live event = %s", got)
	}
	cancel()
	if _, err := collector.Append(Event{Type: NodeReady, Payload: NodeReadyPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(Event{Type: NodeStarted, Payload: NodeStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "cancelled"}}); err != nil {
		t.Fatal(err)
	}
	close(live)
	last := uint64(1)
	for e := range live {
		if e.Sequence != last+1 {
			t.Fatal("live delivery gap")
		}
		last = e.Sequence
	}
	es := collector.Snapshot()
	if es[len(es)-1].Type != RunFinished {
		t.Fatal("terminal fact missing")
	}

}

func TestIdentityRedactionProvenanceStaysInMemory(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	recorded, err := collector.Append(Event{Type: NodeReady, NodeID: "node", NodeRunID: "node-run", Payload: NodeReadyPayload{}, IdentityRedacted: true})
	if err != nil || !recorded.IdentityRedacted {
		t.Fatalf("append provenance = %+v, %v", recorded, err)
	}
	for name, events := range map[string][]Event{
		"snapshot": collector.Snapshot(),
	} {
		if len(events) != 1 || !events[0].IdentityRedacted {
			t.Fatalf("%s provenance = %+v", name, events)
		}
	}
	page, err := collector.Page(0, 1)
	if err != nil || len(page.Events) != 1 || !page.Events[0].IdentityRedacted {
		t.Fatalf("page provenance = %+v, %v", page, err)
	}
	bounded, err := collector.PageBounded(0, 1, 1024)
	if err != nil || len(bounded.Events) != 1 || !bounded.Events[0].IdentityRedacted {
		t.Fatalf("bounded provenance = %+v, %v", bounded, err)
	}
	encoded, err := json.Marshal(recorded)
	if err != nil || strings.Contains(string(encoded), "identity_redacted") || strings.Contains(string(encoded), "IdentityRedacted") {
		t.Fatalf("wire leaked provenance: %s, %v", encoded, err)
	}
	var decoded Event
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.IdentityRedacted {
		t.Fatalf("wire unexpectedly carries provenance: %+v, %v", decoded, err)
	}
	digest := sha256.Sum256(encoded)
	if len(page.EventHashes) != 1 || page.EventHashes[0] != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("wire hash changed by memory-only provenance: %+v", page.EventHashes)
	}
}

func TestCollectorPageBoundedSelectsLargeFactsBeforeCopying(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	largePath := strings.Repeat("x", 4<<20)
	for i := 0; i < 3; i++ {
		if _, err := collector.Append(Event{Type: NodeEvidence,
			Payload: NodeEvidencePayload{ChangedFiles: []ChangedFile{{Path: largePath}}}}); err != nil {
			t.Fatal(err)
		}
	}
	const budget = (7 << 20) - 4096
	first, err := collector.PageBounded(0, 3, budget)
	if err != nil || len(first.Events) != 1 || first.NextAfter != 1 || !first.HasMore || first.Watermark != 3 {
		t.Fatalf("first bounded page = %d facts, cursor %d, more %t, watermark %d, %v",
			len(first.Events), first.NextAfter, first.HasMore, first.Watermark, err)
	}
	encoded, err := json.Marshal(first.Events[0])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if len(first.EventHashes) != 1 || first.EventHashes[0] != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("retained bytes hash = %#v", first.EventHashes)
	}
	second, err := collector.PageBounded(first.NextAfter, 3, budget)
	if err != nil || len(second.Events) != 1 || second.Events[0].Sequence != 2 || second.NextAfter != 2 || !second.HasMore {
		t.Fatalf("second bounded page = %+v, %v", second, err)
	}
	secondJSON, err := json.Marshal(second.Events[0])
	if err != nil {
		t.Fatal(err)
	}
	secondDigest := sha256.Sum256(secondJSON)
	if len(second.EventHashes) != 1 || second.EventHashes[0] != "sha256:"+hex.EncodeToString(secondDigest[:]) ||
		second.EventHashes[0] == first.EventHashes[0] {
		t.Fatalf("aligned hashes = %#v", second.EventHashes)
	}
	if _, err := collector.PageBounded(0, 1, 1); !errors.Is(err, ErrTraceEventTooLarge) {
		t.Fatalf("single oversized event = %v", err)
	}
}

func TestCollectorReservesOriginalSequenceForSensitiveOmission(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	sequence, err := collector.OmitSensitive()
	if err != nil || sequence != 2 {
		t.Fatalf("omitted sequence = %d, %v", sequence, err)
	}
	if _, err := collector.Append(Event{Type: NodeReady, NodeID: "node", NodeRunID: "node-run", Payload: NodeReadyPayload{}}); err != nil {
		t.Fatal(err)
	}
	first, err := collector.Page(0, 1)
	if err != nil || len(first.Events) != 1 || first.Events[0].Sequence != 1 || first.NextAfter != 1 || len(first.Omissions) != 0 {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := collector.Page(first.NextAfter, 1)
	if err != nil || len(second.Events) != 1 || second.Events[0].Sequence != 3 || second.NextAfter != 3 || second.HasMore ||
		len(second.Omissions) != 1 || second.Omissions[0] != (OmissionRange{From: 2, To: 2, Reason: OmissionSensitiveIdentity}) {
		t.Fatalf("second page = %+v, %v", second, err)
	}
}

func TestCollectorPagesOnlySensitiveOmissionsAndCoalescesRanges(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := collector.OmitSensitive(); err != nil {
			t.Fatal(err)
		}
	}
	page, err := collector.Page(1, 1)
	if err != nil || len(page.Events) != 0 || page.NextAfter != 4 || page.HasMore || page.Watermark != 4 ||
		len(page.Omissions) != 1 || page.Omissions[0] != (OmissionRange{From: 2, To: 4, Reason: OmissionSensitiveIdentity}) {
		t.Fatalf("all-omission page = %+v, %v", page, err)
	}
	if _, err := collector.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.OmitSensitive(); !errors.Is(err, ErrCollectorFinished) {
		t.Fatalf("late omission = %v", err)
	}
}

func TestCollectorPageBoundedAccountsForOmissionsBeforeLargeFact(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.OmitSensitive(); err != nil {
		t.Fatal(err)
	}
	large, err := collector.Append(Event{Type: NodeEvidence,
		Payload: NodeEvidencePayload{ChangedFiles: []ChangedFile{{Path: strings.Repeat("x", 4<<20)}}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(large)
	if err != nil {
		t.Fatal(err)
	}
	budget := len(encoded) + 4 // event array and empty omission array fit exactly
	first, err := collector.PageBounded(1, 1, budget)
	if err != nil || len(first.Events) != 0 || first.NextAfter != 2 || !first.HasMore ||
		len(first.Omissions) != 1 || first.Omissions[0] != (OmissionRange{From: 2, To: 2, Reason: OmissionSensitiveIdentity}) {
		t.Fatalf("bounded omission page = %d events, cursor %d, more %t, omissions %+v, %v",
			len(first.Events), first.NextAfter, first.HasMore, first.Omissions, err)
	}
	second, err := collector.PageBounded(first.NextAfter, 1, budget)
	if err != nil || len(second.Events) != 1 || second.Events[0].Sequence != 3 || len(second.Omissions) != 0 || second.HasMore {
		t.Fatalf("bounded fact page = %d events, %+v omissions, %v", len(second.Events), second.Omissions, err)
	}
	pageEvents, err := json.Marshal(second.Events)
	if err != nil {
		t.Fatal(err)
	}
	pageOmissions, err := json.Marshal(second.Omissions)
	if err != nil || len(pageEvents)+len(pageOmissions) > budget {
		t.Fatalf("bounded encoded page = %d bytes, %v", len(pageEvents)+len(pageOmissions), err)
	}
}

func TestCollectorPageBoundedSeparatesTrailingOmissions(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	fact, err := collector.Append(Event{Type: NodeReady, NodeID: "node", NodeRunID: "node-run", Payload: NodeReadyPayload{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.OmitSensitive(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	budget := len(encoded) + 4
	first, err := collector.PageBounded(1, 1, budget)
	if err != nil || len(first.Events) != 1 || first.Events[0].Sequence != 2 ||
		first.NextAfter != 2 || !first.HasMore || len(first.Omissions) != 0 {
		t.Fatalf("trailing split = %d events, cursor %d, more %t, omissions %+v, %v",
			len(first.Events), first.NextAfter, first.HasMore, first.Omissions, err)
	}
	second, err := collector.PageBounded(first.NextAfter, 1, budget)
	if err != nil || len(second.Events) != 0 || second.NextAfter != 3 || second.HasMore ||
		len(second.Omissions) != 1 || second.Omissions[0].From != 3 || second.Omissions[0].To != 3 {
		t.Fatalf("trailing omission = %d events, cursor %d, more %t, omissions %+v, %v",
			len(second.Events), second.NextAfter, second.HasMore, second.Omissions, err)
	}
}

func TestCollectorNextBoundedReportsOversizeWithoutCopyingFact(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.OmitSensitive(); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(Event{Type: NodeEvidence, Payload: NodeEvidencePayload{AdapterVersion: strings.Repeat("x", 4<<20)}}); err != nil {
		t.Fatal(err)
	}
	first, err := collector.NextBounded(0, 3<<20)
	if err != nil || first.Event == nil || first.Event.Sequence != 1 || first.OversizeSequence != 0 || first.NextAfter != 1 || first.Watermark != 3 {
		t.Fatalf("first bounded candidate = %+v, %v", first, err)
	}
	second, err := collector.NextBounded(1, 3<<20)
	if err != nil || second.Event != nil || second.OversizeSequence != 3 || second.NextAfter != 3 || len(second.Omissions) != 1 || second.Omissions[0] != (OmissionRange{From: 2, To: 2, Reason: OmissionSensitiveIdentity}) {
		t.Fatalf("oversize bounded candidate = %+v, %v", second, err)
	}
	if _, err := collector.OmitSensitive(); err != nil {
		t.Fatal(err)
	}
	third, err := collector.NextBounded(second.NextAfter, 3<<20)
	if err != nil || third.Event != nil || third.OversizeSequence != 0 || third.NextAfter != 4 || len(third.Omissions) != 1 || third.Omissions[0].From != 4 || third.Omissions[0].To != 4 {
		t.Fatalf("all-omission bounded candidate = %+v, %v", third, err)
	}
	if _, err := collector.NextBounded(5, 3<<20); err == nil {
		t.Fatal("cursor past watermark accepted")
	}
}

func TestCollectorNextBoundedDetachesReturnedPayload(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: LogChunk, Payload: LogChunkPayload{Stream: Stdout, Data: []byte("original")}}); err != nil {
		t.Fatal(err)
	}
	candidate, err := collector.NextBounded(0, 1024)
	if err != nil || candidate.Event == nil {
		t.Fatalf("candidate = %+v, %v", candidate, err)
	}
	data := candidate.Event.Payload.(LogChunkPayload).Data
	data[0] = 'X'
	again, err := collector.NextBounded(0, 1024)
	if err != nil || string(again.Event.Payload.(LogChunkPayload).Data) != "original" {
		t.Fatalf("retained payload mutated through candidate: %+v, %v", again, err)
	}
}

func TestCollectorSequencesConcurrentEventsAndCopiesLogs(t *testing.T) {
	collector := NewCollector("run", "graph", "sha256-v0:hash")
	const workers, perWorker = 8, 20
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < perWorker; j++ {
				data := []byte("secret")
				collector.Append(Event{Type: LogChunk, NodeID: "worker", NodeRunID: "node-run", Payload: LogChunkPayload{Stream: Stdout, Data: data}})
				data[0] = 'X'
			}
		}()
	}
	group.Wait()
	events := collector.Snapshot()
	if len(events) != workers*perWorker {
		t.Fatalf("events = %d", len(events))
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) || event.RunID != "run" || event.GraphVersion != "sha256-v0:hash" || event.Timestamp.IsZero() {
			t.Fatalf("event %d = %#v", i, event)
		}
		if string(event.Payload.(LogChunkPayload).Data) != "secret" {
			t.Fatalf("event data mutated: %#v", event.Payload)
		}
	}
	events[0].Payload = NodeReadyPayload{}
	if _, ok := collector.Snapshot()[0].Payload.(LogChunkPayload); !ok {
		t.Fatal("snapshot changed collector")
	}
}

func TestCollectorSnapshotCannotMutateDuration(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	duration := int64(12)
	collector.Append(Event{Type: NodeFinished, Payload: NodeFinishedPayload{Status: "succeeded", DurationMS: &duration}})
	duration = 99
	first := collector.Snapshot()
	*first[0].Payload.(NodeFinishedPayload).DurationMS = 88
	got := collector.Snapshot()[0].Payload.(NodeFinishedPayload)
	if *got.DurationMS != 12 {
		t.Fatalf("stored duration = %d", *got.DurationMS)
	}
}

func TestCollectorAppendReturnCannotMutateStoredPayload(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	recorded, err := collector.Append(Event{Type: LogChunk, Payload: LogChunkPayload{Stream: Stdout, Data: []byte("safe")}})
	if err != nil {
		t.Fatal(err)
	}
	recorded.Payload.(LogChunkPayload).Data[0] = 'X'
	if got := string(collector.Snapshot()[0].Payload.(LogChunkPayload).Data); got != "safe" {
		t.Fatalf("stored payload = %q", got)
	}
}

func TestCollectorRejectsEventsAfterRunFinished(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}})
	collector.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}})
	collector.Append(Event{Type: NodeReady, Payload: NodeReadyPayload{}})
	collector.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "failed"}})
	events := collector.Snapshot()
	if len(events) != 2 || events[1].Type != RunFinished {
		t.Fatalf("events after terminal = %#v", events)
	}
}

func TestCollectorRejectsInvalidAndLateEvents(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: LogChunk, Payload: NodeFinishedPayload{Status: "failed"}}); err == nil {
		t.Fatal("mismatched event accepted")
	}
	if got := len(collector.Snapshot()); got != 0 {
		t.Fatalf("invalid event changed collector: %d events", got)
	}
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(Event{Type: NodeReady, Payload: NodeReadyPayload{}}); err == nil {
		t.Fatal("late event accepted without error")
	}
	if got := len(collector.Snapshot()); got != 2 {
		t.Fatalf("late event changed collector: %d events", got)
	}
}

func TestCollectorPageContinuesWithoutSkippingRetainedFacts(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	for _, event := range []Event{
		{Type: RunStarted, Payload: RunStartedPayload{}},
		{Type: NodeReady, NodeID: "node", NodeRunID: "node-run", Payload: NodeReadyPayload{}},
		{Type: NodeStarted, NodeID: "node", NodeRunID: "node-run", Payload: NodeStartedPayload{}},
	} {
		if _, err := collector.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	first, err := collector.Page(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || first.Events[0].Sequence != 1 || first.Events[1].Sequence != 2 || first.NextAfter != 2 || !first.HasMore || first.Watermark != 3 {
		t.Fatalf("first page = %+v", first)
	}
	second, err := collector.Page(first.NextAfter, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].Sequence != 3 || second.NextAfter != 3 || second.HasMore || second.Watermark != 3 {
		t.Fatalf("second page = %+v", second)
	}
}

func TestCollectorPageAdvancesAcrossOmittedProgressAndDetachesPayload(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	for i := 0; i < MaxRetainedProgressItems+1; i++ {
		if _, err := collector.Append(Event{Type: LogChunk, Payload: LogChunkPayload{Stream: Stdout, Data: []byte("safe")}}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := collector.Page(MaxRetainedProgressItems-1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Sequence != MaxRetainedProgressItems || page.NextAfter != MaxRetainedProgressItems+1 || page.Watermark != MaxRetainedProgressItems+1 || page.HasMore || page.Stats.RetainedOmittedEvents != 1 {
		t.Fatalf("omission page = %+v", page)
	}
	page.Events[0].Payload.(LogChunkPayload).Data[0] = 'X'
	again, err := collector.Page(MaxRetainedProgressItems-1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(again.Events[0].Payload.(LogChunkPayload).Data); got != "safe" {
		t.Fatalf("stored payload = %q", got)
	}
	if _, err := collector.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	final, err := collector.Page(page.NextAfter, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Finished || len(final.Events) != 2 || final.Events[0].Type != ProgressOmitted || final.Events[1].Type != RunFinished || final.HasMore {
		t.Fatalf("final page = %+v", final)
	}
}

func TestCollectorPageRejectsInvalidCursorAndLimit(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	for _, tc := range []struct {
		after uint64
		limit int
	}{{0, 0}, {0, MaxTracePageItems + 1}, {1, 1}} {
		if _, err := collector.Page(tc.after, tc.limit); err == nil {
			t.Fatalf("Page(%d, %d) accepted", tc.after, tc.limit)
		}
	}
}

func TestCollectorPageResumesAfterAppendBetweenSnapshots(t *testing.T) {
	collector := NewCollector("run", "graph", "version")
	if _, err := collector.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	first, err := collector.Page(0, 1)
	if err != nil || first.HasMore || first.NextAfter != 1 || first.Watermark != 1 || first.Finished {
		t.Fatalf("initial page = %+v, %v", first, err)
	}
	resume := make(chan struct{})
	appended := make(chan struct{})
	go func() {
		<-resume
		collector.Append(Event{Type: NodeReady, NodeID: "node", NodeRunID: "node-run", Payload: NodeReadyPayload{}})
		collector.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}})
		close(appended)
	}()
	close(resume)
	<-appended
	second, err := collector.Page(first.NextAfter, 2)
	if err != nil || len(second.Events) != 2 || second.Events[0].Sequence != 2 || second.Events[1].Sequence != 3 || second.Events[1].Type != RunFinished || second.NextAfter != 3 || second.HasMore || second.Watermark != 3 || !second.Finished {
		t.Fatalf("resumed page = %+v, %v", second, err)
	}
}
