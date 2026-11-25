package runtime

import (
	"agentplane/internal/domain/run"
	"context"
	"sort"

	"agentplane/internal/domain/events"
	"agentplane/internal/gitstate"
)

// batchState belongs to the coordinator and survives retries of one batch.
type batchState struct {
	attempts  map[string]events.AttemptID
	numbers   map[string]int
	feedbacks map[string]*run.CorrectionFeedback
	baselines map[string]*gitstate.Baseline
	diffs     map[string]DiffSnapshot // previous attempt's after snapshot
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
		if m.nodes[id].State == run.NodeReady {
			m.transitionNode(id, run.NodeRunning)
		}
		if m.isolation.nodes[id] && m.worktrees[id] == nil {
			if e := m.prepareIsolated(ctx, id); e != nil {
				m.endUnprepared(ctx, id, e)
				continue
			}
		}
		if l := m.definition.Nodes[id].Loop; l != nil {
			aid, e := m.attemptIDs.NewAttemptID()
			if e != nil {
				m.nodes[id].Failure = &run.Failure{NodeID: id, Kind: run.FailureScheduler, Err: e}
				m.transitionNode(id, run.NodeFailed)
				continue
			}
			if m.units[id] != nil {
				if e := m.units.begin(id, aid); e != nil {
					m.nodes[id].Failure = &run.Failure{NodeID: id, Kind: run.FailureScheduler, Err: e}
					m.transitionNode(id, run.NodeFailed)
					continue
				}
			}
			state.attempts[id] = aid
			state.numbers[id]++
			m.appendEvent(events.Event{Type: events.AttemptStarted, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: aid, Payload: events.AttemptStartedPayload{Number: state.numbers[id], MaxAttempts: l.Attempts()}})
			m.appendEvent(events.Event{Type: events.AttemptInput, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: aid, Payload: attemptInput(m.definition.Nodes[id], m.resolved[id], state.feedbacks[id], redactor)})
		}
		node := m.definition.Nodes[id]
		if w := m.worktrees[id]; w != nil {
			node = isolatedNode(node, w.path)
		}
		resolved := m.resolved[id]
		aid, feedback, baseline, number, carried := state.attempts[id], state.feedbacks[id], state.baselines[id], state.numbers[id], state.diffs[id]
		go func(id string) {
			request := scanRequest{id: id, node: node, number: number, redactor: redactor}
			completed <- m.scanned(ctx, request, func() completedNode {
				return m.cycle(ctx, executor, id, node, resolved, aid, number, feedback, baseline, carried, redactor)
			})
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
