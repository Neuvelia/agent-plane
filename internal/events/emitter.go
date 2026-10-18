package events

import (
	"sync"
	"time"
)

// Sink accepts one in-process event and returns the sequenced fact. F0.6 uses
// Collector; it does not invoke arbitrary consumer callbacks during execution.
type Sink interface{ Append(Event) Event }

// Collector is a run-local, concurrency-safe in-memory event sink.
type Collector struct {
	mu           sync.Mutex
	runID        RunID
	graphID      string
	graphVersion string
	events       []Event
	finished     bool
}

func NewCollector(runID RunID, graphID, graphVersion string) *Collector {
	return &Collector{runID: runID, graphID: graphID, graphVersion: graphVersion}
}

// Append owns timestamp and sequence assignment. The caller owns lifecycle
// meaning; Collector owns ordering and immutable storage. Append returns a
// zero Event if the run already finished, so late observers cannot extend a
// terminal trace.
func (c *Collector) Append(event Event) Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return Event{}
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
	return event
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
