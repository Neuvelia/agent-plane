package runtime

import (
	"context"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

// unsignalledContext reports its parent's cancellation through Err and Cause
// but never closes Done, so a context.AfterFunc registered on it never fires.
// It pins the order the scheduler itself guarantees, without racing the
// AfterFunc goroutine.
type unsignalledContext struct{ context.Context }

func (unsignalledContext) Done() <-chan struct{} { return nil }

// A node cancelled by the run cause is recorded after the run-scoped
// cancel_requested, even when nothing but the scheduler emits it (review of
// #198, finding 3).
//
// roadmap: F3.4-AC2
func TestCancelRequestedPrecedesNodesCancelledByTheRunCause(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		cancel(run.CancelCause{Reason: run.CancelUserCancel})
		return nil, nil
	})
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	options := timeoutOptions(time.Second)
	options.MaxConcurrency = 1
	result, err := Run(unsignalledContext{parent}, def, nil, exec, options)
	if err != nil {
		t.Fatal(err)
	}
	if b := nodeResult(t, result, "b"); b.State != run.NodeCancelled || b.Reason != run.CancelUserCancel {
		t.Fatalf("b = %+v", b)
	}
	req, term := eventIndex(result, events.CancelRequested, ""), eventIndex(result, events.NodeTerminated, "b")
	if req < 0 || term < 0 || req > term {
		t.Fatalf("cancel_requested %d node_terminated %d", req, term)
	}
}
