package trace

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/domain/events"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLiveEventsRemainPrefixAfterCancellationDropsAnEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	live := make(chan events.Event, 1)
	collector := NewCollectorWithLiveEvents(ctx, system.Clock{}, "run", "graph", "version", live)
	if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if got := (<-live).Type; got != events.RunStarted {
		t.Fatalf("first live event = %s", got)
	}
	cancel()
	if _, err := collector.Append(events.Event{Type: events.NodeReady, Payload: events.NodeReadyPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(events.Event{Type: events.NodeStarted, Payload: events.NodeStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "cancelled"}}); err != nil {
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
	if es[len(es)-1].Type != events.RunFinished {
		t.Fatal("terminal fact missing")
	}

}

func TestCollectorSequencesConcurrentEventsAndCopiesLogs(t *testing.T) {
	collector := NewCollector(system.Clock{}, "run", "graph", "sha256-v0:hash")
	const workers, perWorker = 8, 20
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < perWorker; j++ {
				data := []byte("secret")
				collector.Append(events.Event{Type: events.LogChunk, NodeID: "worker", NodeRunID: "node-run", Payload: events.LogChunkPayload{Stream: events.Stdout, Data: data}})
				data[0] = 'X'
			}
		}()
	}
	group.Wait()
	snapshot := collector.Snapshot()
	if len(snapshot) != workers*perWorker {
		t.Fatalf("snapshot = %d", len(snapshot))
	}
	for i, event := range snapshot {
		if event.Sequence != uint64(i+1) || event.RunID != "run" || event.GraphVersion != "sha256-v0:hash" || event.Timestamp.IsZero() {
			t.Fatalf("event %d = %#v", i, event)
		}
		if string(event.Payload.(events.LogChunkPayload).Data) != "secret" {
			t.Fatalf("event data mutated: %#v", event.Payload)
		}
	}
	snapshot[0].Payload = events.NodeReadyPayload{}
	if _, ok := collector.Snapshot()[0].Payload.(events.LogChunkPayload); !ok {
		t.Fatal("snapshot changed collector")
	}
}

func TestCollectorSnapshotCannotMutateDuration(t *testing.T) {
	collector := NewCollector(system.Clock{}, "run", "graph", "version")
	duration := int64(12)
	collector.Append(events.Event{Type: events.NodeFinished, Payload: events.NodeFinishedPayload{Status: "succeeded", DurationMS: &duration}})
	duration = 99
	first := collector.Snapshot()
	*first[0].Payload.(events.NodeFinishedPayload).DurationMS = 88
	got := collector.Snapshot()[0].Payload.(events.NodeFinishedPayload)
	if *got.DurationMS != 12 {
		t.Fatalf("stored duration = %d", *got.DurationMS)
	}
}

func TestCollectorAppendReturnCannotMutateStoredPayload(t *testing.T) {
	collector := NewCollector(system.Clock{}, "run", "graph", "version")
	recorded, err := collector.Append(events.Event{Type: events.LogChunk, Payload: events.LogChunkPayload{Stream: events.Stdout, Data: []byte("safe")}})
	if err != nil {
		t.Fatal(err)
	}
	recorded.Payload.(events.LogChunkPayload).Data[0] = 'X'
	if got := string(collector.Snapshot()[0].Payload.(events.LogChunkPayload).Data); got != "safe" {
		t.Fatalf("stored payload = %q", got)
	}
}

func TestCollectorRejectsEventsAfterRunFinished(t *testing.T) {
	collector := NewCollector(system.Clock{}, "run", "graph", "version")
	collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}})
	collector.Append(events.Event{Type: events.NodeReady, Payload: events.NodeReadyPayload{}})
	collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "failed"}})
	snapshot := collector.Snapshot()
	if len(snapshot) != 2 || snapshot[1].Type != events.RunFinished {
		t.Fatalf("snapshot after terminal = %#v", snapshot)
	}
}

func TestCollectorRejectsInvalidAndLateEvents(t *testing.T) {
	collector := NewCollector(system.Clock{}, "run", "graph", "version")
	if _, err := collector.Append(events.Event{Type: events.LogChunk, Payload: events.NodeFinishedPayload{Status: "failed"}}); err == nil {
		t.Fatal("mismatched event accepted")
	}
	if got := len(collector.Snapshot()); got != 0 {
		t.Fatalf("invalid event changed collector: %d events", got)
	}
	if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Append(events.Event{Type: events.NodeReady, Payload: events.NodeReadyPayload{}}); err == nil {
		t.Fatal("late event accepted without error")
	}
	if got := len(collector.Snapshot()); got != 2 {
		t.Fatalf("late event changed collector: %d events", got)
	}
}

func TestCollectorPersistsRetainedEventsInSequenceOrder(t *testing.T) {
	c := NewCollector(system.Clock{}, "run", "g", "sha256-v0:x")
	var got []events.Event
	c.SetPersist(func(e events.Event) error { got = append(got, e); return nil })
	c.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{NodeCount: 1}})
	c.Append(events.Event{Type: events.LogChunk, NodeID: "n", NodeRunID: "nr", Payload: events.LogChunkPayload{Stream: events.Stdout, Data: []byte("x")}})
	c.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}})
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
	c := NewCollector(system.Clock{}, "run", "g", "sha256-v0:x")
	calls := 0
	c.SetPersist(func(events.Event) error { calls++; return errors.New("disk full") })
	c.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	c.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}})
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
	c := NewCollector(system.Clock{}, "run", "g", "sha256-v0:x")
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var order []uint64
	first := true
	c.SetPersist(func(e events.Event) error {
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
	secondCalling := make(chan struct{})
	go func() {
		c.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
		done <- struct{}{}
	}()
	<-entered
	go func() {
		close(secondCalling) // the second Append is about to be issued
		c.Append(events.Event{Type: events.NodeReady, NodeID: "n", NodeRunID: "nr", Payload: events.NodeReadyPayload{}})
		done <- struct{}{}
	}()
	<-secondCalling
	// Bounded absence check: neither Append may finish while the sink blocks.
	select {
	case <-done:
		t.Fatal("an Append completed while the sink was blocked")
	case <-time.After(100 * time.Millisecond):
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

func TestCollectorCopiesRecordedValue(t *testing.T) {
	c := NewCollector(system.Clock{}, "run", "g", "v")
	text := "hello"
	p := events.ValueRecordedPayload{NodeRunID: "nr", AttemptID: "a", Port: "stdout", Type: "string", SizeBytes: int64(len(text)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), Value: &text}
	recorded, err := c.Append(events.Event{Type: events.ValueRecorded, NodeID: "work", NodeRunID: "nr", AttemptID: "a", Payload: p})
	if err != nil {
		t.Fatal(err)
	}
	*p.Value = "HELLO"
	*recorded.Payload.(events.ValueRecordedPayload).Value = "mutated"
	if got := *c.Snapshot()[0].Payload.(events.ValueRecordedPayload).Value; got != "hello" {
		t.Fatalf("retained value = %q", got)
	}
}
