package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/testenv"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("AGENTPLANE_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(90)
	}
	switch args[0] {
	case "echo":
		data, _ := json.Marshal(args[1:])
		fmt.Print(string(data))
		fmt.Fprint(os.Stderr, "stderr only")
	case "fail":
		fmt.Print("partial")
		fmt.Fprint(os.Stderr, "failure detail")
		os.Exit(7)
	case "cwd":
		cwd, _ := os.Getwd()
		fmt.Print(cwd)
	case "env":
		fmt.Printf("%s|%s|%s", os.Getenv("SHELL_ALLOWED"), os.Getenv("SHELL_ADDED"), os.Getenv("SHELL_SECRET"))
	case "mapped-env":
		fmt.Printf("%s|%s", os.Getenv("LEFT_TEXT"), os.Getenv("RIGHT_TEXT"))
	case "json":
		fmt.Print(args[1])
	case "wait":
		_ = os.WriteFile(args[1], []byte("ready"), 0600)
		for {
			time.Sleep(time.Hour)
		}
	case "touch":
		_ = os.WriteFile(args[1], []byte("ran"), 0600)
	case "empty":
	case "bulk":
		data := strings.Repeat("x", 1<<20)
		fmt.Fprint(os.Stdout, data)
		fmt.Fprint(os.Stderr, data)
	case "spawn-holder":
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", "pipeholder", args[1], args[2], args[3])
		child.Env = os.Environ()
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(92)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(args[1]); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(93)
			}
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Print("direct child exited")
		if len(args) > 4 && args[4] == "wait" {
			for {
				time.Sleep(time.Hour)
			}
		}
	case "pipeholder":
		_ = os.WriteFile(args[1], []byte("ready"), 0600)
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(args[2]); err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = os.WriteFile(args[3], []byte("done"), 0600)
	default:
		os.Exit(91)
	}
	os.Exit(0)
}

func helperNode(mode string, more ...string) graph.NodeDefinition {
	args := append([]string{"-test.run=^TestHelperProcess$", "--", mode}, more...)
	return graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{
		"command": os.Args[0], "args": args, "env": map[string]string{"AGENTPLANE_HELPER": "1"},
	}}
}

func TestExecuteCapturesSeparateStreamsAndPreservesArgv(t *testing.T) {
	result, err := (Executor{}).Execute(context.Background(), "worker", helperNode("echo", "two words", "$HOME; $(echo unsafe)", "quote\"'\\"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout != `["two words","$HOME; $(echo unsafe)","quote\"'\\"]` || result.Stderr != "stderr only" {
		t.Fatalf("result = %#v", result)
	}
	if result.StartedAt.IsZero() || result.EndedAt.Before(result.StartedAt) {
		t.Fatalf("timestamps = %v / %v", result.StartedAt, result.EndedAt)
	}
}

func TestBoundStringInputsReachExplicitChildEnvironment(t *testing.T) {
	node := helperNode("mapped-env")
	node.Inputs = map[string]graph.PortDefinition{
		"left":  {Type: graph.ValueTypeString, Required: true},
		"right": {Type: graph.ValueTypeString, Required: true},
	}
	node.Config["input_env"] = map[string]string{"left": "LEFT_TEXT", "right": "RIGHT_TEXT"}
	result, err := (Executor{}).Execute(context.Background(), "join", node, map[string]graph.Value{
		"left": graph.NewStringValue("from-left"), "right": graph.NewStringValue("from-right"),
	})
	if err != nil || result.Stdout != "from-left|from-right" {
		t.Fatalf("mapped inputs: result=%#v err=%v", result, err)
	}
}

func TestInputEnvironmentRejectsInvalidMappingsBeforeProcessStart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*graph.NodeDefinition)
		inputs  map[string]graph.Value
		message string
	}{
		{"undeclared", func(n *graph.NodeDefinition) { n.Config["input_env"] = map[string]string{"absent": "MAPPED"} }, nil, "undeclared input"},
		{"non-string", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"count": {Type: graph.ValueTypeNumber}}
			n.Config["input_env"] = map[string]string{"count": "MAPPED"}
		}, nil, "string input"},
		{"duplicate-target", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"first": {Type: graph.ValueTypeString}, "second": {Type: graph.ValueTypeString}}
			n.Config["input_env"] = map[string]string{"first": "MAPPED", "second": "MAPPED"}
		}, nil, "duplicate"},
		{"duplicate-case-variant", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"first": {Type: graph.ValueTypeString}, "second": {Type: graph.ValueTypeString}}
			n.Config["input_env"] = map[string]string{"first": "MAPPED", "second": "mapped"}
		}, nil, "duplicate"},
		{"conflict", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"first": {Type: graph.ValueTypeString}}
			n.Config["input_env"] = map[string]string{"first": "AGENTPLANE_HELPER"}
		}, nil, "conflicts"},
		{"conflict-case-variant", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"first": {Type: graph.ValueTypeString}}
			n.Config["input_env"] = map[string]string{"first": "agentplane_helper"}
		}, nil, "conflicts"},
		{"missing-required", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"first": {Type: graph.ValueTypeString, Required: true}}
			n.Config["input_env"] = map[string]string{"first": "MAPPED"}
		}, nil, "missing"},
		{"wrong-runtime-type", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"first": {Type: graph.ValueTypeString}}
			n.Config["input_env"] = map[string]string{"first": "MAPPED"}
		}, map[string]graph.Value{"first": graph.NewBooleanValue(true)}, "string value"},
		{"nul-value", func(n *graph.NodeDefinition) {
			n.Inputs = map[string]graph.PortDefinition{"first": {Type: graph.ValueTypeString}}
			n.Config["input_env"] = map[string]string{"first": "MAPPED"}
		}, map[string]graph.Value{"first": graph.NewStringValue("bad\x00value")}, "NUL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := helperNode("touch", filepath.Join(t.TempDir(), "started"))
			started := node.Config["args"].([]string)[3]
			tc.mutate(&node)
			_, err := (Executor{}).Execute(context.Background(), "worker", node, tc.inputs)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error=%v, want %q", err, tc.message)
			}
			var executionErr *ExecutionError
			if !errors.As(err, &executionErr) || executionErr.Kind != FailureConfig {
				t.Fatalf("mapping error kind=%v, want config", err)
			}
			if _, statErr := os.Stat(started); !os.IsNotExist(statErr) {
				t.Fatalf("process started: %v", statErr)
			}
		})
	}
}

