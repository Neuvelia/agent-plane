package desktop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/capture"
	"agentplane/internal/events"
	"agentplane/internal/gitstate"
	"agentplane/internal/graph"
	"agentplane/internal/history"
	"agentplane/internal/runtime"
)

type nodeCaptureAdapter struct{ file string }

func (nodeCaptureAdapter) ID() agent.AdapterID { return "fake" }
func (nodeCaptureAdapter) Detect(context.Context) (agent.CapabilityReport, error) {
	return agent.CapabilityReport{Available: true}, nil
}
func (a nodeCaptureAdapter) Execute(_ context.Context, input agent.AgentExecutionInput) (agent.AgentExecutionOutput, error) {
	if input.AttemptID != "" || input.NodeRunID != "node-one" {
		return agent.AgentExecutionOutput{}, errors.New("one-shot Agent received invalid correlation")
	}
	return agent.AgentExecutionOutput{}, os.WriteFile(a.file, []byte("after"), 0600)
}

func captureStoreFixture(t *testing.T) (*history.Store, string, capture.Resolver, string) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	safe, err := history.ParseSafeJSON([]byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Launch(context.Background(), history.LaunchRecord{RunID: "run-1", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: time.Now().UTC(), Graph: safe, Launch: safe, Completeness: safe}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	identity, err := capture.DirectoryIdentity(workspace)
	if err != nil {
		t.Skipf("native workspace identity fixture unavailable: %v", err)
	}
	resolver := func() (capture.Workspace, error) {
		return capture.Workspace{Path: workspace, Identity: identity}, nil
	}
	return store, workspace, resolver, state
}

func observeTest(controller *captureController, ctx context.Context, node graph.NodeDefinition, ids runtime.ExecutionCorrelation, produce func() (map[string]graph.Value, *events.NodeEvidencePayload, error)) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	return controller.observe(node, runtime.ExecutionObservation{Correlation: ids}, func(observation runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		observation.BeforeProduce(ctx)
		values, evidence, err := produce()
		observation.AfterProduce(ctx)
		return values, evidence, err
	})
}

