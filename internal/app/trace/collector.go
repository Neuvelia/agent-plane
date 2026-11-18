// Package trace sequences, stamps, bounds and delivers a run's events. It owns
// the Collector; the domain events package owns what an event is.
package trace

import (
	"agentplane/internal/domain/events"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Clock supplies event timestamps; the engine injects the system clock and tests
// inject a fixed one.
type Clock interface{ Now() time.Time }

// Sink receives sequenced events.
type Sink interface {
	Append(events.Event) (events.Event, error)
}

// ErrCollectorFinished is returned for events appended after run_finished.
var ErrCollectorFinished = errors.New("event collector is finished")

// Bounds on retained progress and on the live delivery queue.
const (
	MaxRetainedProgressBytes = 4 << 20
	MaxRetainedProgressItems = 4096
	MaxLiveBytes             = 1 << 20
	MaxLiveItems             = 1024
)

type queuedEvent struct {
	event events.Event
	bytes int
}

// Collector retains lifecycle/evidence independently from bounded progress.
// Live delivery is a bounded FIFO prefix, including the in-flight head.
type Collector struct {
	mu                           sync.Mutex
	clock                        Clock
	runID                        events.RunID
	graphID, graphVersion        string
	events                       []events.Event
	sequence                     uint64
	finished, finalizing         bool
	retainedBytes, retainedItems int
	stats                        events.DeliveryStats
	live                         chan<- events.Event
	queue                        []queuedEvent
	queueBytes                   int
	wake, stop, done             chan struct{}
	persist                      func(events.Event) error
	persistErr                   error
}

// NewCollector returns a collector without live delivery.
func NewCollector(clock Clock, runID events.RunID, graphID, graphVersion string) *Collector {
	return &Collector{clock: clock, runID: runID, graphID: graphID, graphVersion: graphVersion}
}

// NewCollectorWithLiveEvents also delivers events to live. The caller owns live
// and may close it only after finalization returns.
// Append never waits for a reader. Final run/omission facts are replay-only.
func NewCollectorWithLiveEvents(ctx context.Context, clock Clock, runID events.RunID, graphID, graphVersion string, live chan<- events.Event) *Collector {
	c := NewCollector(clock, runID, graphID, graphVersion)
	c.live = live
	if live != nil {
		c.wake = make(chan struct{}, 1)
		c.stop = make(chan struct{})
		c.done = make(chan struct{})
		go c.pump(ctx)
	}
	return c
}

// SetPersist registers a synchronous, ordered recorder for every retained
// event (lifecycle and bounded progress, including the final progress marker),
// called under the collector lock so the durable order equals the sequence
// order. It must be set before the first Append. After the first error the
// recorder is no longer called and PersistError reports the cause.
func (c *Collector) SetPersist(f func(events.Event) error) { c.persist = f }

// PersistError is the first recorder failure, or nil.
func (c *Collector) PersistError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.persistErr
}

