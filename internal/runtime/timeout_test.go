package runtime

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/proctree"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func timeoutOptions() Options {
	return Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}}
}

func shortTimeouts(t *testing.T, bound time.Duration) {
	t.Helper()
	oldUnit, oldBound := timeoutUnit, abandonBound
	timeoutUnit, abandonBound = 20*time.Millisecond, bound
	t.Cleanup(func() { timeoutUnit, abandonBound = oldUnit, oldBound })
}

func intp(v int) *int { return &v }

func eventIndex(result run.Result, typ events.Type, node string) int {
	for i, e := range result.Events {
		if e.Type == typ && e.NodeID == node {
			return i
		}
	}
	return -1
}

func waitForCtx(ctx context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestNodeDeadlineFailsOnlyThatNodeWithTimeoutReason(t *testing.T) {
	shortTimeouts(t, time.Second)
	def := definition(map[string]graph.NodeDefinition{"slow": {Type: "shell", TimeoutSeconds: intp(1)}, "fast": {Type: "shell"}})
	exec := executorFunc(func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "fast" {
			return nil, nil
		}
		return waitForCtx(ctx, id, n, in)
	})
	result, err := Run(context.Background(), def, nil, exec, timeoutOptions())
	if err != nil {
		t.Fatal(err)
	}
	slow := nodeResult(t, result, "slow")
	if slow.State != run.NodeFailed || slow.Failure == nil || slow.Failure.Kind != run.FailureTimeout || slow.Reason != run.CancelNodeTimeout {
		t.Fatalf("slow = %+v", slow)
	}
	if nodeResult(t, result, "fast").State != run.NodeSucceeded {
		t.Fatalf("fast = %+v", nodeResult(t, result, "fast"))
	}
	if result.State != run.RunFailed || result.Reason != run.CancelNodeTimeout {
		t.Fatalf("run = %v reason %q", result.State, result.Reason)
	}
	req, fin := eventIndex(result, events.CancelRequested, "slow"), eventIndex(result, events.NodeFinished, "slow")
	if req < 0 || fin < 0 || req > fin {
		t.Fatalf("cancel_requested %d node_finished %d", req, fin)
	}
	payload := result.Events[fin].Payload.(events.NodeFinishedPayload)
	if payload.Status != "failed" || payload.FailureKind != "timeout" || payload.Reason != "node_timeout" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestOrdinaryFailureHasNoTimeoutReason(t *testing.T) {
	shortTimeouts(t, time.Second)
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell", TimeoutSeconds: intp(3600)}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, errors.New("node_timeout")
	})
	result, _ := Run(context.Background(), def, nil, exec, timeoutOptions())
	a := nodeResult(t, result, "a")
	if a.State != run.NodeFailed || a.Failure.Kind != run.FailureExecutor || a.Reason != "" || result.Reason != "" {
		t.Fatalf("a = %+v run reason %q", a, result.Reason)
	}
}

func TestRunDeadlineCancelsWithRunTimeoutReason(t *testing.T) {
	shortTimeouts(t, time.Second)
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	def.Limits = &graph.LimitsDefinition{RunTimeoutSeconds: intp(1)}
	result, err := Run(context.Background(), def, nil, executorFunc(waitForCtx), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != run.RunCancelled || result.Reason != run.CancelRunTimeout {
		t.Fatalf("run = %v reason %q", result.State, result.Reason)
	}
	for _, n := range result.Nodes {
		if n.State != run.NodeCancelled || n.Reason != run.CancelRunTimeout {
			t.Fatalf("node = %+v", n)
		}
	}
	finished, req, last := 0, -1, -1
	for i, e := range result.Events {
		switch e.Type {
		case events.CancelRequested:
			req = i
			p := e.Payload.(events.CancelRequestedPayload)
			if p.Scope != "run" || p.Reason != "run_timeout" || p.Source != "deadline" {
				t.Fatalf("cancel_requested = %+v", p)
			}
		case events.RunFinished:
			finished++
			last = i
			if p := e.Payload.(events.RunFinishedPayload); p.Status != "cancelled" || p.Reason != "run_timeout" || p.FailureKind != "" {
				t.Fatalf("run_finished = %+v", p)
			}
		case events.NodeFinished, events.NodeTerminated:
			if req < 0 {
				t.Fatal("node terminal before cancel_requested")
			}
		}
	}
	if finished != 1 || req < 0 || last != len(result.Events)-1 {
		t.Fatalf("finished=%d req=%d last=%d of %d", finished, req, last, len(result.Events))
	}
}

func TestCancelReasonComesFromTypedCauseNotErrorText(t *testing.T) {
	shortTimeouts(t, time.Second)
	for _, tc := range []struct {
		name  string
		cause error
		want  run.CancelReason
	}{
		{"interrupt", run.CancelCause{Reason: run.CancelInterrupt}, run.CancelInterrupt},
		{"user cancel", run.CancelCause{Reason: run.CancelUserCancel}, run.CancelUserCancel},
		{"untyped", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			started := make(chan struct{})
			exec := executorFunc(func(ctx context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
				close(started)
				<-ctx.Done()
				return nil, errors.Join(errors.New("run_timeout"), ctx.Err()) // misleading text
			})
			done := make(chan run.Result, 1)
			go func() {
				r, _ := Run(ctx, definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}}), nil, exec, timeoutOptions())
				done <- r
			}()
			<-started
			cancel(tc.cause)
			result := <-done
			if result.State != run.RunCancelled || result.Reason != tc.want {
				t.Fatalf("state %v reason %q want %q", result.State, result.Reason, tc.want)
			}
			if got := eventIndex(result, events.CancelRequested, "") >= 0; got != (tc.want != "") {
				t.Fatalf("cancel_requested present = %v", got)
			}
		})
	}
}