func TestCaptureControllerPersistsImmutableAttemptDiffBeforeRetry(t *testing.T) {
	store, workspace, resolver, state := captureStoreFixture(t)
	path := filepath.Join(workspace, "work.txt")
	if err := os.WriteFile(path, []byte("before private\n"), 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := newCaptureController(store, resolver, []string{"private"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	node := graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"work.txt"}}}
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: "attempt-1"}
	values, _, produceErr := observeTest(controller, context.Background(), node, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		return nil, nil, os.WriteFile(path, []byte("after private\n"), 0600)
	})
	if produceErr != nil || values != nil {
		t.Fatalf("produce result=%+v err=%v", values, produceErr)
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-1", "attempt-1", "", 10)
	if err != nil || len(page.Descriptors) != 4 || page.Complete {
		t.Fatalf("stored page=%+v err=%v", page, err)
	}
	var pre, post, diff history.EvidenceDescriptor
	for _, item := range page.Descriptors {
		switch item.Kind {
		case "preimage":
			pre = item
		case "postimage":
			post = item
		case "diff":
			diff = item
		}
	}
	if pre.CaptureGroup == "" || pre.CaptureGroup != post.CaptureGroup || pre.CaptureGroup != diff.CaptureGroup || pre.SourcePath != "work.txt" || !pre.Redacted || !post.Redacted || !diff.Redacted {
		t.Fatalf("pairing/redaction: pre=%+v post=%+v diff=%+v", pre, post, diff)
	}
	if err := os.WriteFile(path, []byte("second retry\n"), 0600); err != nil {
		t.Fatal(err)
	}
	chunk, err := store.Evidence(context.Background(), pre.EvidenceID, 0, 1024)
	if err != nil || !bytes.Contains(chunk.Data, []byte("before")) || bytes.Contains(chunk.Data, []byte("private")) {
		t.Fatalf("immutable preimage=%q err=%v", chunk.Data, err)
	}
	chunk, err = store.Evidence(context.Background(), diff.EvidenceID, 0, 1024)
	if err != nil || !bytes.Contains(chunk.Data, []byte("-before")) || !bytes.Contains(chunk.Data, []byte("+after")) || bytes.Contains(chunk.Data, []byte("private")) {
		t.Fatalf("safe diff=%q err=%v", chunk.Data, err)
	}
	for _, name := range []string{"history.db", "history.db-wal", "history.db-shm"} {
		data, readErr := os.ReadFile(filepath.Join(state, name))
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, []byte("private")) {
			t.Fatalf("known value persisted in %s", name)
		}
	}
	entries, err := os.ReadDir(filepath.Join(state, "evidence"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, readErr := os.ReadFile(filepath.Join(state, "evidence", entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, []byte("private")) {
			t.Fatalf("known value persisted in managed evidence %s", entry.Name())
		}
	}
}

type failingEvidenceWriter struct{}

func (failingEvidenceWriter) CaptureEvidence(context.Context, history.EvidenceCapture) error {
	return errors.New("disk failure PRIVATE")
}

type captureFailureStore struct{ *history.Store }

func (c captureFailureStore) CaptureEvidence(context.Context, history.EvidenceCapture) error {
	return errors.New("failed capture write PRIVATE")
}

func TestRunServiceShowsCaptureWriteFailureWhileRunIsActive(t *testing.T) {
	store, workspace, resolver, _ := captureStoreFixture(t)
	_ = workspace
	prepared := preparedForHistory()
	prepared.Definition.Nodes = map[string]graph.NodeDefinition{"work": {Type: "agent", Config: graph.NodeConfig{"workspace": workspace, "allowed_paths": []string{"./"}}}}
	prepared.Executor = desktopNodeExecutor{workspaceResolver: resolver}
	ready := make(chan struct{})
	release := make(chan struct{})
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) { return prepared, nil },
		func(_ context.Context, _ *graph.Definition, _ map[string]graph.Value, executor runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
			if err := options.BeforeDispatch("run-live"); err != nil {
				return runtime.RunResult{}, err
			}
			collector := events.NewCollector("run-live", "graph", "graph-version")
			if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
				return runtime.RunResult{}, err
			}
			options.OnRunReady("run-live", collector)
			desktopExecutor := executor.(desktopNodeExecutor)
			desktopExecutor.capture.record(runtime.ExecutionCorrelation{RunID: "run-live", NodeRunID: "node-1"}, "diff", "", "", "capture_failed", nil)
			close(ready)
			<-release
			if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "failed"}}); err != nil {
				return runtime.RunResult{}, err
			}
			return runtime.RunResult{RunID: "run-live", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunFailed}, nil
		}, captureFailureStore{store}, nil)
	defer service.Close()
	started, serviceErr := service.Start(context.Background(), runRequest())
	if serviceErr != nil || started.RunID != "run-live" {
		t.Fatalf("start=%+v err=%v", started, serviceErr)
	}
	<-ready
	snapshot, serviceErr := service.Snapshot("run-live")
	if serviceErr != nil || snapshot.HistoryAvailability != HistoryUnavailable || snapshot.State != runtime.RunRunning {
		t.Fatalf("active failure snapshot=%+v err=%v", snapshot, serviceErr)
	}
	page, handled, serviceErr := service.ActiveEvidenceDescriptors("run-live", "", "", 10)
	if !handled || serviceErr != nil || page.Complete || len(page.Descriptors) != 1 || page.Descriptors[0].OmissionReason != "capture_failed" {
		t.Fatalf("active evidence=%+v handled=%v err=%v", page, handled, serviceErr)
	}
	chunk, found := service.ActiveEvidenceUnavailable(page.Descriptors[0].EvidenceID)
	if !found || chunk.Availability != "unavailable" || chunk.OmissionReason != "capture_failed" || len(chunk.Data) != 0 || !chunk.End {
		t.Fatalf("active failed evidence read=%+v found=%v", chunk, found)
	}
	if _, found := service.ActiveEvidenceUnavailable("other-evidence"); found {
		t.Fatal("unrelated evidence intercepted")
	}
	close(release)
	if _, serviceErr := service.Cancel(context.Background(), "run-live"); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	stored, err := store.Get(context.Background(), "run-live")
	if err != nil || stored.StorageState != history.Active {
		t.Fatalf("unarchived marker=%+v err=%v", stored, err)
	}
	next := runRequest()
	next.IdempotencyKey = "22222222-2222-4222-8222-222222222222"
	if _, serviceErr := service.Start(context.Background(), next); serviceErr == nil || serviceErr.Code != "history_unavailable" {
		t.Fatalf("next Start after one-shot capture failure=%v", serviceErr)
	}
}

