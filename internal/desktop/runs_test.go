package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
)

type fixedTrace struct{ page events.TracePage }

func (f fixedTrace) Page(uint64, int) (events.TracePage, error)             { return f.page, nil }
func (f fixedTrace) PageBounded(uint64, int, int) (events.TracePage, error) { return f.page, nil }

func runRequest() RunStartRequest {
	return RunStartRequest{IdempotencyKey: "11111111-1111-4111-8111-111111111111",
		PreflightToken: strings.Repeat("a", 64), PayloadDigest: "sha256:" + sixtyFourZeroes,
		DocumentID: "33333333-3333-4333-8333-333333333333", Revision: 1,
		WorkspaceID: "44444444-4444-4444-8444-444444444444",
		Inputs:      map[string]graph.Value{}, MaxConcurrency: 1}
}

const sixtyFourZeroes = "0000000000000000000000000000000000000000000000000000000000000000"

func testPrepared() PreparedRun {
	return PreparedRun{Definition: &graph.Definition{ID: "graph"}, Inputs: map[string]graph.Value{},
		Executor: inertRunExecutor{}, Options: runtime.Options{MaxConcurrency: 1}, GraphVersion: "graph-version"}
}

type inertRunExecutor struct{}

func (inertRunExecutor) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	return nil, errors.New("must not dispatch")
}

func TestRunOwnerIdempotencyBusyAndTokenIndependentRetry(t *testing.T) {
	var prepares, executions atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepares.Add(1)
		return testPrepared(), nil
	}, func(ctx context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		executions.Add(1)
		options.OnRunReady("run-1", fixedTrace{events.TracePage{Watermark: 7}})
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return runtime.RunResult{RunID: "run-1", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	})
	defer service.Close()
	request := runRequest()
	first, err := service.Start(context.Background(), request)
	if err != nil || first.RunID != "run-1" || first.State != runtime.RunRunning || first.TraceWatermark != 7 {
		t.Fatalf("first start = %#v, %v", first, err)
	}
	<-started
	retry := request
	retry.PreflightToken = strings.Repeat("b", 64)
	second, err := service.Start(context.Background(), retry)
	if err != nil || second.RunID != first.RunID || prepares.Load() != 1 || executions.Load() != 1 {
		t.Fatalf("idempotent retry = %#v, %v, prepared %d, executed %d", second, err, prepares.Load(), executions.Load())
	}
	changed := request
	changed.MaxConcurrency = 2
	if _, err := service.Start(context.Background(), changed); err == nil || err.Code != "conflict" {
		t.Fatalf("changed same key = %v", err)
	}
	other := request
	other.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if _, err := service.Start(context.Background(), other); err == nil || err.Code != "busy" {
		t.Fatalf("different active key = %v", err)
	}
	close(release)
	cancelled, err := service.Cancel(context.Background(), "run-1")
	if err != nil || cancelled.State != runtime.RunSucceeded || cancelled.RunID != "run-1" {
		t.Fatalf("terminal = %#v, %v", cancelled, err)
	}
}

func TestRunOwnerFreezesReplayRedactionsBeforePublishingTrace(t *testing.T) {
	preparedValues := []string{"private"}
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepared := testPrepared()
		prepared.Options.LogRedactions = preparedValues
		return prepared, nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		collector := events.NewCollector("run-safe", "graph", "graph-version")
		if _, err := collector.Append(events.Event{Type: events.LogChunk, Payload: events.LogChunkPayload{Stream: events.Stdout, Data: []byte("before private after")}}); err != nil {
			return runtime.RunResult{}, err
		}
		options.OnRunReady("run-safe", collector)
		return runtime.RunResult{RunID: "run-safe", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	})
	defer service.Close()
	result, serviceErr := service.Start(context.Background(), runRequest())
	if serviceErr != nil || result.RunID != "run-safe" {
		t.Fatalf("start = %+v, %v", result, serviceErr)
	}
	preparedValues[0] = "changed"
	page, serviceErr := service.Events("run-safe", 0, 1)
	if serviceErr != nil || len(page.Events) != 1 || strings.Contains(string(page.Events[0]), "private") {
		t.Fatalf("replay did not retain frozen redactions: %+v, %v", page, serviceErr)
	}
	if got := replayEvent(t, page.Events[0]).Payload.(events.LogChunkPayload); string(got.Data) != "before *** after" {
		t.Fatalf("replay redaction = %q", got.Data)
	}
}

