package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/history"
	"agentplane/internal/runtime"
	"agentplane/internal/safeview"
)

// finalTrace is assembled exclusively from the canonical bounded runs.events
// projection. Original-sequence omission ranges remain compressed in memory.
type finalTrace struct {
	Events    []history.Event
	Omissions []history.OmissionRange
	Watermark uint64
	Complete  bool
}

func (s *RunService) collectFinalTrace(runID events.RunID) (finalTrace, error) {
	var result finalTrace
	after := uint64(0)
	for {
		page, serviceErr := s.Events(runID, after, events.MaxTracePageItems)
		if serviceErr != nil {
			return finalTrace{}, errors.New("safe final trace page is unavailable")
		}
		if !page.Finished || page.Watermark == 0 || page.Watermark > math.MaxInt64 || page.AfterSequence != after ||
			page.NextAfter <= after || page.NextAfter > page.Watermark || len(page.Events) != len(page.EventHashes) {
			return finalTrace{}, errors.New("safe final trace is not finished or has invalid coverage")
		}
		if result.Watermark == 0 {
			result.Watermark = page.Watermark
		} else if page.Watermark != result.Watermark {
			return finalTrace{}, errors.New("safe final trace watermark changed")
		}
		for index, raw := range page.Events {
			var identity struct {
				Sequence uint64 `json:"sequence"`
			}
			if err := json.Unmarshal(raw, &identity); err != nil || identity.Sequence <= after || identity.Sequence > page.NextAfter {
				return finalTrace{}, errors.New("safe final trace event identity is invalid")
			}
			projection, err := history.ParseSafeJSON(raw, nil)
			if err != nil || !bytes.Equal(projection.Bytes(), raw) {
				return finalTrace{}, errors.New("safe final trace event bytes are not canonical")
			}
			sum := sha256.Sum256(raw)
			if page.EventHashes[index] != "sha256:"+hex.EncodeToString(sum[:]) {
				return finalTrace{}, errors.New("safe final trace event hash changed")
			}
			result.Events = append(result.Events, history.Event{Sequence: identity.Sequence, Projection: projection})
		}
		for _, omission := range page.Omissions {
			if omission.From <= after || omission.To > page.NextAfter || omission.From > omission.To ||
				omission.Reason != events.OmissionSensitiveIdentity && omission.Reason != events.OmissionNotRetained {
				return finalTrace{}, errors.New("safe final trace omission is invalid")
			}
			if count := len(result.Omissions); count > 0 && result.Omissions[count-1].To+1 == omission.From && result.Omissions[count-1].Reason == omission.Reason {
				result.Omissions[count-1].To = omission.To
			} else {
				result.Omissions = append(result.Omissions, history.OmissionRange{From: omission.From, To: omission.To, Reason: omission.Reason})
			}
		}
		if page.NextAfter == page.Watermark {
			if page.HasMore {
				return finalTrace{}, errors.New("safe final trace cursor is inconsistent")
			}
			break
		}
		if !page.HasMore {
			return finalTrace{}, errors.New("safe final trace ended before watermark")
		}
		after = page.NextAfter
	}
	if !exactFinalTraceCoverage(result) {
		return finalTrace{}, errors.New("safe final trace does not cover every source sequence")
	}
	result.Complete = len(result.Omissions) == 0
	return result, nil
}

func exactFinalTraceCoverage(trace finalTrace) bool {
	type span struct{ from, to uint64 }
	spans := make([]span, 0, len(trace.Events)+len(trace.Omissions))
	for _, event := range trace.Events {
		spans = append(spans, span{event.Sequence, event.Sequence})
	}
	for _, omission := range trace.Omissions {
		spans = append(spans, span{omission.From, omission.To})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
	covered := uint64(0)
	for _, item := range spans {
		if item.from != covered+1 || item.to < item.from || item.to > trace.Watermark {
			return false
		}
		covered = item.to
	}
	return covered == trace.Watermark
}

func historyOutcomeStatusesSafe(redactions []string) bool {
	redactor, err := runtime.NewCaptureRedactor(redactions)
	if err != nil {
		return false
	}
	for _, status := range []string{"succeeded", "failed", "skipped", "cancelled", "accepted", "rejected", "needs_intervention", "execution_error"} {
		if !bytes.Equal(redactor.Redact([]byte(status)), []byte(status)) {
			return false
		}
	}
	return true
}

type historyFinalizer interface {
	Finalize(context.Context, history.Finalization) error
}

// finalizeRun retains only typed, bounded projections. A failed write leaves
// the Launch marker active for crash recovery and keeps later Starts closed.
func (s *RunService) finalizeRun(result runtime.RunResult, redactions []string, graphComplete, launchComplete, evidenceComplete bool) error {
	writer, ok := s.history.(historyFinalizer)
	if !ok {
		return errors.New("history finalizer is unavailable")
	}
	projector, err := safeview.NewProjector(redactions)
	if err != nil {
		return err
	}
	projected, err := projector.ProjectResult(result)
	if err != nil {
		return err
	}
	attempts := make([]history.AttemptOutcome, 0, len(result.Attempts))
	attemptComplete := true
	for _, summary := range result.Attempts {
		outcome, complete, err := projector.ProjectAttemptSummary(summary)
		if err != nil {
			return err
		}
		attempts = append(attempts, outcome)
		attemptComplete = attemptComplete && complete
	}
	trace, err := s.collectFinalTrace(result.RunID)
	if err != nil {
		return err
	}
	bits, err := json.Marshal([]any{1, graphComplete, launchComplete, projected.Complete, trace.Complete, attemptComplete, evidenceComplete})
	if err != nil {
		return err
	}
	completeness, err := history.ParseSafeJSON(bits, redactions)
	if err != nil {
		return err
	}
	final := history.Finalization{RunID: string(result.RunID), EndedAt: time.Now().UTC(),
		RuntimeState: string(result.State), Watermark: trace.Watermark, Result: projected.Result,
		Completeness: completeness, Events: trace.Events, OmissionRanges: trace.Omissions,
		Nodes: projected.Nodes, Attempts: attempts}
	commitCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	return writer.Finalize(commitCtx, final)
}