func TestOneShotAgentArchivesNodeScopedEvidenceWithoutAttempt(t *testing.T) {
	store, workspace, resolver, _ := captureStoreFixture(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	gitPath, err = filepath.Abs(gitPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		command := exec.Command(gitPath, args...)
		command.Dir = workspace
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("Git fixture: %v: %s", err, output)
		}
	}
	file := filepath.Join(workspace, "work.txt")
	if err := os.WriteFile(file, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	prepared := preparedForHistory()
	prepared.Definition.Nodes = map[string]graph.NodeDefinition{"one": {Type: "agent", Config: graph.NodeConfig{"adapter": "fake", "task": "edit", "workspace": workspace, "allowed_paths": []string{"work.txt"}}}}
	prepared.Executor = desktopNodeExecutor{workspaceResolver: resolver, agents: agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"fake": nodeCaptureAdapter{file: file}}}}
	prepared.Options.GitExecutable = gitPath
	produced := make(chan struct{})
	service := NewRunServiceWithHistory(context.Background(), func(context.Context, RunStartRequest) (PreparedRun, *ServiceError) { return prepared, nil },
		func(ctx context.Context, _ *graph.Definition, _ map[string]graph.Value, executor runtime.Executor, options runtime.Options) (runtime.RunResult, error) {
			defer close(produced)
			if err := options.BeforeDispatch("run-one-shot"); err != nil {
				return runtime.RunResult{}, err
			}
			collector := events.NewCollector("run-one-shot", "graph", "graph-version")
			if _, err := collector.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}}); err != nil {
				return runtime.RunResult{}, err
			}
			options.OnRunReady("run-one-shot", collector)
			gitCtx, err := gitstate.WithExecutable(ctx, options.GitExecutable)
			if err != nil {
				return runtime.RunResult{}, err
			}
			owned := executor.(desktopNodeExecutor)
			_, _, err = owned.ExecuteNodeObserved(gitCtx, "one", prepared.Definition.Nodes["one"], nil, runtime.ExecutionObservation{Correlation: runtime.ExecutionCorrelation{RunID: "run-one-shot", NodeRunID: "node-one"}})
			if err != nil {
				return runtime.RunResult{}, err
			}
			if _, err := collector.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}}); err != nil {
				return runtime.RunResult{}, err
			}
			return runtime.RunResult{RunID: "run-one-shot", GraphID: "graph", GraphVersion: "graph-version", State: runtime.RunSucceeded,
				Nodes: []runtime.NodeResult{{NodeID: "one", NodeRunID: "node-one", State: runtime.NodeSucceeded}}}, nil
		}, store, nil)
	defer service.Close()
	if _, serviceErr := service.Start(context.Background(), runRequest()); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	<-produced
	if _, serviceErr := service.Cancel(context.Background(), "run-one-shot"); serviceErr != nil {
		t.Fatal(serviceErr)
	}
	snapshot, serviceErr := service.Snapshot("run-one-shot")
	if serviceErr != nil || snapshot.HistoryAvailability != HistoryArchived {
		t.Fatalf("one-shot archive=%+v err=%v", snapshot, serviceErr)
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-one-shot", "", "", 10)
	if err != nil || len(page.Descriptors) != 4 || !page.Complete {
		t.Fatalf("node evidence=%+v err=%v", page, err)
	}
	for _, item := range page.Descriptors {
		if item.AttemptID != "" || item.NodeRunID != "node-one" || item.Availability != "available" {
			t.Fatalf("invented attempt or unavailable node evidence: %+v", item)
		}
	}
}

