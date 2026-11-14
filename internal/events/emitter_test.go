package events

import (
	"context"
	"errors"
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

func TestCollectorPersistsRetainedEventsInSequenceOrder(t *testing.T) {
	c := NewCollector("run", "g", "sha256-v0:x")
	var got []Event
	c.SetPersist(func(e Event) error { got = append(got, e); return nil })
	c.Append(Event{Type: RunStarted, Payload: RunStartedPayload{NodeCount: 1}})
	c.Append(Event{Type: LogChunk, NodeID: "n", NodeRunID: "nr", Payload: LogChunkPayload{Stream: Stdout, Data: []byte("x")}})
	c.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}})
	snapshot := c.Snapshot()
	if len(got) != len(snapshot) {
		t.Fatalf("persisted %d of %d retained events", len(got), len(snapshot))
	}
	for i := range got {
		if got[i].Sequence != snapshot[i].Sequence || got[i].Type != snapshot[i].Type {
			t.Fatalf("event %d persisted out of order", i)
		}
	}
}

func TestCollectorStopsPersistingAfterFirstErrorAndReportsIt(t *testing.T) {
	c := NewCollector("run", "g", "sha256-v0:x")
	calls := 0
	c.SetPersist(func(Event) error { calls++; return errors.New("disk full") })
	c.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}})
	c.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}})
	if calls != 1 || c.PersistError() == nil {
		t.Fatalf("calls=%d err=%v", calls, c.PersistError())
	}
	if len(c.Snapshot()) != 2 {
		t.Fatal("a persistence failure must not drop in-memory events")
	}
}

// Persistence runs under the collector lock (ADR-0019): a blocked sink delays
// every later Append, and releasing it preserves sequence order. This pins the
// documented behaviour so a change to it is deliberate.
func TestBlockedPersistSinkDelaysLaterAppendsAndKeepsOrder(t *testing.T) {
	c := NewCollector("run", "g", "sha256-v0:x")
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var order []uint64
	first := true
	c.SetPersist(func(e Event) error {
		if first {
			first = false
			close(entered)
			<-release
		}
		mu.Lock()
		order = append(order, e.Sequence)
		mu.Unlock()
		return nil
	})
	done := make(chan struct{}, 2)
	go func() { c.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}}); done <- struct{}{} }()
	<-entered
	go func() {
		c.Append(Event{Type: NodeReady, NodeID: "n", NodeRunID: "nr", Payload: NodeReadyPayload{}})
		done <- struct{}{}
	}()
	select {
	case <-done:
		t.Fatal("an Append completed while the sink was blocked")
	default:
	}
	close(release)
	<-done
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("order after release: %v", order)
	}
}