func TestExecutorIgnoringContextIsAbandonedAsShutdown(t *testing.T) {
	shortTimeouts(t, 30*time.Millisecond)
	release := make(chan struct{})
	def := definition(map[string]graph.NodeDefinition{"stuck": {Type: "shell", TimeoutSeconds: intp(1)}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		<-release
		return nil, nil
	})
	done := make(chan run.Result, 1)
	go func() { r, _ := Run(context.Background(), def, nil, exec, timeoutOptions()); done <- r }()
	var result run.Result
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("scheduler held by an executor that ignores its context")
	}
	close(release)
	n := nodeResult(t, result, "stuck")
	if n.State != run.NodeFailed || n.Failure.Kind != run.FailureShutdown || n.Reason != "" {
		t.Fatalf("node = %+v", n)
	}
}

type terminationError struct{ outcome proctree.Outcome }

func (e terminationError) Error() string                         { return "terminated" }
func (e terminationError) Unwrap() error                         { return context.DeadlineExceeded }
func (e terminationError) ProcessTermination() *proctree.Outcome { return &e.outcome }

func TestProcessTerminatedIsEmittedBetweenCancelRequestedAndTerminal(t *testing.T) {
	shortTimeouts(t, time.Second)
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell", TimeoutSeconds: intp(1)}})
	exec := executorFunc(func(ctx context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		<-ctx.Done()
		return nil, terminationError{proctree.Outcome{Method: proctree.MethodForced, Grace: time.Second, Elapsed: 2 * time.Second}}
	})
	result, _ := Run(context.Background(), def, nil, exec, timeoutOptions())
	req, term, fin := eventIndex(result, events.CancelRequested, "a"), eventIndex(result, events.ProcessTerminated, "a"), eventIndex(result, events.NodeFinished, "a")
	if req < 0 || term < 0 || fin < 0 || !(req < term && term < fin) {
		t.Fatalf("order cancel_requested=%d process_terminated=%d node_finished=%d", req, term, fin)
	}
	if p := result.Events[term].Payload.(events.ProcessTerminatedPayload); p.Method != "forced" || p.GraceMS != 1000 || p.ElapsedMS != 2000 {
		t.Fatalf("payload = %+v", p)
	}
}

// An executor that ignores a run deadline is abandoned, but the node is
// cancelled with the run's reason, not failed as a shutdown, so the run stays
// cancelled (ADR-0028).
func TestAbandonedExecutorUnderRunTimeoutIsCancelledNotFailed(t *testing.T) {
	shortTimeouts(t, 30*time.Millisecond)
	release := make(chan struct{})
	def := definition(map[string]graph.NodeDefinition{"stuck": {Type: "shell"}})
	def.Limits = &graph.LimitsDefinition{RunTimeoutSeconds: intp(1)}
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		<-release
		return nil, nil
	})
	done := make(chan run.Result, 1)
	go func() { r, _ := Run(context.Background(), def, nil, exec, timeoutOptions()); done <- r }()
	var result run.Result
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("scheduler held by an executor that ignores the run deadline")
	}
	close(release)
	n := nodeResult(t, result, "stuck")
	if n.State != run.NodeCancelled || n.Failure != nil || n.Reason != run.CancelRunTimeout {
		t.Fatalf("node = %+v", n)
	}
	if result.State != run.RunCancelled || result.Reason != run.CancelRunTimeout {
		t.Fatalf("run = %v reason %q", result.State, result.Reason)
	}
}

// An abandoned executor is never started a second time for the same node: the
// node fails as shutdown instead of retrying beside the still-running call.
func TestAbandonedExecutorIsNotRestarted(t *testing.T) {
	shortTimeouts(t, 30*time.Millisecond)
	release := make(chan struct{})
	var calls atomic.Int32
	def := definition(map[string]graph.NodeDefinition{"stuck": {Type: "shell", TimeoutSeconds: intp(1)}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		calls.Add(1)
		<-release
		return nil, nil
	})
	result, _ := Run(context.Background(), def, nil, exec, timeoutOptions())
	close(release)
	if n := nodeResult(t, result, "stuck"); n.Failure == nil || n.Failure.Kind != run.FailureShutdown {
		t.Fatalf("node = %+v", n)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("executor started %d times", got)
	}
}