func TestExecuteNodeReportsSeparateBoundedLogChunks(t *testing.T) {
	node := helperNode("bulk")
	var mu sync.Mutex
	chunks := map[string][][]byte{}
	_, err := (Executor{}).ExecuteNodeWithLogs(context.Background(), "worker", node, nil, func(stream string, data []byte) {
		mu.Lock()
		defer mu.Unlock()
		if len(data) == 0 || len(data) > 16*1024 {
			t.Errorf("chunk size %d", len(data))
		}
		chunks[stream] = append(chunks[stream], append([]byte(nil), data...))
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		var joined []byte
		for _, chunk := range chunks[stream] {
			joined = append(joined, chunk...)
		}
		if len(joined) != 1<<20 || string(joined) != strings.Repeat("x", 1<<20) {
			t.Fatalf("%s length = %d", stream, len(joined))
		}
	}
}

func TestFailedShellRunEmitsLogsBeforeTerminalEvents(t *testing.T) {
	node := helperNode("fail")
	node.Config["env"].(map[string]string)["PRIVATE_MARKER"] = "configured-private-value"
	def := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "shell-log-fail", Inputs: map[string]graph.InputDefinition{}, Nodes: map[string]graph.NodeDefinition{"worker": node}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}}
	result, err := runtime.Run(context.Background(), def, nil, Executor{}, runtime.Options{LogRedactions: []string{"failure detail"}})
	if err != nil || result.State != runtime.RunFailed {
		t.Fatalf("run = %#v, %v", result, err)
	}
	var stdout, stderr string
	var finishIndex, runFinishIndex int
	for i, event := range result.Events {
		switch event.Type {
		case events.LogChunk:
			payload := event.Payload.(events.LogChunkPayload)
			if payload.Stream == events.Stdout {
				stdout += string(payload.Data)
			} else if payload.Stream == events.Stderr {
				stderr += string(payload.Data)
			}
		case events.NodeFinished:
			finishIndex = i
		case events.RunFinished:
			runFinishIndex = i
		}
	}
	if stdout != "partial" || stderr != "***" || finishIndex == 0 || runFinishIndex != len(result.Events)-1 || finishIndex >= runFinishIndex {
		t.Fatalf("events = %#v", result.Events)
	}
	encoded, _ := json.Marshal(result.Events)
	if strings.Contains(string(encoded), "failure detail") || strings.Contains(string(encoded), "AGENTPLANE_HELPER") || strings.Contains(string(encoded), "configured-private-value") {
		t.Fatalf("secret/config leaked: %s", encoded)
	}
}