func TestCaptureControllerWriteFailureVisibleBeforeReturn(t *testing.T) {
	workspace := t.TempDir()
	identity, err := capture.DirectoryIdentity(workspace)
	if err != nil {
		t.Skipf("native workspace identity fixture unavailable: %v", err)
	}
	file := filepath.Join(workspace, "work.txt")
	if err := os.WriteFile(file, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	resolver := func() (capture.Workspace, error) {
		return capture.Workspace{Path: workspace, Identity: identity}, nil
	}
	var unavailable atomic.Bool
	controller, err := newCaptureController(failingEvidenceWriter{}, resolver, nil, func() { unavailable.Store(true) })
	if err != nil {
		t.Fatal(err)
	}
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: "attempt-1"}
	_, _, produceErr := observeTest(controller, context.Background(), graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"work.txt"}}}, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		return nil, nil, os.WriteFile(file, []byte("after"), 0600)
	})
	if produceErr != nil || !unavailable.Load() || !controller.failedWrite() {
		t.Fatalf("capture write failure hidden: %v %v", produceErr, unavailable.Load())
	}
	page, serviceErr := controller.page("run-1", "attempt-1", "", 10)
	if serviceErr != nil || page.Complete || len(page.Descriptors) != 1 || page.Descriptors[0].Availability != "unavailable" || page.Descriptors[0].OmissionReason != "capture_failed" {
		t.Fatalf("active failure page=%+v err=%v", page, serviceErr)
	}
	if strings.Contains(page.Descriptors[0].SourcePath, "PRIVATE") {
		t.Fatal("raw storage error leaked")
	}
}

func TestCaptureControllerPostimageAfterCancellation(t *testing.T) {
	store, workspace, resolver, _ := captureStoreFixture(t)
	path := filepath.Join(workspace, "work.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := newCaptureController(store, resolver, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1"}
	_, _, produceErr := observeTest(controller, ctx, graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"work.txt"}}}, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
			return nil, nil, err
		}
		cancel()
		return nil, nil, context.Canceled
	})
	if !errors.Is(produceErr, context.Canceled) {
		t.Fatalf("producer error=%v", produceErr)
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-1", "", "", 10)
	if err != nil || len(page.Descriptors) < 3 {
		t.Fatalf("post-cancel descriptors=%+v err=%v", page, err)
	}
	foundPost := false
	foundGitUnavailable := false
	for _, item := range page.Descriptors {
		if item.Kind == "postimage" && item.Availability == "available" {
			foundPost = true
		}
		if item.Kind == "git_observation" && item.OmissionReason == "diff_unavailable" {
			foundGitUnavailable = true
		}
	}
	if !foundPost || !foundGitUnavailable {
		t.Fatalf("postimage or Git unavailable marker missing after cancel: %+v", page.Descriptors)
	}
}

func TestCaptureControllerUnsafeChildCannotYieldEmptyDiff(t *testing.T) {
	store, workspace, resolver, _ := captureStoreFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "bad\\name"), []byte("hidden"), 0600); err != nil {
		t.Skipf("filesystem cannot create unsafe fixture: %v", err)
	}
	controller, err := newCaptureController(store, resolver, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: "attempt-1"}
	_, _, produceErr := observeTest(controller, context.Background(), graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"./"}}}, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		return nil, nil, nil
	})
	if produceErr != nil {
		t.Fatal(produceErr)
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-1", "attempt-1", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	unsafe := false
	for _, item := range page.Descriptors {
		if item.Kind == "diff" && item.OmissionReason == "unsafe_path" {
			unsafe = true
		}
		if item.Kind == "diff" && item.Availability == "available" {
			t.Fatalf("false successful diff: %+v", item)
		}
	}
	if !unsafe {
		t.Fatalf("unsafe path unavailable marker missing: %+v", page.Descriptors)
	}
}

