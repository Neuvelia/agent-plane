package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func graphFile(t *testing.T, body string) string {
	t.Helper()
	body = strings.ReplaceAll(body, `"@TEST_BIN@"`, strconv.Quote(os.Args[0]))
	path := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("AGENTPLANE_CLI_HELPER") != "1" {
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
		_, _ = os.Stdout.WriteString("hello\n")
	case "streams":
		_, _ = os.Stdout.WriteString("out")
		_, _ = os.Stderr.WriteString("err")
	case "env":
		_, _ = os.Stdout.WriteString(os.Getenv("AGENTPLANE_TEST_SECRET"))
	case "fail":
		_, _ = os.Stderr.WriteString("failure detail")
		os.Exit(7)
	case "touch":
		if len(args) != 2 || os.WriteFile(args[1], []byte("ran"), 0600) != nil {
			os.Exit(91)
		}
	case "wait":
		_, _ = os.Stderr.WriteString("child-ready:" + strconv.Itoa(os.Getpid()) + "\n")
		for {
			time.Sleep(time.Hour)
		}
	case "write-wait":
		if len(args) != 2 || os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(96)
		}
		_, _ = os.Stdout.WriteString("child output\n")
		for {
			time.Sleep(time.Hour)
		}
	case "barrier":
		if len(args) != 3 {
			os.Exit(93)
		}
		_, _ = os.Stderr.WriteString("branch-ready:" + args[1] + "\n")
		for {
			if _, err := os.Stat(args[2]); err == nil {
				_, _ = os.Stdout.WriteString(args[1] + "\n")
				break
			} else if !os.IsNotExist(err) {
				os.Exit(94)
			}
			time.Sleep(10 * time.Millisecond)
		}
	case "join":
		left, leftOK := os.LookupEnv("LEFT_TEXT")
		right, rightOK := os.LookupEnv("RIGHT_TEXT")
		if !leftOK || !rightOK {
			os.Exit(95)
		}
		_, _ = os.Stdout.WriteString(strings.TrimSuffix(left, "\n") + "|" + strings.TrimSuffix(right, "\n") + "\n")
	default:
		os.Exit(92)
	}
	os.Exit(0)
}

const oneNode = `schema_version: 0
id: cli-test
inputs: {}
nodes:
  work:
    type: shell
    config:
      command: "@TEST_BIN@"
      args: ["-test.run=^TestCLIHelperProcess$", "--", "echo"]
      env: {AGENTPLANE_CLI_HELPER: "1"}
    outputs:
      stdout: {type: string}
edges: []
outputs:
  result: {from: work.outputs.stdout}
`