func TestLargeShellRunLogsAreCompleteAndBounded(t *testing.T) {
	def := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "shell-log-bound", Inputs: map[string]graph.InputDefinition{}, Nodes: map[string]graph.NodeDefinition{"worker": helperNode("bulk")}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}}
	result, err := runtime.Run(context.Background(), def, nil, Executor{}, runtime.Options{LogRedactions: []string{"secret"}})
	if err != nil || result.State != runtime.RunSucceeded {
		t.Fatalf("run = %#v, %v", result, err)
	}
	var stdout, stderr strings.Builder
	for _, event := range result.Events {
		if event.Type != events.LogChunk {
			continue
		}
		if event.NodeID != "worker" || event.NodeRunID != result.Nodes[0].NodeRunID {
			t.Fatalf("log correlation = %#v", event)
		}
		payload := event.Payload.(events.LogChunkPayload)
		if got := len(payload.Data); got > 16*1024 {
			t.Fatalf("event chunk has %d bytes", got)
		}
		switch payload.Stream {
		case events.Stdout:
			stdout.Write(payload.Data)
		case events.Stderr:
			stderr.Write(payload.Data)
		default:
			t.Fatalf("stream = %q", payload.Stream)
		}
	}
	if stdout.String() != strings.Repeat("x", 1<<20) || stderr.String() != strings.Repeat("x", 1<<20) {
		t.Fatalf("logs incomplete: stdout=%d stderr=%d", stdout.Len(), stderr.Len())
	}
}

func TestExecuteNonzeroExitPreservesEvidence(t *testing.T) {
	result, err := (Executor{}).Execute(context.Background(), "worker", helperNode("fail"), nil)
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureExit || result.ExitCode != 7 || executionErr.Result.ExitCode != 7 {
		t.Fatalf("result/error = %#v / %v", result, err)
	}
	if result.Stdout != "partial" || result.Stderr != "failure detail" || result.EndedAt.IsZero() {
		t.Fatalf("evidence = %#v", result)
	}
}

func TestExecuteCwdAndExplicitEnvironment(t *testing.T) {
	dir := t.TempDir()
	node := helperNode("cwd")
	node.Config["cwd"] = dir
	result, err := (Executor{}).Execute(context.Background(), "worker", node, nil)
	resolvedDir, resolveErr := filepath.EvalSymlinks(dir)
	if err != nil || resolveErr != nil || result.Stdout != resolvedDir {
		t.Fatalf("cwd = %#v / %v", result, err)
	}
	t.Setenv("SHELL_ALLOWED", "inherited")
	t.Setenv("SHELL_ADDED", "parent")
	t.Setenv("SHELL_SECRET", "must-not-leak")
	node = helperNode("env")
	node.Config["env_allowlist"] = []string{"SHELL_ALLOWED", "SHELL_ADDED"}
	node.Config["env"].(map[string]string)["SHELL_ADDED"] = "explicit"
	result, err = (Executor{}).Execute(context.Background(), "worker", node, nil)
	if err != nil || result.Stdout != "inherited|explicit|" {
		t.Fatalf("environment = %#v / %v", result, err)
	}
}

func TestExecuteResolvesRelativeCwdFromRunnerDirectory(t *testing.T) {
	runnerDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	relativeDir, err := filepath.Rel(runnerDir, targetDir)
	if err != nil {
		t.Skipf("runner and temp directory have no relative path: %v", err)
	}
	node := helperNode("cwd")
	node.Config["cwd"] = relativeDir
	result, err := (Executor{}).Execute(context.Background(), "worker", node, nil)
	resolvedTarget, resolveErr := filepath.EvalSymlinks(targetDir)
	if err != nil || resolveErr != nil || result.Stdout != resolvedTarget {
		t.Fatalf("relative cwd %q = %#v / %v", relativeDir, result, err)
	}
}

func TestExecuteDrainsLargeSeparateStreamsAndEmptyOutput(t *testing.T) {
	result, err := (Executor{}).Execute(context.Background(), "worker", helperNode("bulk"), nil)
	if err != nil || len(result.Stdout) != 1<<20 || len(result.Stderr) != 1<<20 || result.ExitCode != 0 {
		t.Fatalf("large streams: stdout=%d stderr=%d exit=%d err=%v", len(result.Stdout), len(result.Stderr), result.ExitCode, err)
	}
	result, err = (Executor{}).Execute(context.Background(), "worker", helperNode("empty"), nil)
	if err != nil || result.Stdout != "" || result.Stderr != "" || result.ExitCode != 0 {
		t.Fatalf("empty output = %#v / %v", result, err)
	}
}