func TestNodeScopedOversizePreimageRemainsUnavailable(t *testing.T) {
	store, workspace, resolver, _ := captureStoreFixture(t)
	path := filepath.Join(workspace, "large.txt")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), capture.MaxFileBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := newCaptureController(store, resolver, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1"}
	_, _, produceErr := observeTest(controller, context.Background(), graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"large.txt"}}}, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		return nil, nil, os.WriteFile(path, []byte("small"), 0600)
	})
	if produceErr != nil {
		t.Fatal(produceErr)
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-1", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var preLimited, postAvailable, diffUnavailable bool
	for _, item := range page.Descriptors {
		if item.AttemptID != "" || item.NodeRunID != "node-1" {
			t.Fatalf("wrong node identity: %+v", item)
		}
		preLimited = preLimited || item.Kind == "preimage" && item.OmissionReason == "size_limit"
		postAvailable = postAvailable || item.Kind == "postimage" && item.Availability == "available"
		diffUnavailable = diffUnavailable || item.Kind == "diff" && item.OmissionReason == "diff_unavailable"
	}
	if !preLimited || !postAvailable || !diffUnavailable {
		t.Fatalf("oversize node image was treated as empty: %+v", page.Descriptors)
	}
}

func TestCaptureControllerCountsPreAndPostImagesOnceWithinRunBudget(t *testing.T) {
	store, workspace, resolver, _ := captureStoreFixture(t)
	content := bytes.Repeat([]byte{0, 1, 2}, 3<<20) // 9 MiB binary per file
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		if err := os.WriteFile(filepath.Join(workspace, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	controller, err := newCaptureController(store, resolver, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: "attempt-1"}
	_, _, produceErr := observeTest(controller, context.Background(), graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"./"}}}, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
			changed := append([]byte(nil), content...)
			changed[0] = 9
			if err := os.WriteFile(filepath.Join(workspace, name), changed, 0600); err != nil {
				return nil, nil, err
			}
		}
		return nil, nil, nil
	})
	if produceErr != nil || controller.failedWrite() || controller.budget.Used() != 54<<20 {
		t.Fatalf("valid image budget rejected: err=%v failed=%v used=%d", produceErr, controller.failedWrite(), controller.budget.Used())
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-1", "attempt-1", "", 20)
	if err != nil || len(page.Descriptors) != 10 {
		t.Fatalf("captured descriptors=%d err=%v", len(page.Descriptors), err)
	}
	available := 0
	for _, item := range page.Descriptors {
		if item.Kind == "preimage" || item.Kind == "postimage" {
			if item.Availability != "available" {
				t.Fatalf("image unavailable: %+v", item)
			}
			available++
		}
	}
	if available != 6 {
		t.Fatalf("available image count=%d", available)
	}
}

func TestCaptureControllerProducerFailureKeepsCompleteImages(t *testing.T) {
	store, workspace, resolver, _ := captureStoreFixture(t)
	path := filepath.Join(workspace, "work.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := newCaptureController(store, resolver, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: "attempt-1"}
	_, _, produceErr := observeTest(controller, context.Background(), graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"work.txt"}}}, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("producer failed")
	})
	if produceErr == nil {
		t.Fatal("producer error lost")
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-1", "attempt-1", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Descriptors {
		if item.Kind == "diff" && item.OmissionReason == "capture_failed" {
			t.Fatalf("producer outcome misclassified as capture failure: %+v", page.Descriptors)
		}
	}
}

