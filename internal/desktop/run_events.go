package desktop

import (
	"encoding/json"

	"agentplane/internal/events"
	"agentplane/internal/safeview"
)

// Keep the result below the protocol frame ceiling with room for the response
// envelope and its bounded request identity.
const maxRunEventsResultBytes = 7 << 20
const maxRawReplayCandidateBytes = 4 << 20

// RunEventsPage freezes the first collector watermark and stats. Its canonical
// event objects retain runtime schema and original sequence identities.
type RunEventsPage struct {
	RunID         events.RunID           `json:"run_id"`
	AfterSequence uint64                 `json:"after_sequence"`
	Events        []json.RawMessage      `json:"events"`
	EventHashes   []string               `json:"event_hashes"`
	Omissions     []events.OmissionRange `json:"omissions"`
	NextAfter     uint64                 `json:"next_after"`
	HasMore       bool                   `json:"has_more"`
	Watermark     uint64                 `json:"watermark"`
	Stats         RunDeliveryStats       `json:"stats"`
	Finished      bool                   `json:"finished"`
}

// Events reads retained facts without consuming the lossy live channel. Later
// candidate reads are clipped to the first watermark; retained facts are immutable.
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
	redactions := append([]string(nil), record.redactions...)
	graphID, graphVersion := record.graphID, record.graphVersion
	s.mu.Unlock()
	if reader == nil {
		return RunEventsPage{}, protocolError("unavailable", "Older in-memory run trace is unavailable")
	}
	bounded, ok := reader.(events.BoundedTraceReader)
	if !ok {
		return RunEventsPage{}, protocolError("unavailable", "Safe bounded run trace is unavailable")
	}
	projector, err := safeview.NewProjector(redactions)
	if err != nil {
		return RunEventsPage{}, protocolError("unavailable", "Safe run projection is unavailable")
	}
	candidate, err := bounded.NextBounded(after, maxRawReplayCandidateBytes)
	if err != nil {
		return RunEventsPage{}, protocolError("invalid_request", "Invalid event page cursor")
	}
	result := RunEventsPage{
		RunID: runID, AfterSequence: after, Events: []json.RawMessage{},
		EventHashes: []string{}, Omissions: []events.OmissionRange{},
		NextAfter: after, Watermark: candidate.Watermark,
		Finished: candidate.Finished, Stats: runDeliveryStats(candidate.Stats),
	}
	if after > result.Watermark {
		return RunEventsPage{}, protocolError("invalid_request", "Invalid event page cursor")
	}
	for consumed := 0; consumed < limit && result.NextAfter < result.Watermark; consumed++ {
		if candidate.NextAfter <= result.NextAfter || candidate.NextAfter > candidate.Watermark || candidate.Event != nil && candidate.OversizeSequence != 0 {
			return RunEventsPage{}, protocolError("unavailable", "Run trace integrity is unavailable")
		}
		through := candidate.NextAfter
		if through > result.Watermark {
			through = result.Watermark
		}
		if !candidateCoversGap(candidate, result.NextAfter, through) {
			return RunEventsPage{}, protocolError("unavailable", "Run trace sequence coverage is unavailable")
		}
		trial := result
		trial.Events = append([]json.RawMessage{}, result.Events...)
		trial.EventHashes = append([]string{}, result.EventHashes...)
		trial.Omissions = append([]events.OmissionRange{}, result.Omissions...)
		for _, omission := range candidate.Omissions {
			if omission.From > through {
				break
			}
			if omission.To > through {
				omission.To = through
			}
			if !appendSafeOmission(&trial.Omissions, omission) {
				return RunEventsPage{}, protocolError("unavailable", "Run trace omissions are invalid")
			}
		}
		if candidate.NextAfter <= result.Watermark {
			switch {
			case candidate.Event != nil:
				if candidate.Event.Sequence != candidate.NextAfter || candidate.Event.RunID != runID || candidate.Event.GraphID != graphID || candidate.Event.GraphVersion != graphVersion {
					return RunEventsPage{}, protocolError("unavailable", "Run trace identity is unavailable")
				}
				projected, omission, err := projector.ProjectEvent(*candidate.Event)
				if err != nil {
					return RunEventsPage{}, protocolError("unavailable", "Safe run projection is unavailable")
				}
				if omission != "" {
					if !appendSafeOmission(&trial.Omissions, events.OmissionRange{From: candidate.NextAfter, To: candidate.NextAfter, Reason: omission}) {
						return RunEventsPage{}, protocolError("unavailable", "Run trace omissions are invalid")
					}
				} else {
					trial.Events = append(trial.Events, json.RawMessage(projected.JSON.Bytes()))
					trial.EventHashes = append(trial.EventHashes, projected.Hash)
				}
			case candidate.OversizeSequence != 0:
				if candidate.OversizeSequence != candidate.NextAfter || !appendSafeOmission(&trial.Omissions, events.OmissionRange{From: candidate.NextAfter, To: candidate.NextAfter, Reason: events.OmissionNotRetained}) {
					return RunEventsPage{}, protocolError("unavailable", "Run trace oversize marker is invalid")
				}
			}
		}
		trial.NextAfter = through
		trial.HasMore = trial.NextAfter < trial.Watermark
		encoded, err := json.Marshal(trial)
		if err != nil {
			return RunEventsPage{}, protocolError("unavailable", "Run trace cannot be encoded")
		}
		if len(encoded) > maxRunEventsResultBytes {
			if result.NextAfter == after {
				return RunEventsPage{}, protocolError("response_too_large", "Retained page exceeds frame limit")
			}
			break
		}
		result = trial
		if result.NextAfter == result.Watermark {
			break
		}
		candidate, err = bounded.NextBounded(result.NextAfter, maxRawReplayCandidateBytes)
		if err != nil {
			return RunEventsPage{}, protocolError("unavailable", "Run trace is unavailable")
		}
	}
	result.HasMore = result.NextAfter < result.Watermark
	if result.NextAfter == after && result.Watermark == after {
		result.HasMore = false
	}
	return result, nil
}

func candidateCoversGap(candidate events.BoundedCandidate, after, through uint64) bool {
	gapEnd := through
	if candidate.NextAfter <= through && (candidate.Event != nil || candidate.OversizeSequence != 0) {
		gapEnd--
	}
	covered := after
	for _, omission := range candidate.Omissions {
		if omission.From > through {
			if candidate.NextAfter <= through {
				return false
			}
			break
		}
		end := omission.To
		if end > through {
			end = through
		}
		if omission.From != covered+1 || end < omission.From || omission.Reason != events.OmissionSensitiveIdentity && omission.Reason != events.OmissionNotRetained {
			return false
		}
		covered = end
	}
	return covered == gapEnd
}

func appendSafeOmission(target *[]events.OmissionRange, omission events.OmissionRange) bool {
	if omission.From == 0 || omission.From > omission.To || omission.Reason != events.OmissionSensitiveIdentity && omission.Reason != events.OmissionNotRetained {
		return false
	}
	items := *target
	if len(items) > 0 {
		last := &items[len(items)-1]
		if omission.From <= last.To {
			return false
		}
		if last.To+1 == omission.From && last.Reason == omission.Reason {
			last.To = omission.To
			return true
		}
	}
	*target = append(items, omission)
	return true
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