func TestExecuteDistinguishesStartErrors(t *testing.T) {
	missingExe := graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": filepath.Join(t.TempDir(), "missing")}}
	badCwd := helperNode("echo")
	badCwd.Config["cwd"] = filepath.Join(t.TempDir(), "missing")
	for _, node := range []graph.NodeDefinition{missingExe, badCwd} {
		result, err := (Executor{}).Execute(context.Background(), "worker", node, nil)
		var executionErr *ExecutionError
		if !errors.As(err, &executionErr) || executionErr.Kind != FailureStart || result.ExitCode != -1 {
			t.Fatalf("start = %#v / %v", result, err)
		}
	}
}

func TestExecuteCancellationTerminatesChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := (Executor{}).Execute(ctx, "worker", helperNode("wait", marker), nil)
		done <- outcome{result, err}
	}()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case got := <-done:
			t.Fatalf("child ended early: %#v", got)
		case <-deadline:
			t.Fatal("child did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || got.result.EndedAt.IsZero() {
			t.Fatalf("cancellation = %#v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not terminate")
	}
}

func TestExecuteNodeJSONThroughScheduler(t *testing.T) {
	node := helperNode("json", `{"answer":[1,true]}`)
	node.Config["stdout_json"] = true
	node.Outputs = map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString}, "stderr": {Type: graph.ValueTypeString}, "exit_code": {Type: graph.ValueTypeNumber}, "json": {Type: graph.ValueTypeJSON, Required: true}}
	definition := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "shell-test", Inputs: map[string]graph.InputDefinition{}, Nodes: map[string]graph.NodeDefinition{"worker": node}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{"data": {From: "worker.outputs.json"}}}
	run, err := runtime.Run(context.Background(), definition, nil, Executor{}, runtime.Options{})
	if err != nil || run.State != runtime.RunSucceeded || run.Nodes[0].State != runtime.NodeSucceeded {
		t.Fatalf("run = %#v / %v", run, err)
	}
	data, ok := run.Outputs[0].Value.AsJSON()
	if !ok || string(data) != `{"answer":[1,true]}` {
		t.Fatalf("json = %q, %v", data, ok)
	}
	if len(run.Nodes[0].Outputs) != 4 {
		t.Fatalf("node outputs = %#v", run.Nodes[0].Outputs)
	}
	ports := make(map[string]graph.Value)
	for _, output := range run.Nodes[0].Outputs {
		ports[output.Name] = output.Value
	}
	stdout, stdoutOK := ports["stdout"].AsString()
	stderr, stderrOK := ports["stderr"].AsString()
	exitCode, exitOK := ports["exit_code"].AsNumber()
	if !stdoutOK || stdout != `{"answer":[1,true]}` || !stderrOK || stderr != "" || !exitOK || exitCode != "0" {
		t.Fatalf("process outputs = %#v", ports)
	}
}

func TestExecuteNodeRejectsInvalidJSONAndConfig(t *testing.T) {
	node := helperNode("json", `prefix {"x":1}`)
	node.Config["stdout_json"] = true
	node.Outputs = map[string]graph.PortDefinition{"json": {Type: graph.ValueTypeJSON, Required: true}}
	_, err := (Executor{}).ExecuteNode(context.Background(), "worker", node, nil)
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureOutput || !strings.Contains(executionErr.Result.Stdout, "prefix") {
		t.Fatalf("JSON error = %v", err)
	}
	node.Config["args"] = []any{"okay", 7}
	_, err = (Executor{}).ExecuteNode(context.Background(), "worker", node, nil)
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureConfig {
		t.Fatalf("config error = %v", err)
	}
}

