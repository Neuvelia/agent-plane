package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/desktop"
	"agentplane/internal/events"
	"agentplane/internal/gate"
	"agentplane/internal/graph"
	"agentplane/internal/history"
	apruntime "agentplane/internal/runtime"
	"agentplane/internal/shell"
)

func TestSidecarStdioHandshake(t *testing.T) {
	name := "agentplane-desktop"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sidecar: %v: %s", err, output)
	}
	var input bytes.Buffer
	for _, request := range []json.RawMessage{
		json.RawMessage(`{"protocol_version":1,"request_id":"wrong","method":"hello","params":{"client_protocol_version":2}}`),
		json.RawMessage(`{"protocol_version":1,"request_id":"ready","method":"hello","params":{"client_protocol_version":1}}`),
		json.RawMessage(`{"protocol_version":1,"request_id":"validation","method":"graphs.validate","params":{"document_id":"00000000-0000-0000-0000-000000000000","revision":1}}`),
	} {
		if err := desktop.WriteFrame(&input, request); err != nil {
			t.Fatal(err)
		}
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	process := exec.Command(binary, "--state-dir", stateDir)
	process.Stdin = &input
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	if err := process.Run(); err != nil {
		t.Fatalf("sidecar: %v: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", stderr.String())
	}
	if info, err := os.Stat(filepath.Join(stateDir, "history.db")); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("sidecar did not open private history store: %v / %v", info, err)
	}
	var responses []desktop.Response
	for i := 0; i < 3; i++ {
		frame, err := desktop.ReadFrame(&stdout)
		if err != nil {
			t.Fatal(err)
		}
		var response desktop.Response
		if err := json.Unmarshal(frame, &response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if responses[0].RequestID != "wrong" || responses[0].Error == nil || responses[0].Error.Code != "version_mismatch" {
		t.Fatalf("version response = %#v", responses[0])
	}
	if responses[1].RequestID != "ready" || responses[1].Error != nil ||
		!bytes.Contains(responses[1].Result, []byte(`"projects.grant"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"graphs.validate"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"runs.preflight"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"runs.start"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"runs.cancel"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"runs.snapshot"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"runs.events"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.list"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.get"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.outcomes"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.events"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.evidence.list"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.evidence"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.pin"`)) ||
		!bytes.Contains(responses[1].Result, []byte(`"history.prune"`)) {
		t.Fatalf("hello response = %#v", responses[1])
	}
	if responses[2].RequestID != "validation" || responses[2].Error == nil || responses[2].Error.Code != "not_found" {
		t.Fatalf("validation response = %#v", responses[2])
	}
	if _, err := desktop.ReadFrame(&stdout); err != io.EOF {
		t.Fatalf("stdout contains non-protocol data: %v", err)
	}
}

func TestHistoryOpensUnderRegistryLock(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	registry, err := desktop.NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	directory, err := registry.HistoryDirectory()
	if err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatalf("history.Open: %v", err)
	}
	defer store.Close()
	if page, err := store.List(context.Background(), history.ListQuery{Limit: 1}); err != nil || len(page.Runs) != 0 {
		t.Fatalf("empty list=%+v err=%v", page, err)
	}
}