func TestRunOwnerPreReadyFailureHasNoPhantomIdentity(t *testing.T) {
	var calls atomic.Int32
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		calls.Add(1)
		return testPrepared(), nil
	}, func(context.Context, *graph.Definition, map[string]graph.Value, runtime.Executor, runtime.Options) (runtime.RunResult, error) {
		return runtime.RunResult{}, errors.New("runtime preflight failed with SECRET")
	})
	defer service.Close()
	if got, err := service.Start(context.Background(), runRequest()); err == nil || err.Code != "unavailable" || got.RunID != "" || calls.Load() != 1 {
		t.Fatalf("pre-ready failure = %#v, %v, calls %d", got, err, calls.Load())
	}
	if _, err := service.Snapshot("invented"); err == nil || err.Code != "not_found" {
		t.Fatalf("phantom snapshot = %v", err)
	}
	// No accepted key or active slot remains after a failed runtime preflight.
	if _, err := service.Start(context.Background(), runRequest()); err == nil || err.Code != "unavailable" || calls.Load() != 2 {
		t.Fatalf("retry after failed launch = %v, calls %d", err, calls.Load())
	}
}

func TestRunOwnerAlreadyCancelledRequestDoesNotReserve(t *testing.T) {
	var prepares atomic.Int32
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepares.Add(1)
		return testPrepared(), nil
	}, nil)
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := service.Start(ctx, runRequest()); err == nil || err.Code != "unavailable" || got.RunID != "" || prepares.Load() != 0 {
		t.Fatalf("cancelled Start = %#v, %v, prepares %d", got, err, prepares.Load())
	}
	service.mu.Lock()
	active := service.active
	service.mu.Unlock()
	if active != nil {
		t.Fatal("cancelled request reserved the run slot")
	}
}

func TestRunOwnerCloseCancelsAndJoins(t *testing.T) {
	stopped := make(chan struct{})
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		return testPrepared(), nil
	}, func(ctx context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		options.OnRunReady("run-close", fixedTrace{events.TracePage{Watermark: 1}})
		<-ctx.Done()
		close(stopped)
		return runtime.RunResult{RunID: "run-close", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunCancelled}, nil
	})
	if _, err := service.Start(context.Background(), runRequest()); err != nil {
		t.Fatal(err)
	}
	service.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("Close returned before owned runner stopped")
	}
	if _, err := service.Start(context.Background(), runRequest()); err == nil || err.Code != "unavailable" {
		t.Fatalf("start after close = %v", err)
	}
}

func TestRunOwnerConcurrentSameKeyJoinsPendingPreparation(t *testing.T) {
	preparing := make(chan struct{})
	releasePreparation := make(chan struct{})
	runRelease := make(chan struct{})
	var prepares atomic.Int32
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepares.Add(1)
		close(preparing)
		<-releasePreparation
		return testPrepared(), nil
	}, func(ctx context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		options.OnRunReady("joined-run", fixedTrace{events.TracePage{Watermark: 3}})
		select {
		case <-runRelease:
		case <-ctx.Done():
		}
		return runtime.RunResult{RunID: "joined-run", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	})
	defer service.Close()
	type outcome struct {
		result RunStartResult
		err    *ServiceError
	}
	first := make(chan outcome, 1)
	go func() {
		result, err := service.Start(context.Background(), runRequest())
		first <- outcome{result, err}
	}()
	<-preparing
	other := runRequest()
	other.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if _, err := service.Start(context.Background(), other); err == nil || err.Code != "busy" {
		t.Fatalf("different key during preparation = %v", err)
	}
	second := make(chan outcome, 1)
	go func() {
		request := runRequest()
		request.PreflightToken = strings.Repeat("b", 64)
		result, err := service.Start(context.Background(), request)
		second <- outcome{result, err}
	}()
	close(releasePreparation)
	a, b := <-first, <-second
	if a.err != nil || b.err != nil || a.result.RunID != "joined-run" || b.result.RunID != a.result.RunID || prepares.Load() != 1 {
		t.Fatalf("joined start = %#v / %#v, prepares %d", a, b, prepares.Load())
	}
	close(runRelease)
	if _, err := service.Cancel(context.Background(), "joined-run"); err != nil {
		t.Fatal(err)
	}
}

