package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Sink interface{ Append(Event) (Event, error) }

var ErrCollectorFinished = errors.New("event collector is finished")
var ErrTraceEventTooLarge = errors.New("retained event exceeds page byte limit")

const (
	MaxRetainedProgressBytes = 4 << 20
	MaxRetainedProgressItems = 4096
	MaxLiveBytes             = 1 << 20
	MaxLiveItems             = 1024
	MaxTracePageItems        = 256
)

// TracePage is one atomic view of retained facts and their sequence watermark.
// NextAfter is safe to use as the next cursor. When HasMore is false, it skips
// sequence numbers consumed by omitted progress without claiming those facts
// were retained.
type TracePage struct {
	Events      []Event
	EventHashes []string
	NextAfter   uint64
	HasMore     bool
	Watermark   uint64
	Stats       DeliveryStats
	Finished    bool
}

// TraceReader exposes retained facts without allowing a caller to append them.
type TraceReader interface {
	Page(after uint64, limit int) (TracePage, error)
	PageBounded(after uint64, limit, maxEventBytes int) (TracePage, error)
}

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
	eventSizes                   []int
	eventHashes                  []string
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
			markerJSON, _ := json.Marshal(marker)
			c.events = append(c.events, marker)
			c.eventSizes = append(c.eventSizes, len(markerJSON))
			c.eventHashes = append(c.eventHashes, eventJSONHash(markerJSON))
		}
		event = c.prepare(event)
		finalJSON, _ := json.Marshal(event)
		c.events = append(c.events, event)
		c.eventSizes = append(c.eventSizes, len(finalJSON))
		c.eventHashes = append(c.eventHashes, eventJSONHash(finalJSON))
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
			c.eventSizes = append(c.eventSizes, size)
			c.eventHashes = append(c.eventHashes, eventJSONHash(encoded))
			c.retainedBytes += size
			c.retainedItems++
			c.stats.RetainedPeakBytes = c.retainedBytes
			c.stats.RetainedPeakItems = c.retainedItems
		}
	} else {
		c.events = append(c.events, event)
		c.eventSizes = append(c.eventSizes, size)
		c.eventHashes = append(c.eventHashes, eventJSONHash(encoded))
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
func eventJSONHash(encoded []byte) string {
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
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

// Page returns detached retained events from one collector snapshot. Sequence
// gaps can be omitted progress; callers must follow NextAfter until HasMore is
// false before treating Watermark as fully read.
func (c *Collector) Page(after uint64, limit int) (TracePage, error) {
	return c.page(after, limit, 0)
}

// PageBounded selects the largest retained prefix whose encoded event objects
// and array commas fit maxEventBytes. It never copies excluded payloads.
func (c *Collector) PageBounded(after uint64, limit, maxEventBytes int) (TracePage, error) {
	if maxEventBytes < 1 {
		return TracePage{}, errors.New("trace page byte limit must be positive")
	}
	return c.page(after, limit, maxEventBytes)
}

func (c *Collector) page(after uint64, limit, maxEventBytes int) (TracePage, error) {
	if limit < 1 || limit > MaxTracePageItems {
		return TracePage{}, fmt.Errorf("trace page limit must be from 1 to %d", MaxTracePageItems)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if after > c.sequence {
		return TracePage{}, fmt.Errorf("trace cursor %d exceeds watermark %d", after, c.sequence)
	}
	page := TracePage{Watermark: c.sequence, Stats: c.stats, Finished: c.finished}
	start := sort.Search(len(c.events), func(i int) bool { return c.events[i].Sequence > after })
	end := start + limit
	if end > len(c.events) {
		end = len(c.events)
	}
	if maxEventBytes > 0 {
		used := 0
		for i := start; i < end; i++ {
			next := c.eventSizes[i]
			if i > start {
				next++ // JSON array comma
			}
			if next > maxEventBytes-used {
				if i == start {
					return TracePage{}, ErrTraceEventTooLarge
				}
				end = i
				break
			}
			used += next
		}
	}
	page.Events = make([]Event, 0, end-start)
	page.EventHashes = append([]string{}, c.eventHashes[start:end]...)
	for _, event := range c.events[start:end] {
		event.Payload = copyPayload(event.Payload)
		page.Events = append(page.Events, event)
	}
	page.HasMore = end < len(c.events)
	if page.HasMore {
		page.NextAfter = page.Events[len(page.Events)-1].Sequence
	} else {
		page.NextAfter = page.Watermark
	}
	return page, nil
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