// record must be called with c.mu held for an event that was just retained.
func (c *Collector) record(e events.Event) {
	if c.persist == nil || c.persistErr != nil {
		return
	}
	e.Payload = events.ClonePayload(e.Payload)
	if err := c.persist(e); err != nil {
		c.persistErr = err
	}
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

// Append validates an event, stamps it and retains it. Run-finished events
// finalize the collector: pending live events are dropped as omissions and the
// final facts are replay-only.
func (c *Collector) Append(event events.Event) (events.Event, error) {
	c.mu.Lock()
	if c.finished || c.finalizing {
		c.mu.Unlock()
		return events.Event{}, ErrCollectorFinished
	}
	event.SchemaVersion = events.SchemaVersion
	event.RunID = c.runID
	event.GraphID = c.graphID
	event.GraphVersion = c.graphVersion
	if err := event.Validate(); err != nil {
		c.mu.Unlock()
		return events.Event{}, fmt.Errorf("append event: %w", err)
	}
	if event.Type == events.RunFinished {
		return c.finalize(event), nil
	}
	event = c.prepare(event)
	encoded, _ := json.Marshal(event)
	c.retain(event, len(encoded))
	c.enqueueLive(event, len(encoded))
	event.Payload = events.ClonePayload(event.Payload)
	c.mu.Unlock()
	return event, nil
}

// finalize is called with c.mu held and releases it. The lock is dropped while
// the live pump drains so a slow reader cannot block the run.
func (c *Collector) finalize(event events.Event) events.Event {
	c.finalizing = true
	if c.stop != nil {
		close(c.stop)
	}
	c.mu.Unlock()
	if c.done != nil {
		<-c.done
	}
	c.mu.Lock()
	c.omitUndelivered()
	if c.stats.RetainedOmittedEvents > 0 || c.stats.LiveOmittedEvents > 0 {
		c.retainAlways(c.prepare(events.Event{Type: events.ProgressOmitted, Payload: events.ProgressOmittedPayload{RetainedEvents: c.stats.RetainedOmittedEvents, RetainedBytes: c.stats.RetainedOmittedBytes, LiveEvents: c.stats.LiveOmittedEvents, LiveBytes: c.stats.LiveOmittedBytes, LiveStopped: c.stats.LiveStopped}}))
	}
	event = c.prepare(event)
	c.retainAlways(event)
	c.finished = true
	c.mu.Unlock()
	return event
}

func (c *Collector) omitUndelivered() {
	for _, q := range c.queue {
		c.stats.LiveOmittedEvents++
		c.stats.LiveOmittedBytes += uint64(q.bytes)
	}
	if len(c.queue) > 0 {
		c.stats.LiveStopped = true
	}
	c.queue = nil
	c.queueBytes = 0
}

func (c *Collector) retainAlways(event events.Event) {
	c.events = append(c.events, event)
	c.record(event)
}

// retain keeps lifecycle and evidence events unconditionally and progress
// events only within the retained bounds.
func (c *Collector) retain(event events.Event, size int) {
	if !events.IsProgress(event.Type) {
		c.retainAlways(event)
		return
	}
	if c.retainedItems >= MaxRetainedProgressItems || c.retainedBytes+size > MaxRetainedProgressBytes {
		c.stats.RetainedOmittedEvents++
		c.stats.RetainedOmittedBytes += uint64(size)
		return
	}
	c.retainAlways(event)
	c.retainedBytes += size
	c.retainedItems++
	c.stats.RetainedPeakBytes = c.retainedBytes
	c.stats.RetainedPeakItems = c.retainedItems
}

// enqueueLive adds the event to the bounded live FIFO, or stops live delivery
// and counts the omission once a bound is exceeded.
func (c *Collector) enqueueLive(event events.Event, size int) {
	if c.live == nil {
		return
	}
	if c.stats.LiveStopped || len(c.queue) >= MaxLiveItems || c.queueBytes+size > MaxLiveBytes {
		c.stats.LiveStopped = true
		c.stats.LiveOmittedEvents++
		c.stats.LiveOmittedBytes += uint64(size)
		return
	}
	delivered := event
	delivered.Payload = events.ClonePayload(event.Payload)
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

func (c *Collector) prepare(e events.Event) events.Event {
	c.sequence++
	e.SchemaVersion = events.SchemaVersion
	e.Sequence = c.sequence
	e.Timestamp = c.clock.Now().UTC()
	e.RunID = c.runID
	e.GraphID = c.graphID
	e.GraphVersion = c.graphVersion
	e.Payload = events.ClonePayload(e.Payload)
	return e
}

// Stats returns the delivery counters so far.
func (c *Collector) Stats() events.DeliveryStats { c.mu.Lock(); defer c.mu.Unlock(); return c.stats }

// Snapshot returns a copy of the retained events in sequence order.
func (c *Collector) Snapshot() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.Event, len(c.events))
	for i, e := range c.events {
		e.Payload = events.ClonePayload(e.Payload)
		out[i] = e
	}
	return out
}
