package shell

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

func sizedNode(stdoutBytes, stderrBytes int) graph.NodeDefinition {
	return helperNode("sized", strconv.Itoa(stdoutBytes), strconv.Itoa(stderrBytes))
}

// roadmap: F0.5-AC2
func TestCaptureBeyondLimitKeepsPrefixWithMarkerAndStreamsEverything(t *testing.T) {
	var mu sync.Mutex
	observed := map[string]int{}
	// One process run proves both: the observer sees every byte while the
	// retained result is cut.
	result, err := (Executor{CaptureLimit: 1000}).execute(context.Background(), Request{NodeID: "worker", Node: sizedNode(5000, 1200), Observe: func(stream string, data []byte) {
		mu.Lock()
		defer mu.Unlock()
		observed[stream] += len(data)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if observed["stdout"] != 5000 || observed["stderr"] != 1200 {
		t.Fatalf("observer lost bytes: %v", observed)
	}
	wantStdout := strings.Repeat("o", 1000) + "\n[agentplane: 4000 bytes of stdout omitted; capture limit 1000 bytes]\n"
	wantStderr := strings.Repeat("e", 1000) + "\n[agentplane: 200 bytes of stderr omitted; capture limit 1000 bytes]\n"
	if result.Stdout != wantStdout || result.StdoutOmitted != 4000 {
		t.Fatalf("stdout = %q (omitted %d)", result.Stdout, result.StdoutOmitted)
	}
	if result.Stderr != wantStderr || result.StderrOmitted != 200 {
		t.Fatalf("stderr = %q (omitted %d)", result.Stderr, result.StderrOmitted)
	}
}

func TestCaptureAtLimitIsCompleteWithoutMarker(t *testing.T) {
	result, err := (Executor{CaptureLimit: 1000}).Execute(context.Background(), "worker", sizedNode(1000, 999), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != strings.Repeat("o", 1000) || result.Stderr != strings.Repeat("e", 999) || result.StdoutOmitted != 0 || result.StderrOmitted != 0 {
		t.Fatalf("result = stdout %d bytes (omitted %d), stderr %d bytes (omitted %d)", len(result.Stdout), result.StdoutOmitted, len(result.Stderr), result.StderrOmitted)
	}
}

func TestDefaultCaptureLimitBoundsRetainedOutput(t *testing.T) {
	result, err := (Executor{}).Execute(context.Background(), "worker", sizedNode(DefaultCaptureLimit+10, 0), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StdoutOmitted != 10 || !strings.HasPrefix(result.Stdout, strings.Repeat("o", DefaultCaptureLimit)+"\n[agentplane: 10 bytes of stdout omitted") {
		t.Fatalf("stdout retained %d bytes, omitted %d", len(result.Stdout), result.StdoutOmitted)
	}
}

func TestDeclaredOutputFromTruncatedStreamFailsWithCaptureLimitError(t *testing.T) {
	for _, tc := range []struct {
		output, stream string
		stdout, stderr int
		json           bool
	}{
		{output: "stdout", stream: "stdout", stdout: 1500},
		{output: "stderr", stream: "stderr", stderr: 1500},
		{output: "json", stream: "stdout", stdout: 1500, json: true},
	} {
		t.Run(tc.output, func(t *testing.T) {
			node := sizedNode(tc.stdout, tc.stderr)
			node.Outputs = map[string]graph.PortDefinition{tc.output: {Type: graph.ValueTypeString}}
			if tc.json {
				node.Config["stdout_json"] = true
			}
			outputs, err := (Executor{CaptureLimit: 1000}).Run(context.Background(), Request{NodeID: "worker", Node: node})
			var executionErr *ExecutionError
			var limitErr *CaptureLimitError
			if outputs != nil || !errors.As(err, &executionErr) || executionErr.Kind != FailureOutput || !errors.As(err, &limitErr) {
				t.Fatalf("outputs/error = %v / %v", outputs, err)
			}
			if limitErr.Output != tc.output || limitErr.Stream != tc.stream || limitErr.Limit != 1000 || limitErr.Omitted != 500 || limitErr.DiagnosticCode() != CaptureDiagnostic {
				t.Fatalf("limit error = %#v", limitErr)
			}
		})
	}
}

func TestOutputsNotReadingTruncatedStreamSucceed(t *testing.T) {
	node := sizedNode(1500, 10)
	node.Outputs = map[string]graph.PortDefinition{"exit_code": {Type: graph.ValueTypeNumber}, "stderr": {Type: graph.ValueTypeString}}
	outputs, err := (Executor{CaptureLimit: 1000}).Run(context.Background(), Request{NodeID: "worker", Node: node})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := outputs["stderr"].AsString(); text != strings.Repeat("e", 10) {
		t.Fatalf("stderr output = %q", text)
	}
}

func TestTruncatedDeclaredOutputFailsTheRunWithDiagnosticCode(t *testing.T) {
	node := sizedNode(1500, 0)
	node.Outputs = map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString}}
	def := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "shell-capture-limit", Inputs: map[string]graph.InputDefinition{}, Nodes: map[string]graph.NodeDefinition{"worker": node}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}}
	result, err := runtime.Run(context.Background(), def, nil, Executor{CaptureLimit: 1000}, runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || result.State != run.RunFailed {
		t.Fatalf("run = %v, %v", result.State, err)
	}
	for _, event := range result.Events {
		if event.Type != events.NodeFinished {
			continue
		}
		payload := event.Payload.(events.NodeFinishedPayload)
		if payload.Status != string(run.NodeFailed) || len(payload.DiagnosticCodes) != 1 || payload.DiagnosticCodes[0] != CaptureDiagnostic {
			t.Fatalf("node_finished = %#v", payload)
		}
		return
	}
	t.Fatal("no node_finished event")
}

// A secret that straddles the capture limit must not survive as a prefix in the
// presented run result: the redactor never sees the bytes that complete it.
func TestSecretCutByTheCaptureLimitIsMaskedInTheRunResult(t *testing.T) {
	const secret = "TOPSECRET-4821"
	node := helperNode("json", strings.Repeat("x", 990)+secret+" tail")
	node.Outputs = map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString}}
	def := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "shell-capture-secret", Inputs: map[string]graph.InputDefinition{}, Nodes: map[string]graph.NodeDefinition{"worker": node}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}}
	result, err := runtime.Run(context.Background(), def, nil, Executor{CaptureLimit: 1000}, runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{secret}})
	if err != nil || result.State != run.RunFailed || result.Failure == nil {
		t.Fatalf("run = %v, %v", result.State, err)
	}
	var presented *ExecutionError
	var limitErr *CaptureLimitError
	if !errors.As(result.Failure.Err, &presented) || !errors.As(result.Failure.Err, &limitErr) {
		t.Fatalf("failure = %v", result.Failure.Err)
	}
	retained, _, found := strings.Cut(presented.Result.Stdout, omissionMarkerPrefix)
	if !found || strings.Contains(retained, secret[:4]) || !strings.HasSuffix(retained, "***") {
		t.Fatalf("retained stdout ends with %q", retained[max(len(retained)-20, 0):])
	}
}