func TestHistoryDetailSurvivesServiceRestartWithoutProjectDirectory(t *testing.T) {
	base := t.TempDir()
	stateDir := filepath.Join(base, "state")
	projectDir := filepath.Join(base, "project")
	if err := os.Mkdir(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := desktop.NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectDir, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	workspace, err := registry.AddWorkspace(project.ProjectID, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := registry.HistoryDirectory()
	if err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	safe := func(source string) history.SafeJSON {
		value, err := history.ParseSafeJSON([]byte(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	runID := "11111111-1111-4111-8111-111111111111"
	started := time.Now().UTC().Add(-time.Minute)
	if err := store.Launch(context.Background(), history.LaunchRecord{RunID: runID, ProjectID: project.ProjectID, WorkspaceID: workspace.WorkspaceID, DocumentID: "22222222-2222-4222-8222-222222222222", GraphID: "graph", GraphHash: "sha256:graph", StartedAt: started, Graph: safe(`[1,"graph"]`), Launch: safe(`[1,[]]`), Completeness: safe(`[1,true,true]`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(context.Background(), history.Finalization{RunID: runID, EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safe(`[1,"done"]`), Completeness: safe(`[1,true,true,true,true,true]`), Events: []history.Event{{Sequence: 1, Projection: safe(`{"type":"run_finished"}`)}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(projectDir); err != nil {
		t.Fatal(err)
	}
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	defer inputWriter.Close()
	defer outputReader.Close()
	done := make(chan error, 1)
	go func() {
		err := runDesktopService(context.Background(), []string{"--state-dir", stateDir}, inputReader, outputWriter)
		_ = outputWriter.Close()
		done <- err
	}()
	request := func(id, method, params string) {
		t.Helper()
		if err := desktop.WriteFrame(inputWriter, json.RawMessage(`{"protocol_version":1,"request_id":"`+id+`","method":"`+method+`","params":`+params+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	response := func() desktop.Response {
		t.Helper()
		frame, err := desktop.ReadFrame(outputReader)
		if err != nil {
			t.Fatal(err)
		}
		var result desktop.Response
		if err := json.Unmarshal(frame, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	request("hello", "hello", `{"client_protocol_version":1}`)
	if got := response(); got.Error != nil {
		t.Fatalf("hello: %+v", got.Error)
	}
	request("history", "history.get", `{"run_id":"`+runID+`"}`)
	got := response()
	if got.Error != nil || !bytes.Contains(got.Result, []byte(`"storage_state":"complete"`)) || !bytes.Contains(got.Result, []byte(`"graph":[1,"graph"]`)) || !bytes.Contains(got.Result, []byte(`"result":[1,"done"]`)) {
		t.Fatalf("historical detail after repo removal=%s error=%+v", got.Result, got.Error)
	}
	_ = inputWriter.Close()
	if err := <-done; err != nil {
		t.Fatalf("service shutdown: %v", err)
	}
}

func TestFixedSupervisorModeRejectsMalformedRequestWithoutServiceOutput(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agentplane-desktop")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build sidecar: %v: %s", err, output)
	}
	cmd := exec.Command(binary, "--internal-process-supervisor")
	cmd.Stdin = strings.NewReader("{}\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 125 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("malformed fixed mode: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestBuiltSidecarSupervisesLegacyDiffWithCLIEvidence(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("fixed /usr/bin/diff is a Unix Gate")
	}
	binary := filepath.Join(t.TempDir(), "agentplane-desktop")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build sidecar: %v: %s", err, output)
	}
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	node := graph.NodeDefinition{
		Type: "gate", Config: graph.NodeConfig{
			"kind": "command", "check_id": "legacy-diff", "cwd": cwd,
			"command": "/usr/bin/diff", "left_input": "left", "right_input": "right",
		},
		Inputs: map[string]graph.PortDefinition{
			"left":  {Type: graph.ValueTypeString, Required: true},
			"right": {Type: graph.ValueTypeString, Required: true},
		},
		Outputs: map[string]graph.PortDefinition{
			"verdict": {Type: graph.ValueTypeString, Required: true},
		},
	}
	inputs := map[string]graph.Value{"left": graph.NewStringValue("left"), "right": graph.NewStringValue("right")}
	for _, tc := range []struct{ name, right, verdict string }{
		{"green", "same\n", "green"}, {"red", "different\n", "red"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(cwd, "left"), []byte("same\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cwd, "right"), []byte(tc.right), 0600); err != nil {
				t.Fatal(err)
			}
			var desktopFact, cliFact events.GateCheckPayload
			_, _, desktopErr := (gate.Executor{SupervisorBinary: binary}).ExecuteNodeObserved(context.Background(), "diff", node, inputs,
				apruntime.ExecutionObservation{Check: func(v events.GateCheckPayload) { desktopFact = v }})
			_, _, cliErr := (gate.Executor{}).ExecuteNodeObserved(context.Background(), "diff", node, inputs,
				apruntime.ExecutionObservation{Check: func(v events.GateCheckPayload) { cliFact = v }})
			if string(desktopFact.Verdict) != tc.verdict || string(cliFact.Verdict) != tc.verdict ||
				!reflect.DeepEqual(desktopFact.Evidence, cliFact.Evidence) || desktopFact.Reason != cliFact.Reason ||
				(desktopErr == nil) != (cliErr == nil) {
				t.Fatalf("desktop=%+v %v; cli=%+v %v", desktopFact, desktopErr, cliFact, cliErr)
			}
			if tc.verdict == "red" {
				var red gate.RedError
				if !errors.As(desktopErr, &red) || !errors.As(cliErr, &red) {
					t.Fatalf("red verdict errors: desktop=%v cli=%v", desktopErr, cliErr)
				}
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestServeReturnCancelsAndJoinsOwnedRun(t *testing.T) {
	for _, test := range []struct {
		name  string
		input io.Reader
	}{
		{name: "EOF", input: bytes.NewBuffer(nil)},
		{name: "pipe error", input: errorReader{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			completed := make(chan struct{})
			preparer := func(context.Context, desktop.RunStartRequest) (desktop.PreparedRun, *desktop.ServiceError) {
				return desktop.PreparedRun{Definition: &graph.Definition{ID: "g"}, Executor: shell.Executor{},
					GraphVersion: "v"}, nil
			}
			runner := func(ctx context.Context, _ *graph.Definition, _ map[string]graph.Value, _ apruntime.Executor, options apruntime.Options) (apruntime.RunResult, error) {
				id := events.RunID("00000000-0000-4000-8000-000000000003")
				options.OnRunReady(id, events.NewCollector(id, "g", "v"))
				<-ctx.Done()
				close(completed)
				return apruntime.RunResult{RunID: id, GraphID: "g", GraphVersion: "v", State: apruntime.RunCancelled}, nil
			}
			runs := desktop.NewRunService(context.Background(), preparer, runner)
			request := desktop.RunStartRequest{
				IdempotencyKey: "00000000-0000-4000-8000-000000000001",
				PreflightToken: strings.Repeat("a", 64),
				PayloadDigest:  "sha256:" + strings.Repeat("b", 64),
				DocumentID:     "00000000-0000-4000-8000-000000000002", Revision: 1,
				WorkspaceID: "00000000-0000-4000-8000-000000000004", MaxConcurrency: 1,
			}
			if _, err := runs.Start(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			server := desktop.NewServer("test", desktop.NewRunHandlers(runs))
			var output bytes.Buffer
			_ = serveOwned(context.Background(), server, runs, test.input, &output)
			select {
			case <-completed:
			default:
				t.Fatal("Serve returned without joining owned run")
			}
		})
	}
}

func TestSidecarSIGTERMJoinsRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows native shutdown test belongs to Task 10")
	}
	if marker := os.Getenv("AGENTPLANE_TERM_TEST_MARKER"); marker != "" {
		ctx, stop := serviceContext()
		defer stop()
		completed := make(chan struct{})
		preparer := func(context.Context, desktop.RunStartRequest) (desktop.PreparedRun, *desktop.ServiceError) {
			return desktop.PreparedRun{Definition: &graph.Definition{ID: "g"}, Executor: shell.Executor{}, GraphVersion: "v"}, nil
		}
		runner := func(ctx context.Context, _ *graph.Definition, _ map[string]graph.Value, _ apruntime.Executor, options apruntime.Options) (apruntime.RunResult, error) {
			id := events.RunID("00000000-0000-4000-8000-000000000003")
			options.OnRunReady(id, events.NewCollector(id, "g", "v"))
			<-ctx.Done()
			close(completed)
			return apruntime.RunResult{RunID: id, GraphID: "g", GraphVersion: "v", State: apruntime.RunCancelled}, nil
		}
		runs := desktop.NewRunService(ctx, preparer, runner)
		request := desktop.RunStartRequest{IdempotencyKey: "00000000-0000-4000-8000-000000000001",
			PreflightToken: strings.Repeat("a", 64), PayloadDigest: "sha256:" + strings.Repeat("b", 64),
			DocumentID: "00000000-0000-4000-8000-000000000002", Revision: 1,
			WorkspaceID: "00000000-0000-4000-8000-000000000004", MaxConcurrency: 1}
		if _, err := runs.Start(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		reader, writer := io.Pipe()
		defer writer.Close()
		fmt.Fprintln(os.Stdout, "ready")
		server := desktop.NewServer("test", desktop.NewRunHandlers(runs))
		_ = serveOwned(ctx, server, runs, reader, io.Discard)
		select {
		case <-completed:
		default:
			t.Fatal("SIGTERM returned without joining the run")
		}
		if err := os.WriteFile(marker, []byte("joined"), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	marker := filepath.Join(t.TempDir(), "joined")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSidecarSIGTERMJoinsRun$")
	process.Env = append(os.Environ(), "AGENTPLANE_TERM_TEST_MARKER="+marker)
	stdout, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	process.Stderr = &stderr
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	if scanner := bufio.NewScanner(stdout); !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatalf("helper readiness: %v / %s", scanner.Err(), stderr.String())
	}
	if err := process.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("signal cleanup: %v / %s", err, stderr.String())
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "joined" {
		t.Fatalf("run was not joined before service exit: %q / %v", data, err)
	}
}
