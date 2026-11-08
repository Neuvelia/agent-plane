package runtime

import (
	"errors"
	"fmt"
	"sort"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

// MaxRetainedAttemptSummaries bounds the independent finalization index.
const MaxRetainedAttemptSummaries = 4096

// ValidateAttemptSummaryBudget rejects graphs whose configured loops can
// create more attempts than a completed RunResult can retain. Callers should
// validate the graph definition first; Run enforces both checks independently.
func ValidateAttemptSummaryBudget(definition *graph.Definition) error {
	if definition == nil {
		return errors.New("attempt summary budget requires a graph definition")
	}
	ids := make([]string, 0, len(definition.Nodes))
	for id := range definition.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	total := 0
	for _, id := range ids {
		loop := definition.Nodes[id].Loop
		if loop == nil {
			continue
		}
		total += loop.Attempts()
		if total > MaxRetainedAttemptSummaries {
			return &PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{
				Severity:   graph.SeverityError,
				Code:       "E_ATTEMPT_SUMMARY_BUDGET",
				GraphID:    definition.ID,
				Location:   graph.DiagnosticLocation{NodeID: id, Path: "nodes." + id + ".loop.max_attempts"},
				Message:    fmt.Sprintf("configured loops exceed the %d attempt history limit", MaxRetainedAttemptSummaries),
				Suggestion: "split the graph into smaller runs or reduce loop attempt budgets",
			}}}}
		}
	}
	return nil
}

func (m *runMachine) beginAttempt(id string, attemptID events.AttemptID) {
	m.attemptIndexes[attemptID] = len(m.result.Attempts)
	m.result.Attempts = append(m.result.Attempts, AttemptSummary{AttemptID: attemptID, NodeID: id, NodeRunID: m.nodes[id].NodeRunID})
}

func (m *runMachine) recordAttemptCheck(attemptID events.AttemptID, fact events.Event, check events.GateCheckPayload) {
	summary := &m.result.Attempts[m.attemptIndexes[attemptID]]
	summary.Check = &AttemptCheckReference{CheckID: check.CheckID, Kind: check.Kind, Verdict: check.Verdict, Sequence: fact.Sequence, IdentityRedacted: fact.IdentityRedacted}
}

func (m *runMachine) recordAttemptEnvelope(attemptID events.AttemptID, fact events.Event, envelope events.ReturnEnvelopePayload) {
	summary := &m.result.Attempts[m.attemptIndexes[attemptID]]
	summary.Envelope = &AttemptEnvelopeReference{UnitID: envelope.UnitID, CheckID: envelope.CheckID, Verdict: envelope.Verdict, Sequence: fact.Sequence, IdentityRedacted: fact.IdentityRedacted}
}

func (m *runMachine) finishAttempt(attemptID events.AttemptID, outcome AttemptOutcome) {
	m.result.Attempts[m.attemptIndexes[attemptID]].Outcome = outcome
}
