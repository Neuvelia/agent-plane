package runtime

import (
	"context"
	"sort"

	"agentplane/internal/events"
	"agentplane/internal/gitstate"
)

// batchState belongs to the coordinator and survives retries of one batch.
type batchState struct {
	attempts  map[string]events.AttemptID
	numbers   map[string]int
	feedbacks map[string]*CorrectionFeedback
	baselines map[string]*gitstate.Baseline
}

func (m *runMachine) runBatchWorkers(ctx context.Context, executor Executor, batch []string, state *batchState, redactor *logRedactor) []completedNode {
	// Only workers run concurrently; the coordinator owns every state and
	// output map. Buffering lets all workers return before result collection.
	completed := make(chan completedNode, len(batch))
	started := 0
	for _, id := range batch {
		if ctx.Err() != nil {
			break
		}
		if m.nodes[id].State == NodeReady {
			m.transitionNode(id, NodeRunning)
		}
		if l := m.definition.Nodes[id].Loop; l != nil {
			aid, e := events.NewAttemptID()
			if e != nil {
				m.nodes[id].Failure = &Failure{NodeID: id, Kind: FailureScheduler, Err: e}
				m.transitionNode(id, NodeFailed)
				continue
			}
			if m.units[id] != nil {
				if e := m.units.begin(id, aid); e != nil {
					m.nodes[id].Failure = &Failure{NodeID: id, Kind: FailureScheduler, Err: e}
					m.transitionNode(id, NodeFailed)
					continue
				}
			}
			state.attempts[id] = aid
			state.numbers[id]++
			m.appendEvent(events.Event{Type: events.AttemptStarted, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: aid, Payload: events.AttemptStartedPayload{Number: state.numbers[id], MaxAttempts: l.Attempts()}})
			m.appendEvent(events.Event{Type: events.AttemptInput, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: aid, Payload: attemptInput(m.definition.Nodes[id], m.resolved[id], state.feedbacks[id], redactor)})
		}
		node := m.definition.Nodes[id]
		resolved := m.resolved[id]
		aid, feedback, baseline, number := state.attempts[id], state.feedbacks[id], state.baselines[id], state.numbers[id]
		go func(id string) {
			completed <- m.cycle(ctx, executor, id, node, resolved, aid, number, feedback, baseline, redactor)
		}(id)
		started++
	}
	results := make([]completedNode, 0, started)
	for range started {
		results = append(results, <-completed)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].id < results[j].id })
	return results
}