func TestExecuteNodeRejectsUnsupportedOutputBeforeProcessStarts(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	node := helperNode("touch", marker)
	node.Outputs = map[string]graph.PortDefinition{"unexpected": {Type: graph.ValueTypeString}}
	_, err := (Executor{}).ExecuteNode(context.Background(), "worker", node, nil)
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureConfig {
		t.Fatalf("output config error = %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("process ran despite invalid output config: %v", err)
	}
}

func TestNonzeroExitFailsSchedulerWithTypedEvidence(t *testing.T) {
	node := helperNode("fail")
	node.Outputs = map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString}}
	definition := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "shell-fail", Inputs: map[string]graph.InputDefinition{}, Nodes: map[string]graph.NodeDefinition{"worker": node}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}}
	run, err := runtime.Run(context.Background(), definition, nil, Executor{}, runtime.Options{})
	var executionErr *ExecutionError
	if err != nil || run.State != runtime.RunFailed || run.Nodes[0].State != runtime.NodeFailed || run.Failure == nil || !errors.As(run.Failure.Err, &executionErr) || executionErr.Kind != FailureExit || executionErr.Result.ExitCode != 7 || len(run.Nodes[0].Outputs) != 0 {
		t.Fatalf("run = %#v / %v", run, err)
	}
}

func TestParsedYAMLShellConfigRunsThroughScheduler(t *testing.T) {
	document := fmt.Sprintf(`schema_version: 0
id: parsed-shell
inputs: {}
nodes:
  worker:
    type: shell
    config:
      command: %q
      args: ["-test.run=^TestHelperProcess$", "--", "echo", "two words"]
      env:
        AGENTPLANE_HELPER: "1"
    outputs:
      stdout:
        type: string
        required: true
edges: []
outputs:
  text:
    from: worker.outputs.stdout
`, os.Args[0])
	definition, err := graph.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.Run(context.Background(), definition, nil, Executor{}, runtime.Options{})
	if err != nil || run.State != runtime.RunSucceeded {
		t.Fatalf("parsed run = %#v / %v; failure = %v", run, err, run.Failure)
	}
	value, ok := run.Outputs[0].Value.AsString()
	if !ok || value != `["two words"]` {
		t.Fatalf("parsed output = %q, %v", value, ok)
	}
}

type cancelAfterFirstCheck struct {
	context.Context
	done   chan struct{}
	checks atomic.Int32
}

func (c *cancelAfterFirstCheck) Done() <-chan struct{} { return c.done }
func (c *cancelAfterFirstCheck) Err() error {
	if c.checks.Add(1) == 1 {
		close(c.done)
		return nil
	}
	return context.Canceled
}

func TestStartErrorIsNotMaskedByConcurrentCancellation(t *testing.T) {
	ctx := &cancelAfterFirstCheck{Context: context.Background(), done: make(chan struct{})}
	node := graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "agentplane-command-definitely-missing"}}
	_, err := (Executor{}).Execute(ctx, "worker", node, nil)
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureStart || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("start error after cancellation = %v", err)
	}
}

func TestExplicitEnvironmentFollowsCaseVariantAllowlistValue(t *testing.T) {
	t.Setenv("shell_case", "parent")
	environment := buildEnvironment(config{envAllowlist: []string{"shell_case"}, env: map[string]string{"SHELL_CASE": "explicit"}})
	if len(environment) != 2 || environment[0] != "shell_case=parent" || environment[1] != "SHELL_CASE=explicit" {
		t.Fatalf("environment order = %#v", environment)
	}
}

func TestConfigErrorsSelectStableField(t *testing.T) {
	node := graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "ignored", "zzz": true, "aaa": true}}
	for range 100 {
		_, err := parseConfig(node)
		if err == nil || !strings.Contains(err.Error(), `"aaa"`) {
			t.Fatalf("unknown field error = %v", err)
		}
	}
}

func TestMalformedInputEnvErrorNamesInputEnv(t *testing.T) {
	node := helperNode("echo")
	node.Config["input_env"] = []string{"not-a-map"}
	_, err := parseConfig(node)
	if err == nil || !strings.Contains(err.Error(), "input_env must be a mapping of string values") || strings.Contains(err.Error(), "input_env: env must") {
		t.Fatalf("input_env configuration error = %v", err)
	}
}

func TestExecuteDoesNotWaitForeverForInheritedOutputPipes(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "holder-ready")
	release := filepath.Join(dir, "holder-release")
	holderDone := filepath.Join(dir, "holder-done")
	releaseHolder := func() { _ = os.WriteFile(release, []byte("release"), 0600) }
	defer releaseAndWaitForHolder(t, ready, release, holderDone)
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := (Executor{}).Execute(context.Background(), "worker", helperNode("spawn-holder", ready, release, holderDone), nil)
		done <- outcome{result, err}
	}()
	select {
	case got := <-done:
		var executionErr *ExecutionError
		if !errors.As(got.err, &executionErr) || executionErr.Kind != FailureOutput || !errors.Is(got.err, exec.ErrWaitDelay) || got.result.ExitCode != 0 || got.result.Stdout != "direct child exited" {
			t.Fatalf("inherited-pipe result = %#v / %v", got.result, got.err)
		}
	case <-time.After(3 * time.Second):
		releaseHolder()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("executor did not return after holder released its pipes")
		}
		t.Fatal("executor waited for a descendant's inherited output pipes")
	}
}