func TestRunOwnerSnapshotWithholdsRawFailureAndOlderTrace(t *testing.T) {
	var sequence atomic.Int32
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		return testPrepared(), nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		n := sequence.Add(1)
		id := events.RunID("run-" + string(rune('0'+n)))
		options.OnRunReady(id, fixedTrace{events.TracePage{Watermark: uint64(n), Stats: events.DeliveryStats{RetainedOmittedEvents: 2}}})
		return runtime.RunResult{RunID: id, GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunFailed,
			Failure: &runtime.Failure{Kind: runtime.FailureExecutor, Err: errors.New("SECRET failure detail")},
			Nodes: []runtime.NodeResult{{NodeID: "step", NodeRunID: "node-1", State: runtime.NodeFailed,
				Failure: &runtime.Failure{Kind: runtime.FailureExecutor, Err: errors.New("SECRET child output")}}}}, nil
	})
	defer service.Close()
	first, err := service.Start(context.Background(), runRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(context.Background(), first.RunID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Snapshot(first.RunID)
	if err != nil || !snapshot.Finished || snapshot.FailureKind != runtime.FailureExecutor ||
		len(snapshot.Nodes) != 1 || snapshot.DeliveryStats.RetainedOmittedEvents != 2 {
		t.Fatalf("safe final snapshot = %#v, %v", snapshot, err)
	}
	encoded, marshalErr := json.Marshal(snapshot)
	if marshalErr != nil || strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "Err") {
		t.Fatalf("raw failure escaped = %q, %v", encoded, marshalErr)
	}
	next := runRequest()
	next.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	second, err := service.Start(context.Background(), next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(context.Background(), second.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Snapshot(first.RunID); err == nil || err.Code != "unavailable" {
		t.Fatalf("older trace should be evicted: %v", err)
	}
	if got, err := service.Cancel(context.Background(), first.RunID); err != nil || got.RunID != first.RunID || got.State != runtime.RunFailed {
		t.Fatalf("cancel of finished evicted run must remain idempotent: %#v, %v", got, err)
	}
	if retry, err := service.Start(context.Background(), runRequest()); err != nil || retry.RunID != first.RunID || retry.State != runtime.RunFailed {
		t.Fatalf("older key tombstone = %#v, %v", retry, err)
	}
}

func TestRunOwnerCapacityRejectsNewKeyWithoutDiscardingIdempotency(t *testing.T) {
	var calls atomic.Int32
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		return testPrepared(), nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		n := calls.Add(1)
		id := events.RunID(fmt.Sprintf("run-%d", n))
		options.OnRunReady(id, fixedTrace{events.TracePage{Watermark: uint64(n)}})
		return runtime.RunResult{RunID: id, GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	})
	defer service.Close()
	firstRequest := runRequest()
	var firstRun events.RunID
	for i := 1; i <= maxOwnedRunKeys; i++ {
		request := runRequest()
		request.IdempotencyKey = fmt.Sprintf("11111111-1111-4111-8111-%012x", i)
		result, err := service.Start(context.Background(), request)
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		if i == 1 {
			firstRequest, firstRun = request, result.RunID
		}
		if _, err := service.Cancel(context.Background(), result.RunID); err != nil {
			t.Fatalf("completion %d: %v", i, err)
		}
	}
	extra := runRequest()
	extra.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if _, err := service.Start(context.Background(), extra); err == nil || err.Code != "unavailable" || calls.Load() != maxOwnedRunKeys {
		t.Fatalf("capacity = %v, calls %d", err, calls.Load())
	}
	if got, err := service.Start(context.Background(), firstRequest); err != nil || got.RunID != firstRun || calls.Load() != maxOwnedRunKeys {
		t.Fatalf("old key after capacity = %#v, %v", got, err)
	}
}

func TestRunOwnerCloseDuringPendingPreparationJoins(t *testing.T) {
	entered := make(chan struct{})
	stopped := make(chan struct{})
	var launched atomic.Bool
	service := NewRunService(context.Background(), func(ctx context.Context, _ RunStartRequest) (PreparedRun, *ServiceError) {
		close(entered)
		<-ctx.Done()
		close(stopped)
		return PreparedRun{}, protocolError("unavailable", "Service is closing")
	}, func(context.Context, *graph.Definition, map[string]graph.Value, runtime.Executor, runtime.Options) (runtime.RunResult, error) {
		launched.Store(true)
		return runtime.RunResult{}, errors.New("runner launched after Close")
	})
	started := make(chan *ServiceError, 1)
	go func() { _, err := service.Start(context.Background(), runRequest()); started <- err }()
	<-entered
	service.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("Close did not join pending preparation")
	}
	if err := <-started; err == nil || err.Code != "unavailable" {
		t.Fatalf("pending start after Close = %v", err)
	}
	if launched.Load() {
		t.Fatal("runner launched after Close")
	}
}

