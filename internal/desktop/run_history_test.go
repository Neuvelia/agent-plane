package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/history"
	"agentplane/internal/runtime"
)

type launchWriterFunc func(context.Context, history.LaunchRecord) error

func (f launchWriterFunc) Launch(ctx context.Context, record history.LaunchRecord) error {
	return f(ctx, record)
}

func preparedForHistory() PreparedRun {
	prepared := testPrepared()
	prepared.ProjectID = "55555555-5555-4555-8555-555555555555"
	prepared.WorkspaceID = runRequest().WorkspaceID
	prepared.DocumentID = runRequest().DocumentID
	prepared.Revision = runRequest().Revision
	return prepared
}

func TestRunServiceCommitsHistoryMarkerBeforeReady(t *testing.T) {
	marked := make(chan struct{})
	recorded := make(chan history.LaunchRecord, 1)
	writer := launchWriterFunc(func(_ context.Context, record history.LaunchRecord) error {
		recorded <- record
		close(marked)
		return nil
	})
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		return preparedForHistory(), nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		if options.BeforeDispatch == nil {
			return runtime.RunResult{}, errors.New("missing launch hook")
		}
		if err := options.BeforeDispatch("run-1"); err != nil {
			return runtime.RunResult{}, err
		}
		select {
		case <-marked:
		default:
			return runtime.RunResult{}, errors.New("marker not committed")
		}
		options.OnRunReady("run-1", fixedTrace{events.TracePage{Watermark: 1}})
		return runtime.RunResult{RunID: "run-1", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	}, writer, nil)
	defer service.Close()
	started, serviceErr := service.Start(context.Background(), runRequest())
	if serviceErr != nil || started.RunID != "run-1" {
		t.Fatalf("start=%+v err=%v", started, serviceErr)
	}
	// The writer received only canonical safe projections and frozen metadata.
	if writerRecord := <-recorded; writerRecord.ProjectID != "55555555-5555-4555-8555-555555555555" || writerRecord.WorkspaceID != runRequest().WorkspaceID || writerRecord.DocumentID != runRequest().DocumentID || writerRecord.GraphHash != "graph-version" || len(writerRecord.Graph.Bytes()) == 0 || len(writerRecord.Launch.Bytes()) == 0 || len(writerRecord.Completeness.Bytes()) == 0 {
		t.Fatalf("invalid launch marker: %+v", writerRecord)
	}
	if _, serviceErr := service.Cancel(context.Background(), "run-1"); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	if snapshot, serviceErr := service.Snapshot("run-1"); serviceErr != nil || snapshot.HistoryAvailability != HistoryUnavailable {
		t.Fatalf("unarchived snapshot=%+v err=%v", snapshot, serviceErr)
	}
	if retry, serviceErr := service.Start(context.Background(), runRequest()); serviceErr != nil || retry.RunID != "run-1" || retry.HistoryAvailability != HistoryUnavailable {
		t.Fatalf("accepted retry=%+v err=%v", retry, serviceErr)
	}
	other := runRequest()
	other.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if _, serviceErr := service.Start(context.Background(), other); serviceErr == nil || serviceErr.Code != "history_unavailable" {
		t.Fatalf("unarchived run allowed another start: %v", serviceErr)
	}
}

func TestRunServiceHistoryFailureRejectsStartWithoutIdentityAndGatesNewKeys(t *testing.T) {
	var prepares, ready atomic.Int32
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepares.Add(1)
		return preparedForHistory(), nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		if err := options.BeforeDispatch("run-private"); err != nil {
			return runtime.RunResult{}, err
		}
		ready.Add(1)
		options.OnRunReady("run-private", fixedTrace{})
		return runtime.RunResult{}, nil
	}, launchWriterFunc(func(context.Context, history.LaunchRecord) error { return errors.New("disk failure PRIVATE") }), nil)
	defer service.Close()
	started, serviceErr := service.Start(context.Background(), runRequest())
	if serviceErr == nil || serviceErr.Code != "history_unavailable" || started.RunID != "" || ready.Load() != 0 {
		t.Fatalf("start=%+v err=%v ready=%d", started, serviceErr, ready.Load())
	}
	other := runRequest()
	other.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if _, serviceErr := service.Start(context.Background(), other); serviceErr == nil || serviceErr.Code != "history_unavailable" || prepares.Load() != 1 {
		t.Fatalf("later start err=%v prepares=%d", serviceErr, prepares.Load())
	}
	if _, serviceErr := service.Snapshot("run-private"); serviceErr == nil || serviceErr.Code != "not_found" {
		t.Fatalf("phantom identity: %v", serviceErr)
	}
}

