package desktop

import (
	"encoding/json"
	"errors"

	"agentplane/internal/events"
)

// Keep the result below the protocol frame ceiling with room for the response
// envelope and its bounded request identity.
const maxRunEventsResultBytes = 7 << 20
const maxRunEventsEventBytes = maxRunEventsResultBytes - 32768

// RunEventsPage is a transport view of one atomic retained trace page. The
// event objects retain their runtime schema and sequence identities.
type RunEventsPage struct {
	RunID         events.RunID     `json:"run_id"`
	AfterSequence uint64           `json:"after_sequence"`
	Events        []events.Event   `json:"events"`
	EventHashes   []string         `json:"event_hashes"`
	NextAfter     uint64           `json:"next_after"`
	HasMore       bool             `json:"has_more"`
	Watermark     uint64           `json:"watermark"`
	Stats         RunDeliveryStats `json:"stats"`
	Finished      bool             `json:"finished"`
}

// Events reads the current retained trace without consuming its lossy live
// channel. Byte selection and the resulting page share one collector snapshot.
func (s *RunService) Events(runID events.RunID, after uint64, limit int) (RunEventsPage, *ServiceError) {
	if s == nil || runID == "" {
		return RunEventsPage{}, protocolError("not_found", "Run identity was not found")
	}
	if limit < 1 || limit > events.MaxTracePageItems {
		return RunEventsPage{}, protocolError("invalid_request", "Invalid event page limit")
	}
	s.mu.Lock()
	record := s.byID[runID]
	if record == nil {
		s.mu.Unlock()
		return RunEventsPage{}, protocolError("not_found", "Run identity was not found")
	}
	reader := record.reader
	s.mu.Unlock()
	if reader == nil {
		return RunEventsPage{}, protocolError("unavailable", "Older in-memory run trace is unavailable")
	}
	page, err := reader.PageBounded(after, limit, maxRunEventsEventBytes)
	if errors.Is(err, events.ErrTraceEventTooLarge) {
		return RunEventsPage{}, protocolError("response_too_large", "Retained event exceeds page frame limit")
	}
	if err != nil {
		return RunEventsPage{}, protocolError("invalid_request", "Invalid event page cursor")
	}
	if len(page.EventHashes) != len(page.Events) {
		return RunEventsPage{}, protocolError("unavailable", "Run trace integrity is unavailable")
	}
	result := RunEventsPage{
		RunID: runID, AfterSequence: after, Events: page.Events,
		EventHashes: page.EventHashes,
		NextAfter:   page.NextAfter, HasMore: page.HasMore,
		Watermark: page.Watermark, Finished: page.Finished,
		Stats: runDeliveryStats(page.Stats),
	}
	if result.Events == nil {
		result.Events = []events.Event{}
	}
	if result.EventHashes == nil {
		result.EventHashes = []string{}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return RunEventsPage{}, protocolError("unavailable", "Run trace cannot be encoded")
	}
	if len(encoded) > maxRunEventsResultBytes {
		return RunEventsPage{}, protocolError("response_too_large", "Retained page exceeds frame limit")
	}
	return result, nil
}

func runDeliveryStats(stats events.DeliveryStats) RunDeliveryStats {
	return RunDeliveryStats{
		RetainedOmittedEvents: stats.RetainedOmittedEvents,
		RetainedOmittedBytes:  stats.RetainedOmittedBytes,
		LiveOmittedEvents:     stats.LiveOmittedEvents,
		LiveOmittedBytes:      stats.LiveOmittedBytes,
		LiveStopped:           stats.LiveStopped,
	}
}
