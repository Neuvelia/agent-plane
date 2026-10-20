package events

import (
	"context"
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