func TestRunServiceOversizedSnapshotDoesNotPoisonHealthyHistory(t *testing.T) {
	var writes, prepares atomic.Int32
	writer := launchWriterFunc(func(context.Context, history.LaunchRecord) error {
		if writes.Add(1) == 1 {
			return history.ErrPageTooLarge
		}
		return nil
	})
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepares.Add(1)
		return preparedForHistory(), nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		if err := options.BeforeDispatch("run-1"); err != nil {
			return runtime.RunResult{}, err
		}
		options.OnRunReady("run-1", fixedTrace{events.TracePage{Watermark: 1}})
		return runtime.RunResult{RunID: "run-1", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	}, writer, nil)
	defer service.Close()
	if started, serviceErr := service.Start(context.Background(), runRequest()); serviceErr == nil || serviceErr.Code != "conflict" || started.RunID != "" {
		t.Fatalf("oversized start=%+v err=%v", started, serviceErr)
	}
	other := runRequest()
	other.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if started, serviceErr := service.Start(context.Background(), other); serviceErr != nil || started.RunID != "run-1" || prepares.Load() != 2 || writes.Load() != 2 {
		t.Fatalf("later start=%+v err=%v prepares=%d writes=%d", started, serviceErr, prepares.Load(), writes.Load())
	}
}

func TestRunServiceRequiresHistoryStore(t *testing.T) {
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		t.Fatal("preparer called without history store")
		return PreparedRun{}, nil
	}, nil, nil, nil)
	defer service.Close()
	if _, serviceErr := service.Start(context.Background(), runRequest()); serviceErr == nil || serviceErr.Code != "history_unavailable" {
		t.Fatalf("missing store err=%v", serviceErr)
	}
	var typedNilStore *history.Store
	typedNil := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		t.Fatal("preparer called with typed nil history store")
		return PreparedRun{}, nil
	}, nil, typedNilStore, nil)
	defer typedNil.Close()
	if _, serviceErr := typedNil.Start(context.Background(), runRequest()); serviceErr == nil || serviceErr.Code != "history_unavailable" {
		t.Fatalf("typed nil store err=%v", serviceErr)
	}
	newer := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		t.Fatal("preparer called with unsupported history schema")
		return PreparedRun{}, nil
	}, nil, nil, history.ErrNewerSchema)
	defer newer.Close()
	if _, serviceErr := newer.Start(context.Background(), runRequest()); serviceErr == nil || serviceErr.Code != "history_version_unsupported" || !strings.Contains(serviceErr.Message, "update AgentPlane") {
		t.Fatalf("newer schema err=%v", serviceErr)
	}
}

func TestRunServiceLaunchMarkerSurvivesStoreRestartWithoutPrivateValue(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) {
		prepared := preparedForHistory()
		prepared.Definition.Nodes = map[string]graph.NodeDefinition{"one": {Type: "shell", Config: graph.NodeConfig{"command": "private"}}}
		prepared.Inputs = map[string]graph.Value{"message": graph.NewStringValue("private")}
		prepared.Options.LogRedactions = []string{"private"}
		return prepared, nil
	}, func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, _ runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
		if err := options.BeforeDispatch("run-1"); err != nil {
			return runtime.RunResult{}, err
		}
		options.OnRunReady("run-1", fixedTrace{events.TracePage{Watermark: 1}})
		return runtime.RunResult{RunID: "run-1", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded}, nil
	}, store, nil)
	request := runRequest()
	request.Inputs = map[string]graph.Value{"message": graph.NewStringValue("private")}
	started, serviceErr := service.Start(context.Background(), request)
	if serviceErr != nil || started.RunID != "run-1" {
		t.Fatalf("start=%+v err=%v", started, serviceErr)
	}
	service.Close()
	before, err := store.Get(context.Background(), "run-1")
	if err != nil || before.StorageState != history.Active {
		t.Fatalf("active marker=%+v err=%v", before, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(filepath.Join(directory, "history.db") + suffix)
		if err != nil && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private") {
			t.Fatalf("private value persisted in %s", suffix)
		}
	}
	reopened, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Get(context.Background(), "run-1")
	if err != nil || after.StorageState != history.Interrupted {
		t.Fatalf("restart marker=%+v err=%v", after, err)
	}
}
