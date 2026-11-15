package runtime

import (
	"context"
	"errors"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

// acceptBatchResults runs on the coordinator after worker results are sorted.
// It returns only nodes eligible for another bounded attempt.
func (machine *runMachine) acceptBatchResults(ctx context.Context, definition *graph.Definition, results []completedNode, state *batchState) []string {
	var retry []string
	for _, work := range results {
		if work.baseline != nil {
			state.baselines[work.id] = work.baseline
		}
		nodeResult := machine.nodes[work.id]
		if definition.Nodes[work.id].Loop != nil {
			outcome := "execution_error"
			if work.err != nil && ctx.Err() != nil && (errors.Is(work.err, context.Canceled) || errors.Is(work.err, context.DeadlineExceeded)) {
				outcome = "cancelled"
			}
			if work.check != nil && !work.checkInvalid {
				machine.appendEvent(events.Event{Type: events.GateCheck, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: events.CloneGate(*work.check)})
				if machine.units[work.id] != nil {
					envelope, err := machine.units.envelope(work.id, work.attemptID, *work.check)
					if err == nil {
						err = machine.units.publish(envelope, *work.check, work.outputs)
					}
					if err != nil {
						work.err = err
						work.outputs = nil
						work.check = nil
						work.checkInvalid = true
					} else {
						machine.appendEvent(events.Event{Type: events.ReturnEnvelope, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: envelope})
					}
				}
				if work.check != nil && work.check.Verdict == "red" {
					outcome = "rejected"
					if state.numbers[work.id] >= definition.Nodes[work.id].Loop.Attempts() {
						outcome = "needs_intervention"
					}
					if ctx.Err() != nil {
						outcome = "cancelled"
						work.err = ctx.Err()
					}
				}
				if work.check != nil && work.check.Verdict == "green" {
					outcome = "accepted"
				}
			}
			if work.evidence != nil {
				machine.appendEvent(events.Event{Type: events.NodeEvidence, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: *work.evidence})
				work.evidence = nil
			}
			checkID := ""
			if work.check != nil {
				checkID = work.check.CheckID
			}
			machine.appendEvent(events.Event{Type: events.AttemptFinished, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: events.AttemptFinishedPayload{Outcome: outcome, CheckID: checkID}})
			if outcome == "rejected" && ctx.Err() == nil {
				state.feedbacks[work.id] = &CorrectionFeedback{PreviousAttemptID: work.attemptID, CheckResult: events.CloneGate(*work.check), IntendedScope: originalScope(definition.Nodes[work.id])}
				retry = append(retry, work.id)
				continue
			}
			if outcome == "needs_intervention" {
				nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureNeedsIntervention, Err: interventionError{}}
				machine.transitionNode(work.id, NodeFailed)
				continue
			}
			if outcome == "rejected" {
				work.err = ctx.Err()
			}
			work.check = nil
			work.checkInvalid = false
		}
		if work.check != nil || work.checkInvalid {
			valid := !work.checkInvalid && work.check != nil && definition.Nodes[work.id].Type == "gate" && work.evidence == nil
			if valid && work.check.Verdict == "red" {
				var verdict interface{ GateVerdict() string }
				valid = errors.As(work.err, &verdict) && verdict.GateVerdict() == "red" && len(work.outputs) == 0 && !errors.Is(work.err, context.Canceled) && !errors.Is(work.err, context.DeadlineExceeded)
			}
			if valid && work.check.Verdict == "green" {
				valid = work.err == nil && len(validateNodeOutputs(definition.ID, work.id, definition.Nodes[work.id].Outputs, work.outputs)) == 0
				for k, v := range work.outputs {
					t, ok := v.AsString()
					valid = valid && k == "verdict" && ok && t == "green"
				}
			}
			if !valid {
				work.err = errors.New("invalid or inconsistent gate observation")
				work.outputs = nil
			} else {
				machine.appendEvent(events.Event{Type: events.GateCheck, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, Payload: *work.check})
			}
		} else if definition.Nodes[work.id].Type == "gate" && work.err == nil {
			work.err = errors.New("gate execution missing check observation")
			work.outputs = nil
		}
		if work.err != nil {
			if ctx.Err() != nil && (errors.Is(work.err, context.Canceled) || errors.Is(work.err, context.DeadlineExceeded)) {
				machine.transitionNode(work.id, NodeCancelled)
			} else {
				nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureExecutor, Err: work.err}
				if errors.As(work.err, &rejectedError{}) {
					nodeResult.Failure.Kind = FailureRejected
				}
				if len(work.diagnostics) > 0 {
					nodeResult.Failure.Kind = FailureOutputContract
					nodeResult.Failure.Diagnostics = work.diagnostics
				}
				machine.transitionNode(work.id, NodeFailed)
			}
			continue
		}
		diagnostics := validateNodeOutputs(definition.ID, work.id, definition.Nodes[work.id].Outputs, work.outputs)
		if len(diagnostics) > 0 {
			nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureOutputContract, Diagnostics: diagnostics}
			machine.transitionNode(work.id, NodeFailed)
			continue
		}
		machine.outputs[work.id] = copyValues(work.outputs)
		nodeResult.Outputs = sortedPortValues(work.outputs)
		if u := machine.units[work.id]; u != nil {
			values, ports, err := machine.units.acceptedValues(work.id)
			if err != nil {
				nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureScheduler, Err: err}
				machine.transitionNode(work.id, NodeFailed)
				continue
			}
			machine.outputs[work.id] = values
			nodeResult.Outputs = ports
			nodeResult.AcceptedAttemptID = u.accepted
		}
		if work.evidence != nil {
			machine.appendEvent(events.Event{Type: events.NodeEvidence, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, Payload: *work.evidence})
		}
		acceptedAttempt := work.attemptID
		if nodeResult.AcceptedAttemptID != "" {
			acceptedAttempt = nodeResult.AcceptedAttemptID // matches AcceptedProvenance
		}
		machine.recordAcceptedOutputs(work.id, acceptedAttempt)
		machine.transitionNode(work.id, NodeSucceeded)
	}
	return retry
}
