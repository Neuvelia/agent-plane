package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/history"
	"agentplane/internal/runtime"
)

func TestCollectFinalTraceUsesCanonicalReplayBytesAndExactOmissions(t *testing.T) {
	service := NewRunService(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepared := testPrepared()
		prepared.Options.LogRedactions = []string{"private"}
		return prepared, nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		collector := events.NewCollector("run-1", "graph", "graph-version")
		if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
			return runtime.RunResult{}, err
		}
		options.OnRunReady("run-1", collector)
		if _, err := collector.OmitSensitive(); err != nil {
			return runtime.RunResult{}, err
		}
		if _, err := collector.Append(events.Event{Type: events.LogChunk, Payload: events.LogChunkPayload{Stream: events.Stdout, Data: []byte("private")}}); err != nil {
			return runtime.RunResult{}, err
		}
		if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}}); err != nil {
			return runtime.RunResult{}, err
		}
		return runtime.RunResult{RunID: "run-1", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	})
	defer service.Close()
	if _, serviceErr := service.Start(context.Background(), runRequest()); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	if _, serviceErr := service.Cancel(context.Background(), "run-1"); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	final, err := service.collectFinalTrace("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if final.Watermark != 4 || len(final.Events) != 3 || len(final.Omissions) != 1 || final.Omissions[0].From != 2 || final.Omissions[0].To != 2 || final.Complete {
		t.Fatalf("unexpected trace: %+v", final)
	}
	page, serviceErr := service.Events("run-1", 0, 8)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	for index, item := range final.Events {
		if !bytes.Equal(item.Projection.Bytes(), page.Events[index]) || bytes.Contains(item.Projection.Bytes(), []byte("private")) {
			t.Fatalf("stored bytes differ from live replay at %d", index)
		}
	}
}

func TestHistoryOutcomeStatusesRejectPrivateValueCollisions(t *testing.T) {
	if !historyOutcomeStatusesSafe([]string{"private"}) {
		t.Fatal("unrelated private value blocked outcome statuses")
	}
	for _, value := range []string{"accepted", "failed", "execution_error"} {
		if historyOutcomeStatusesSafe([]string{value}) {
			t.Fatalf("private value %q makes a required status unrepresentable", value)
		}
	}
}

func TestRunServiceArchivesFinalTraceAndAllowsNextStart(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var ordinal atomic.Int32
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		return preparedForHistory(), nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		id := events.RunID(fmt.Sprintf("run-%d", ordinal.Add(1)))
		if err := options.BeforeDispatch(id); err != nil {
			return runtime.RunResult{}, err
		}
		collector := events.NewCollector(id, "graph", "graph-version")
		if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
			return runtime.RunResult{}, err
		}
		options.OnRunReady(id, collector)
		if _, err := collector.OmitSensitive(); err != nil {
			return runtime.RunResult{}, err
		}
		if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}}); err != nil {
			return runtime.RunResult{}, err
		}
		return runtime.RunResult{RunID: id, GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	}, store, nil)
	defer service.Close()
	first, serviceErr := service.Start(context.Background(), runRequest())
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	if _, serviceErr := service.Cancel(context.Background(), first.RunID); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	snapshot, serviceErr := service.Snapshot(first.RunID)
	if serviceErr != nil || !snapshot.Finished || snapshot.HistoryAvailability != HistoryArchived || snapshot.TraceWatermark != 3 {
		t.Fatalf("snapshot=%+v error=%v", snapshot, serviceErr)
	}
	stored, err := store.Get(context.Background(), string(first.RunID))
	if err != nil || stored.StorageState != history.Complete || stored.RuntimeState != "succeeded" || stored.Result == nil {
		t.Fatalf("stored=%+v error=%v", stored, err)
	}
	page, err := store.Events(context.Background(), string(first.RunID), 0, 10)
	if err != nil || page.Watermark != 3 || len(page.Events) != 2 || len(page.Omissions) != 1 || page.Omissions[0].From != 2 {
		t.Fatalf("page=%+v error=%v", page, err)
	}
	retry, serviceErr := service.Start(context.Background(), runRequest())
	if serviceErr != nil || retry.RunID != first.RunID || retry.HistoryAvailability != HistoryArchived || ordinal.Load() != 1 {
		t.Fatalf("retry=%+v error=%v", retry, serviceErr)
	}
	next := runRequest()
	next.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	second, serviceErr := service.Start(context.Background(), next)
	if serviceErr != nil || second.RunID == first.RunID {
		t.Fatalf("second=%+v error=%v", second, serviceErr)
	}
	if _, serviceErr := service.Cancel(context.Background(), second.RunID); serviceErr != nil {
		t.Fatal(serviceErr)
	}
}

type failingFinalizer struct{ *history.Store }

func (f failingFinalizer) Finalize(context.Context, history.Finalization) error {
	return errors.New("disk failure PRIVATE")
}

func TestRunServiceFinalizeFailureKeepsMarkerAndBlocksNextStart(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) { return preparedForHistory(), nil },
		func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
			if err := options.BeforeDispatch("run-1"); err != nil {
				return runtime.RunResult{}, err
			}
			collector := events.NewCollector("run-1", "graph", "graph-version")
			if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
				return runtime.RunResult{}, err
			}
			options.OnRunReady("run-1", collector)
			if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}}); err != nil {
				return runtime.RunResult{}, err
			}
			return runtime.RunResult{RunID: "run-1", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
		}, failingFinalizer{store}, nil)
	defer service.Close()
	if _, serviceErr := service.Start(context.Background(), runRequest()); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	if _, serviceErr := service.Cancel(context.Background(), "run-1"); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	snapshot, serviceErr := service.Snapshot("run-1")
	if serviceErr != nil || snapshot.HistoryAvailability != HistoryUnavailable || snapshot.State != runtime.RunSucceeded {
		t.Fatalf("snapshot=%+v error=%v", snapshot, serviceErr)
	}
	stored, err := store.Get(context.Background(), "run-1")
	if err != nil || stored.StorageState != history.Active {
		t.Fatalf("marker=%+v error=%v", stored, err)
	}
	next := runRequest()
	next.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if _, serviceErr := service.Start(context.Background(), next); serviceErr == nil || serviceErr.Code != "history_unavailable" {
		t.Fatalf("next start=%v", serviceErr)
	}
}

func TestRunServiceRejectsMismatchedTerminalGraphIdentity(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) { return preparedForHistory(), nil },
		func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
			if err := options.BeforeDispatch("run-1"); err != nil {
				return runtime.RunResult{}, err
			}
			collector := events.NewCollector("run-1", "graph", "graph-version")
			if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
				return runtime.RunResult{}, err
			}
			options.OnRunReady("run-1", collector)
			if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}}); err != nil {
				return runtime.RunResult{}, err
			}
			return runtime.RunResult{RunID: "run-1", GraphID: "other-graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
		}, store, nil)
	defer service.Close()
	if _, serviceErr := service.Start(context.Background(), runRequest()); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	if _, serviceErr := service.Cancel(context.Background(), "run-1"); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	snapshot, serviceErr := service.Snapshot("run-1")
	if serviceErr != nil || snapshot.HistoryAvailability != HistoryUnavailable || snapshot.State != "unknown" {
		t.Fatalf("snapshot=%+v error=%v", snapshot, serviceErr)
	}
	stored, err := store.Get(context.Background(), "run-1")
	if err != nil || stored.StorageState != history.Active {
		t.Fatalf("marker=%+v error=%v", stored, err)
	}
}
