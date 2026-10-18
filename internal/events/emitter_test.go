package events

import (
	"sync"
	"testing"
)

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
	recorded := collector.Append(Event{Type: LogChunk, Payload: LogChunkPayload{Stream: Stdout, Data: []byte("safe")}})
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
