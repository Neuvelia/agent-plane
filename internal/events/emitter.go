package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sink accepts one in-process event and returns the sequenced fact. F0.6 uses
// Collector; it does not invoke arbitrary consumer callbacks during execution.
type Sink interface{ Append(Event) (Event, error) }

var ErrCollectorFinished = errors.New("event collector is finished")

// Collector is a run-local, concurrency-safe in-memory event sink.
type Collector struct {
	mu           sync.Mutex
	runID        RunID
	graphID      string
	graphVersion string
	events       []Event
	finished     bool
	live         chan<- Event
	liveContext  context.Context
	liveDropped  bool
}

func NewCollector(runID RunID, graphID, graphVersion string) *Collector {
	return &Collector{runID: runID, graphID: graphID, graphVersion: graphVersion}
}

// NewCollectorWithLiveEvents also sends each appended fact to a caller-owned
// channel while holding the sequence lock. The caller must consume while the
// run is active and must not close the channel until the run returns. A slow
// consumer applies backpressure until ctx is cancelled; after the first
// undelivered event, live delivery stops and the retained slice contains the
// complete tail for replay.
// Nil live disables delivery.
func NewCollectorWithLiveEvents(ctx context.Context, runID RunID, graphID, graphVersion string, live chan<- Event) *Collector {
	c := NewCollector(runID, graphID, graphVersion)
	c.live = live
	c.liveContext = ctx
	return c
}

// Append owns timestamp and sequence assignment. The caller owns lifecycle
// meaning; Collector owns ordering and immutable storage. Invalid and late
// appends are rejected without changing the stored stream.
func (c *Collector) Append(event Event) (Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return Event{}, ErrCollectorFinished
	}
	if err := event.Validate(); err != nil {
		return Event{}, fmt.Errorf("append event: %w", err)
	}
	event.SchemaVersion = SchemaVersion
	event.Timestamp = time.Now().UTC()
	event.Sequence = uint64(len(c.events) + 1)
	event.RunID = c.runID
	event.GraphID = c.graphID
	event.GraphVersion = c.graphVersion
	event.Payload = copyPayload(event.Payload)
	c.events = append(c.events, event)
	if event.Type == RunFinished {
		c.finished = true
	}
	event.Payload = copyPayload(event.Payload)
	if c.live != nil && !c.liveDropped {
		select {
		case c.live <- event:
		default:
			select {
			case c.live <- event:
			case <-c.liveContext.Done():
				// The retained event slice remains complete when a cancelled
				// run cannot deliver to a blocked live consumer. Stop live
				// delivery so the consumer sees a prefix it can replay.
				c.liveDropped = true
			}
		}
	}
	return event, nil
}

// Snapshot returns events in authoritative sequence order, detached from the
// collector's mutable storage.
func (c *Collector) Snapshot() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Event, len(c.events))
	for i, event := range c.events {
		event.Payload = copyPayload(event.Payload)
		result[i] = event
	}
	return result
}

func copyPayload(payload Payload) Payload {
	switch p := payload.(type) {
	case NodeEvidencePayload:
		p.ChangedFiles = append([]ChangedFile{}, p.ChangedFiles...)
		return p
	case LogChunkPayload:
		p.Data = append([]byte(nil), p.Data...)
		return p
	case NodeFinishedPayload:
		p.DiagnosticCodes = append([]string(nil), p.DiagnosticCodes...)
		if p.DurationMS != nil {
			duration := *p.DurationMS
			p.DurationMS = &duration
		}
		return p
	case NodeTerminatedPayload:
		p.DiagnosticCodes = append([]string(nil), p.DiagnosticCodes...)
		return p
	default:
		return payload
	}
}