func TestRunOwnerCancelJoinsRealShellDirectChild(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("native Windows process ownership belongs to Task 10")
	}
	marker := filepath.Join(t.TempDir(), "child-ready")
	definition := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "cancel-shell",
		Inputs: map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{"wait": {Type: "shell", Config: graph.NodeConfig{
			"command": "/bin/sh", "args": []string{"-c", `printf ready > "$1"; exec /bin/sleep 3600`, "sh", marker},
		}, Outputs: map[string]graph.PortDefinition{}}},
		Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}}
	version, err := graph.SemanticVersion(definition)
	if err != nil {
		t.Fatal(err)
	}
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		return PreparedRun{Definition: definition, Inputs: map[string]graph.Value{}, Executor: shell.Executor{},
			Options: runtime.Options{MaxConcurrency: 1}, GraphVersion: version}, nil
	}, nil)
	t.Cleanup(service.Close)
	started, serviceErr := service.Start(context.Background(), runRequest())
	if serviceErr != nil || started.RunID == "" || started.State != runtime.RunRunning {
		t.Fatalf("real Shell start = %#v, %v", started, serviceErr)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(marker); err == nil && string(data) == "ready" {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("direct Shell child never reached deterministic marker")
		case <-tick.C:
		}
	}
	// Shell uses exec.CommandContext and Wait. A completed Cancel therefore
	// proves the direct child was killed and reaped, not merely signalled.
	final, serviceErr := service.Cancel(context.Background(), started.RunID)
	if serviceErr != nil || final.RunID != started.RunID || final.State != runtime.RunCancelled {
		t.Fatalf("real Shell cancel = %#v, %v", final, serviceErr)
	}
	reloaded, serviceErr := service.Snapshot(started.RunID)
	if serviceErr != nil || reloaded.State != runtime.RunCancelled || !reloaded.Finished || reloaded.TraceWatermark == 0 ||
		len(reloaded.Nodes) != 1 || reloaded.Nodes[0].State != runtime.NodeCancelled {
		t.Fatalf("final Shell snapshot = %#v, %v", reloaded, serviceErr)
	}
}