func TestExecuteCancellationReturnsWithInheritedOutputPipes(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "holder-ready")
	release := filepath.Join(dir, "holder-release")
	holderDone := filepath.Join(dir, "holder-done")
	defer releaseAndWaitForHolder(t, ready, release, holderDone)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := (Executor{}).Execute(ctx, "worker", helperNode("spawn-holder", ready, release, holderDone, "wait"), nil)
		done <- outcome{result, err}
	}()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case got := <-done:
			t.Fatalf("direct child ended before cancellation: %#v", got)
		case <-deadline:
			t.Fatal("pipe holder did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case got := <-done:
		var executionErr *ExecutionError
		if !errors.As(got.err, &executionErr) || executionErr.Kind != FailureCanceled || !errors.Is(got.err, context.Canceled) || got.result.EndedAt.IsZero() {
			t.Fatalf("cancellation with inherited pipes = %#v / %v", got.result, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation remained blocked on inherited output pipes")
	}
}

func releaseAndWaitForHolder(t *testing.T, ready, release, done string) {
	t.Helper()
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Errorf("release pipe holder: %v", err)
		return
	}
	if _, err := os.Stat(ready); err != nil {
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(done); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Error("pipe holder did not exit after release")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func attemptNode(extra map[string]any) graph.NodeDefinition {
	cfg := map[string]any{"command": "/usr/bin/printenv", "args": []any{"AGENTPLANE_ATTEMPT"}}
	for k, v := range extra {
		cfg[k] = v
	}
	return graph.NodeDefinition{Type: "shell", Config: cfg, Outputs: map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString}}}
}

// The child must see exactly the attempt number the runtime assigned.
func TestExecuteNodeAttemptInjectsTheAttemptNumberOnlyForLoopAttempts(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/printenv")
	for _, attempt := range []int{1, 2, 7} {
		out, err := Executor{}.ExecuteNodeAttempt(context.Background(), "n", attemptNode(nil), nil, nil, attempt)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := out["stdout"].AsString(); got != strconv.Itoa(attempt)+"\n" {
			t.Fatalf("attempt %d: child saw %q", attempt, got)
		}
	}
	// attempt 0 is "not a loop attempt": nothing is injected, printenv exits 1.
	if _, err := (Executor{}).ExecuteNodeAttempt(context.Background(), "n", attemptNode(nil), nil, nil, 0); err == nil {
		t.Fatal("a non-loop execution must not receive the variable")
	}
}

func TestReservedAttemptVariableIsRejectedEverywhereInAnyCase(t *testing.T) {
	declared := map[string]graph.PortDefinition{"x": {Type: graph.ValueTypeString}}
	for name, node := range map[string]graph.NodeDefinition{
		"env":               attemptNode(map[string]any{"env": map[string]any{"AGENTPLANE_ATTEMPT": "9"}}),
		"env lower case":    attemptNode(map[string]any{"env": map[string]any{"agentplane_attempt": "9"}}),
		"env_allowlist":     attemptNode(map[string]any{"env_allowlist": []any{"AGENTPLANE_ATTEMPT"}}),
		"allowlist mixed":   attemptNode(map[string]any{"env_allowlist": []any{"AgentPlane_Attempt"}}),
		"input_env mapping": {Type: "shell", Inputs: declared, Config: map[string]any{"command": "/usr/bin/true", "input_env": map[string]any{"x": "AGENTPLANE_ATTEMPT"}}},
		"input_env mixed":   {Type: "shell", Inputs: declared, Config: map[string]any{"command": "/usr/bin/true", "input_env": map[string]any{"x": "agentplane_attempt"}}},
	} {
		t.Run(name, func(t *testing.T) {
			err := Executor{}.ValidateNodeConfig("n", node)
			if err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("want a 'reserved' error, got %v", err)
			}
		})
	}
	if err := (Executor{}).ValidateNodeConfig("n", attemptNode(nil)); err != nil {
		t.Fatalf("an ordinary node was rejected: %v", err)
	}
}