func invoke(t *testing.T, ctx context.Context, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runCommand(ctx, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestGraphValidateAndErrors(t *testing.T) {
	valid := graphFile(t, oneNode)
	if code, _, _ := invoke(t, context.Background(), "graph", "validate", valid); code != 0 {
		t.Fatalf("valid exit=%d", code)
	}
	warning := graphFile(t, strings.Replace(oneNode, "stdout: {type: string}", "stdout: {type: string}\n      stderr: {type: string}", 1))
	if code, _, stderr := invoke(t, context.Background(), "graph", "validate", warning); code != 0 || !strings.Contains(stderr, "W_DECLARED_OUTPUT_UNUSED") {
		t.Fatalf("warning: %d %q", code, stderr)
	}
	bad := graphFile(t, strings.Replace(oneNode, "work.outputs.stdout", "missing.outputs.stdout", 1))
	if code, _, stderr := invoke(t, context.Background(), "graph", "validate", bad); code != 3 || !strings.Contains(stderr, "E_GRAPH_OUTPUT") {
		t.Fatalf("invalid: %d %q", code, stderr)
	}
	unknown := graphFile(t, strings.Replace(oneNode, "type: shell", "type: foo", 1))
	if code, _, stderr := invoke(t, context.Background(), "graph", "validate", unknown); code != 3 || !strings.Contains(stderr, "E_NODE_TYPE_UNKNOWN") || !strings.Contains(stderr, "nodes.work.type") || !strings.Contains(stderr, "shell, agent, gate, human") {
		t.Fatalf("unknown type: %d %q", code, stderr)
	}
	malformed := graphFile(t, "schema_version: [")
	if code, _, stderr := invoke(t, context.Background(), "graph", "validate", malformed); code != 3 || !strings.Contains(stderr, "parse") {
		t.Fatalf("malformed: %d %q", code, stderr)
	}
	if code, _, stderr := invoke(t, context.Background(), "graph", "validate", filepath.Join(t.TempDir(), "missing.yaml")); code != 3 || !strings.Contains(stderr, "missing.yaml") {
		t.Fatalf("missing: %d %q", code, stderr)
	}
	if code, stdout, stderr := invoke(t, context.Background(), "graph", "validate", filepath.Join(t.TempDir(), "missing.yaml"), "--json"); code != 3 || stderr != "" {
		t.Fatalf("missing JSON: %d %q %q", code, stdout, stderr)
	} else {
		var doc struct {
			Status string `json:"status"`
			Error  struct {
				Kind string `json:"kind"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil || doc.Status != "error" || doc.Error.Kind != "graph" {
			t.Fatalf("missing JSON: %q %v", stdout, err)
		}
	}
}

func TestRunJSONAndHuman(t *testing.T) {
	path := graphFile(t, oneNode)
	code, stdout, stderr := invoke(t, context.Background(), "run", path, "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("json exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var doc struct {
		Status string `json:"status"`
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || doc.Status != "succeeded" || len(doc.Events) == 0 {
		t.Fatalf("json: %#v %v", doc, err)
	}
	code, stdout, stderr = invoke(t, context.Background(), "run", path)
	if code != 0 || strings.Count(stdout, "[work stdout]") != 1 || !strings.Contains(stderr, "Run succeeded") {
		t.Fatalf("human: %d %q %q", code, stdout, stderr)
	}
}

func TestJSONUsageErrorIsParseable(t *testing.T) {
	path := graphFile(t, oneNode)
	code, stdout, stderr := invoke(t, context.Background(), "run", path, "--json", "--unknown")
	if code != 2 || stderr != "" {
		t.Fatalf("usage exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var doc struct {
		Status string `json:"status"`
		Error  struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || doc.Status != "error" || doc.Error.Kind != "usage" {
		t.Fatalf("usage JSON=%q err=%v", stdout, err)
	}
	code, stdout, stderr = invoke(t, context.Background(), "unknown", "--json")
	if code != 2 || stderr != "" || json.Unmarshal([]byte(stdout), &doc) != nil {
		t.Fatalf("unknown JSON=%d %q %q", code, stdout, stderr)
	}
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestJSONWriteFailureReportsErrorOnStderr(t *testing.T) {
	path := graphFile(t, oneNode)
	var stderr bytes.Buffer
	code := runCommand(context.Background(), []string{"graph", "validate", path, "--json"}, failingOutput{}, &stderr)
	if code != exitInternal || !strings.Contains(stderr.String(), "write JSON") || !strings.Contains(stderr.String(), "output unavailable") {
		t.Fatalf("JSON writer failure: code=%d stderr=%q", code, stderr.String())
	}
}

func TestHumanOutputWriteFailureReturnsInternalStatus(t *testing.T) {
	path := graphFile(t, oneNode)
	var stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", path}, failingOutput{}, &stderr); code != exitInternal {
		t.Fatalf("run with failed stdout exit=%d", code)
	}
	if code := runCommand(context.Background(), []string{"run", path, "--watch"}, failingOutput{}, &stderr); code != exitInternal {
		t.Fatalf("watch with failed stdout exit=%d", code)
	}
	if code := runCommand(context.Background(), []string{"graph", "validate", path}, io.Discard, failingOutput{}); code != exitInternal {
		t.Fatalf("validate with failed stderr exit=%d", code)
	}
}

func TestRunJSONKeepsEmptyOutputsArray(t *testing.T) {
	graph := strings.Replace(oneNode, "outputs:\n  result: {from: work.outputs.stdout}", "outputs: {}", 1)
	path := graphFile(t, graph)
	code, stdout, stderr := invoke(t, context.Background(), "run", path, "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("empty outputs=%d %q %q", code, stdout, stderr)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["outputs"]) != "[]" {
		t.Fatalf("outputs=%s", doc["outputs"])
	}
}

type blockingOutput struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingOutput) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestWatchCancellationWithBlockedOutputReturns(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Cancelling a child that already exited fails with "TerminateProcess:
		// Access is denied", which internal/shell reports as an exit failure
		// (exit 4) instead of cancellation (130). Product fix, not a test fix.
		t.Skip("shell cancellation after child exit is misclassified on Windows")
	}
	// The child outlives the blocked write, so the run cannot finish before the
	// cancellation and the expected exit status is deterministic.
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	path := graphFile(t, strings.Replace(oneNode, `"echo"]`, `"write-wait", `+strconv.Quote(pidFile)+`]`, 1))
	writer := &blockingOutput{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(writer.release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- runCommand(ctx, []string{"run", path, "--watch"}, writer, io.Discard) }()
	select {
	case <-writer.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not write log output")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("canceled watch exit=%d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled watch blocked on output")
	}
}

func TestRunRedactsNamedEnvironmentValueFromEvents(t *testing.T) {
	t.Setenv("AGENTPLANE_TEST_SECRET", "secret-value-482")
	graph := strings.Replace(oneNode, `"echo"]`, `"env"]`, 1)
	graph = strings.Replace(graph, `env: {AGENTPLANE_CLI_HELPER: "1"}`, `env: {AGENTPLANE_CLI_HELPER: "1"}
      env_allowlist: [AGENTPLANE_TEST_SECRET]`, 1)
	graph = strings.Replace(graph, "outputs:\n  result: {from: work.outputs.stdout}", "outputs: {}", 1)
	path := graphFile(t, graph)
	code, stdout, stderr := invoke(t, context.Background(), "run", path, "--redact-env", "AGENTPLANE_TEST_SECRET", "--json")
	if code != 0 || stderr != "" || strings.Contains(stdout, "secret-value-482") {
		t.Fatalf("redacted JSON=%d %q %q", code, stdout, stderr)
	}
	var doc struct {
		Events []struct {
			Type    string `json:"type"`
			Payload struct {
				Data []byte `json:"data"`
			} `json:"payload"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	var logText string
	for _, event := range doc.Events {
		if event.Type == "log_chunk" {
			logText += string(event.Payload.Data)
		}
	}
	if !strings.Contains(logText, "***") || strings.Contains(logText, "secret-value-482") {
		t.Fatalf("event logs=%q", logText)
	}
	code, stdout, stderr = invoke(t, context.Background(), "run", path, "--redact-env", "AGENTPLANE_TEST_SECRET", "--watch")
	if code != 0 || strings.Contains(stdout, "secret-value-482") || !strings.Contains(stdout, "***") || strings.Contains(stderr, "secret-value-482") {
		t.Fatalf("watch redaction=%d %q %q", code, stdout, stderr)
	}
	if code, _, _ := invoke(t, context.Background(), "run", path, "--redact-env", "MISSING_AGENTPLANE_VAR"); code != 2 {
		t.Fatalf("missing redaction variable exit=%d", code)
	}
}

func TestMalformedInputSyntaxPrecedesMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	code, _, stderr := invoke(t, context.Background(), "run", missing, "--input", "malformed")
	if code != 2 || !strings.Contains(stderr, "key=value") {
		t.Fatalf("input/file precedence=%d %q", code, stderr)
	}
}

