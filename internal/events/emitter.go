package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Sink interface{ Append(Event) (Event, error) }

var ErrCollectorFinished = errors.New("event collector is finished")

const (
	MaxRetainedProgressBytes = 4 << 20
	MaxRetainedProgressItems = 4096
	MaxLiveBytes             = 1 << 20
	MaxLiveItems             = 1024
)

// DeliveryStats counts encoded whole-event JSON bytes, not Go heap usage.
type DeliveryStats struct {
	RetainedPeakBytes, RetainedPeakItems        int
	LivePeakBytes, LivePeakItems                int
	RetainedOmittedEvents, RetainedOmittedBytes uint64
	LiveOmittedEvents, LiveOmittedBytes         uint64
	LiveStopped                                 bool
}
type queuedEvent struct {
	event Event
	bytes int
}

// Collector retains lifecycle/evidence independently from bounded progress.
// Live delivery is a bounded FIFO prefix, including the in-flight head.
type Collector struct {
	mu                           sync.Mutex
	runID                        RunID
	graphID, graphVersion        string
	events                       []Event
	sequence                     uint64
	finished, finalizing         bool
	retainedBytes, retainedItems int
	stats                        DeliveryStats
	live                         chan<- Event
	queue                        []queuedEvent
	queueBytes                   int
	wake, stop, done             chan struct{}
}

func NewCollector(runID RunID, graphID, graphVersion string) *Collector {
	return &Collector{runID: runID, graphID: graphID, graphVersion: graphVersion}
}

// The caller owns live and may close it only after finalization returns.
// Append never waits for a reader. Final run/omission facts are replay-only.
func NewCollectorWithLiveEvents(ctx context.Context, runID RunID, graphID, graphVersion string, live chan<- Event) *Collector {
	c := NewCollector(runID, graphID, graphVersion)
	c.live = live
	if live != nil {
		c.wake = make(chan struct{}, 1)
		c.stop = make(chan struct{})
		c.done = make(chan struct{})
		go c.pump(ctx)
	}
	return c
}
func (c *Collector) pump(ctx context.Context) {
	defer close(c.done)
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.mu.Unlock()
			select {
			case <-c.wake:
				continue
			case <-c.stop:
				return
			case <-ctx.Done():
				return
			}
		}
		q := c.queue[0]
		c.mu.Unlock()
		select {
		case c.live <- q.event:
			c.mu.Lock()
			c.queue[0] = queuedEvent{}
			c.queue = c.queue[1:]
			c.queueBytes -= q.bytes
			c.mu.Unlock()
		case <-c.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}
func (c *Collector) Append(event Event) (Event, error) {
	c.mu.Lock()
	if c.finished || c.finalizing {
		c.mu.Unlock()
		return Event{}, ErrCollectorFinished
	}
	event.SchemaVersion = SchemaVersion
	event.RunID = c.runID
	event.GraphID = c.graphID
	event.GraphVersion = c.graphVersion
	if err := event.Validate(); err != nil {
		c.mu.Unlock()
		return Event{}, fmt.Errorf("append event: %w", err)
	}
	if event.Type == RunFinished {
		c.finalizing = true
		if c.stop != nil {
			close(c.stop)
		}
		c.mu.Unlock()
		if c.done != nil {
			<-c.done
		}
		c.mu.Lock()
		for _, q := range c.queue {
			c.stats.LiveOmittedEvents++
			c.stats.LiveOmittedBytes += uint64(q.bytes)
		}
		if len(c.queue) > 0 {
			c.stats.LiveStopped = true
		}
		c.queue = nil
		c.queueBytes = 0
		if c.stats.RetainedOmittedEvents > 0 || c.stats.LiveOmittedEvents > 0 {
			marker := c.prepare(Event{Type: ProgressOmitted, Payload: ProgressOmittedPayload{RetainedEvents: c.stats.RetainedOmittedEvents, RetainedBytes: c.stats.RetainedOmittedBytes, LiveEvents: c.stats.LiveOmittedEvents, LiveBytes: c.stats.LiveOmittedBytes, LiveStopped: c.stats.LiveStopped}})
			c.events = append(c.events, marker)
		}
		event = c.prepare(event)
		c.events = append(c.events, event)
		c.finished = true
		c.mu.Unlock()
		return event, nil
	}
	event = c.prepare(event)
	encoded, _ := json.Marshal(event)
	size := len(encoded)
	if IsProgress(event.Type) {
		if c.retainedItems >= MaxRetainedProgressItems || c.retainedBytes+size > MaxRetainedProgressBytes {
			c.stats.RetainedOmittedEvents++
			c.stats.RetainedOmittedBytes += uint64(size)
		} else {
			c.events = append(c.events, event)
			c.retainedBytes += size
			c.retainedItems++
			c.stats.RetainedPeakBytes = c.retainedBytes
			c.stats.RetainedPeakItems = c.retainedItems
		}
	} else {
		c.events = append(c.events, event)
	}
	if c.live != nil {
		if c.stats.LiveStopped || len(c.queue) >= MaxLiveItems || c.queueBytes+size > MaxLiveBytes {
			c.stats.LiveStopped = true
			c.stats.LiveOmittedEvents++
			c.stats.LiveOmittedBytes += uint64(size)
		} else {
			delivered := event
			delivered.Payload = copyPayload(event.Payload)
			c.queue = append(c.queue, queuedEvent{delivered, size})
			c.queueBytes += size
			if c.queueBytes > c.stats.LivePeakBytes {
				c.stats.LivePeakBytes = c.queueBytes
			}
			if len(c.queue) > c.stats.LivePeakItems {
				c.stats.LivePeakItems = len(c.queue)
			}
			select {
			case c.wake <- struct{}{}:
			default:
			}
		}
	}
	event.Payload = copyPayload(event.Payload)
	c.mu.Unlock()
	return event, nil
}
func (c *Collector) prepare(e Event) Event {
	c.sequence++
	e.SchemaVersion = SchemaVersion
	e.Sequence = c.sequence
	e.Timestamp = time.Now().UTC()
	e.RunID = c.runID
	e.GraphID = c.graphID
	e.GraphVersion = c.graphVersion
	e.Payload = copyPayload(e.Payload)
	return e
}
func (c *Collector) Stats() DeliveryStats { c.mu.Lock(); defer c.mu.Unlock(); return c.stats }
func (c *Collector) Snapshot() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	for i, e := range c.events {
		e.Payload = copyPayload(e.Payload)
		out[i] = e
	}
	return out
}

func copyPayload(payload Payload) Payload {
	switch p := payload.(type) {
	case AttemptInputPayload:
		p.IntendedScope = append([]string{}, p.IntendedScope...)
		return p
	case LegacyGateCheckPayload:
		return CloneLegacyGate(p)
	case ReturnEnvelopePayload:
		return CloneEnvelope(p)
	case DeclaredScopeConflictsPayload:
		p.Details = append([]DeclaredScopeConflict{}, p.Details...)
		return p
	case GateCheckPayload:
		return CloneGate(p)
	case CommandPayload:
		if p.ExitCode != nil {
			v := *p.ExitCode
			p.ExitCode = &v
		}
		return p
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