func TestCaptureControllerGitObservationOnEachRetry(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	gitPath, err = filepath.Abs(gitPath)
	if err != nil {
		t.Fatal(err)
	}
	store, workspace, resolver, _ := captureStoreFixture(t)
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		command := exec.Command(gitPath, args...)
		command.Dir = workspace
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("Git fixture: %v: %s", err, output)
		}
	}
	path := filepath.Join(workspace, "work.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(workspace, "ignored.txt")
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("ignored.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignored, []byte("ignored before"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, err := gitstate.WithExecutable(context.Background(), gitPath)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newCaptureController(store, resolver, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node := graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{"work.txt", "ignored.txt"}}}
	for index, next := range []string{"first", "second"} {
		ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: events.AttemptID("attempt-" + string(rune('1'+index)))}
		_, _, produceErr := observeTest(controller, ctx, node, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
			if err := os.WriteFile(path, []byte(next), 0600); err != nil {
				return nil, nil, err
			}
			if index == 0 {
				return nil, nil, os.WriteFile(ignored, []byte("ignored after"), 0600)
			}
			return nil, nil, nil
		})
		if produceErr != nil {
			t.Fatal(produceErr)
		}
		page, err := store.EvidenceDescriptors(context.Background(), "run-1", string(ids.AttemptID), "", 10)
		if err != nil {
			t.Fatal(err)
		}
		gitCount := 0
		for _, item := range page.Descriptors {
			if item.Kind == "git_observation" {
				gitCount++
				if item.Availability != "available" {
					t.Fatalf("Git observation unavailable on retry %d: %+v", index, item)
				}
				chunk, err := store.Evidence(context.Background(), item.EvidenceID, 0, 1024)
				wantCount := 1
				if index == 0 {
					wantCount = 2
				}
				want := `{"schema":1,"changed_paths":` + string(rune('0'+wantCount)) + `}`
				if err != nil || string(chunk.Data) != want {
					t.Fatalf("Git observation on retry %d: %q %v", index, chunk.Data, err)
				}
			}
		}
		if gitCount != 1 {
			t.Fatalf("Git observations on retry %d = %d", index, gitCount)
		}
	}
	ids := runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: "attempt-3"}
	_, _, produceErr := observeTest(controller, ctx, graph.NodeDefinition{Type: "agent"}, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		return nil, nil, os.WriteFile(path, []byte("third"), 0600)
	})
	if produceErr != nil {
		t.Fatal(produceErr)
	}
	page, err := store.EvidenceDescriptors(context.Background(), "run-1", "attempt-3", "", 10)
	if err != nil || len(page.Descriptors) != 2 {
		t.Fatalf("no-scope descriptors=%+v err=%v", page, err)
	}
	var gitAvailable, diffUnavailable bool
	for _, item := range page.Descriptors {
		gitAvailable = gitAvailable || item.Kind == "git_observation" && item.Availability == "available"
		diffUnavailable = diffUnavailable || item.Kind == "diff" && item.OmissionReason == "diff_unavailable"
	}
	if !gitAvailable || !diffUnavailable {
		t.Fatalf("no-scope Git observation missing: %+v", page.Descriptors)
	}
	bounded, err := newCaptureController(store, resolver, nil, nil)
	if err != nil || !bounded.budget.Reserve(capture.MaxRunBytes-1) {
		t.Fatalf("budget fixture: %v", err)
	}
	ids = runtime.ExecutionCorrelation{RunID: "run-1", NodeRunID: "node-1", AttemptID: "attempt-4"}
	_, _, produceErr = observeTest(bounded, ctx, node, ids, func() (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		return nil, nil, os.WriteFile(path, []byte("fourth"), 0600)
	})
	if produceErr != nil || bounded.budget.Used() != capture.MaxRunBytes-1 {
		t.Fatalf("generated Git payload exceeded budget: err=%v used=%d", produceErr, bounded.budget.Used())
	}
	page, err = store.EvidenceDescriptors(context.Background(), "run-1", "attempt-4", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	gitLimited := false
	for _, item := range page.Descriptors {
		gitLimited = gitLimited || item.Kind == "git_observation" && item.OmissionReason == "size_limit"
	}
	if !gitLimited {
		t.Fatalf("Git budget omission missing: %+v", page.Descriptors)
	}
}