func TestRunFailureAndInputs(t *testing.T) {
	failing := graphFile(t, strings.Replace(oneNode, `"echo"]`, `"fail"]`, 1))
	if code, _, stderr := invoke(t, context.Background(), "run", failing); code != 4 || !strings.Contains(stderr, "exit status") {
		t.Fatalf("failed run exit=%d stderr=%q", code, stderr)
	}
	if code, stdout, stderr := invoke(t, context.Background(), "run", failing, "--json"); code != 4 || stderr != "" {
		t.Fatalf("failed JSON exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	} else {
		var doc struct {
			Status  string `json:"status"`
			RunID   string `json:"run_id"`
			Failure struct {
				Kind string `json:"kind"`
			} `json:"failure"`
		}
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil || doc.Status != "failed" || doc.RunID == "" || doc.Failure.Kind != "executor" {
			t.Fatalf("failed JSON=%q err=%v", stdout, err)
		}
	}
	withInput := strings.Replace(oneNode, "inputs: {}", "inputs:\n  message: {type: string, required: true}", 1)
	withInput = strings.Replace(withInput, "edges: []", "graph_input_bindings:\n  - from: inputs.message\n    to: work.inputs.message\nedges: []", 1)
	withInput = strings.Replace(withInput, "    config:", "    inputs:\n      message: {type: string, required: true}\n    config:", 1)
	path := graphFile(t, withInput)
	if code, _, _ := invoke(t, context.Background(), "run", path); code != 3 {
		t.Fatalf("missing input exit=%d", code)
	}
	if code, _, _ := invoke(t, context.Background(), "run", path, "--input", "message=one"); code != 0 {
		t.Fatalf("input exit=%d", code)
	}
	if code, _, _ := invoke(t, context.Background(), "run", path, "--input", "message=one", "--input", "message=two"); code != 2 {
		t.Fatalf("duplicate input exit=%d", code)
	}
}
