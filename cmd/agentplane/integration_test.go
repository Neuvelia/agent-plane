package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func inRepositoryRoot(t *testing.T) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// roadmap: F0.4-AC2, F0.7-AC1
func TestBuiltCLIPhase0DataFlow(t *testing.T) {
	inRepositoryRoot(t)
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	build := exec.Command("go", "build", "-o", binary, "./cmd/agentplane")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, output)
	}
	graphPath := filepath.Join(".agents", "graphs", "phase0-cli.yaml")
	for _, message := range []string{"alpha", "beta", "a=b"} {
		command := exec.Command(binary, "run", graphPath, "--input", "message="+message, "--json")
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err != nil || stderr.Len() != 0 {
			t.Fatalf("run %q: err=%v stderr=%q", message, err, stderr.String())
		}
		var doc struct {
			Status  string `json:"status"`
			Outputs []struct {
				Value string `json:"value"`
			} `json:"outputs"`
			Events []struct {
				Type     string `json:"type"`
				NodeID   string `json:"node_id"`
				Sequence uint64 `json:"sequence"`
			} `json:"events"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
			t.Fatalf("JSON result: %v output=%q", err, stdout.String())
		}
		if doc.Status != "succeeded" || len(doc.Outputs) != 1 || doc.Outputs[0].Value != "left:"+message+"|right:"+message+"\n" {
			t.Fatalf("result for %q: status=%q outputs=%#v", message, doc.Status, doc.Outputs)
		}
		var leftStart, rightStart, leftFinish, rightFinish, joinStart uint64
		for i, event := range doc.Events {
			if event.Sequence != uint64(i+1) {
				t.Fatalf("sequence at %d: %d", i, event.Sequence)
			}
			switch event.Type + ":" + event.NodeID {
			case "node_started:left":
				leftStart = event.Sequence
			case "node_started:right":
				rightStart = event.Sequence
			case "node_finished:left":
				leftFinish = event.Sequence
			case "node_finished:right":
				rightFinish = event.Sequence
			case "node_started:join":
				joinStart = event.Sequence
			}
		}
		if leftStart == 0 || rightStart == 0 || leftStart >= rightFinish || rightStart >= leftFinish || joinStart <= leftFinish || joinStart <= rightFinish {
			t.Fatalf("dependency order for %q: %#v", message, doc.Events)
		}
	}
}

func TestParallelFixtureAndWatch(t *testing.T) {
	inRepositoryRoot(t)
	path := filepath.Join(".agents", "graphs", "hello-parallel.yaml")
	code, stdout, stderr := invoke(t, context.Background(), "run", path, "--watch", "--max-concurrency", "2")
	if code != 0 || !strings.Contains(stdout, "[left stdout]") || !strings.Contains(stdout, "[right stdout]") || !strings.Contains(stderr, "left running") || !strings.Contains(stderr, "right running") || !strings.Contains(stderr, "merge succeeded") {
		t.Fatalf("watch exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, document, _ := invoke(t, context.Background(), "run", path, "--json")
	var parsed struct {
		Events []struct {
			Type     string `json:"type"`
			NodeID   string `json:"node_id"`
			Sequence uint64 `json:"sequence"`
		} `json:"events"`
		Outputs []struct {
			Name  string
			Value string
		} `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(document), &parsed); err != nil {
		t.Fatal(err)
	}
	starts := map[string]uint64{}
	finishes := map[string]uint64{}
	for i, event := range parsed.Events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("sequence %d = %d", i, event.Sequence)
		}
		if event.Type == "node_started" {
			starts[event.NodeID] = event.Sequence
		}
		if event.Type == "node_finished" {
			finishes[event.NodeID] = event.Sequence
		}
	}
	if starts["left"] == 0 || starts["right"] == 0 || starts["left"] >= finishes["right"] || starts["right"] >= finishes["left"] || starts["merge"] <= finishes["left"] || starts["merge"] <= finishes["right"] {
		t.Fatalf("start=%v finish=%v", starts, finishes)
	}
	if len(parsed.Outputs) != 1 || parsed.Outputs[0].Name != "result" || parsed.Outputs[0].Value != "left|right\n" {
		t.Fatalf("outputs=%#v", parsed.Outputs)
	}
}

func TestPhase0CLIExampleRequiresAndBindsInput(t *testing.T) {
	inRepositoryRoot(t)
	path := filepath.Join(".agents", "graphs", "phase0-cli.yaml")
	if code, _, _ := invoke(t, context.Background(), "graph", "validate", path); code != 0 {
		t.Fatalf("example validation exit=%d", code)
	}
	if code, _, stderr := invoke(t, context.Background(), "run", path); code != 3 || !strings.Contains(stderr, "E_GRAPH_INPUT_VALUE_MISSING") {
		t.Fatalf("missing input=%d %q", code, stderr)
	}
	code, data, stderr := invoke(t, context.Background(), "run", path, "--input", "message=hello", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("example run=%d %q", code, stderr)
	}
	var doc struct {
		Status  string `json:"status"`
		Outputs []struct {
			Value string `json:"value"`
		} `json:"outputs"`
		Events []struct {
			Type     string `json:"type"`
			NodeID   string `json:"node_id"`
			Sequence uint64 `json:"sequence"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Status != "succeeded" || len(doc.Outputs) != 1 || doc.Outputs[0].Value != "left:hello|right:hello\n" {
		t.Fatalf("example result=%#v", doc)
	}
	code, changed, stderr := invoke(t, context.Background(), "run", path, "--input", "message=changed", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("changed input run=%d stderr=%q", code, stderr)
	}
	var changedDoc struct {
		Outputs []struct {
			Value string `json:"value"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(changed), &changedDoc); err != nil || len(changedDoc.Outputs) != 1 || changedDoc.Outputs[0].Value != "left:changed|right:changed\n" {
		t.Fatalf("changed result=%#v err=%v", changedDoc, err)
	}
	var leftStart, rightStart, leftFinish, rightFinish, joinStart uint64
	for _, event := range doc.Events {
		switch event.Type + ":" + event.NodeID {
		case "node_started:left":
			leftStart = event.Sequence
		case "node_started:right":
			rightStart = event.Sequence
		case "node_finished:left":
			leftFinish = event.Sequence
		case "node_finished:right":
			rightFinish = event.Sequence
		case "node_started:join":
			joinStart = event.Sequence
		}
	}
	if leftStart == 0 || rightStart == 0 || leftStart >= rightFinish || rightStart >= leftFinish || joinStart <= leftFinish || joinStart <= rightFinish {
		t.Fatalf("event order=%#v", doc.Events)
	}
}

func TestStdoutStderrAndJSONSeparation(t *testing.T) {
	graph := strings.Replace(oneNode, `"echo"]`, `"streams"]`, 1)
	path := graphFile(t, graph)
	code, stdout, stderr := invoke(t, context.Background(), "run", path, "--watch")
	if code != 0 || !strings.Contains(stdout, "[work stdout] out") || !strings.Contains(stderr, "[work stderr] err") {
		t.Fatalf("streams=%d %q %q", code, stdout, stderr)
	}
	code, stdout, stderr = invoke(t, context.Background(), "run", path, "--json")
	if code != 0 || stderr != "" || strings.Contains(stdout, "Run started:") {
		t.Fatalf("json streams=%d %q %q", code, stdout, stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
}

func TestInputErrorsAndCancellation(t *testing.T) {
	withInput := strings.Replace(oneNode, "inputs: {}", "inputs:\n  message: {type: string, required: true}\n  count: {type: number}", 1)
	withInput = strings.Replace(withInput, "edges: []", "graph_input_bindings:\n  - from: inputs.message\n    to: work.inputs.message\nedges: []", 1)
	withInput = strings.Replace(withInput, "    config:", "    inputs:\n      message: {type: string, required: true}\n    config:", 1)
	path := graphFile(t, withInput)
	if code, _, _ := invoke(t, context.Background(), "run", path, "--input", "message=hello", "--input", "count=2"); code != 0 {
		t.Fatalf("repeated distinct input exit=%d", code)
	}
	if code, _, stderr := invoke(t, context.Background(), "run", path, "--input", "message=x", "--input", "unknown=x"); code != 3 || !strings.Contains(stderr, "E_GRAPH_INPUT_VALUE_UNKNOWN") {
		t.Fatalf("unknown input=%d %q", code, stderr)
	}
	if code, _, stderr := invoke(t, context.Background(), "run", path, "--input", "message=x", "--input", "count=nope"); code != 3 || !strings.Contains(stderr, "E_CLI_INPUT_VALUE_INVALID") {
		t.Fatalf("typed input=%d %q", code, stderr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code, _, stderr := invoke(t, ctx, "run", graphFile(t, oneNode)); code != 130 || !strings.Contains(stderr, "Run cancelled") {
		t.Fatalf("cancel=%d %q", code, stderr)
	}
}

func TestInvalidGraphDoesNotStartShellProcess(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "executed")
	graph := strings.Replace(oneNode, `"echo"]`, `"touch", `+strconv.Quote(sentinel)+`]`, 1)
	graph = strings.Replace(graph, "work.outputs.stdout", "missing.outputs.stdout", 1)
	path := graphFile(t, graph)
	code, _, stderr := invoke(t, context.Background(), "run", path)
	if code != 3 || !strings.Contains(stderr, "E_GRAPH_OUTPUT") {
		t.Fatalf("invalid run=%d %q", code, stderr)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("invalid graph launched process: stat=%v", err)
	}
}

// roadmap: F0.7-AC2
func TestBuiltBinaryExitAndJSON(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	valid := graphFile(t, oneNode)
	failed := graphFile(t, strings.Replace(oneNode, `"echo"]`, `"fail"]`, 1))
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"graph", "validate", valid}, 0},
		{[]string{"run", valid, "--json"}, 0},
		{[]string{"run", failed}, 4},
		{[]string{"graph", "validate", filepath.Join(t.TempDir(), "missing")}, 3},
		{[]string{"run"}, 2},
	} {
		command := exec.Command(binary, tc.args...)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		code := 0
		if err != nil {
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != tc.want {
			t.Fatalf("%v exit=%d want=%d stdout=%q stderr=%q", tc.args, code, tc.want, stdout.String(), stderr.String())
		}
		if len(tc.args) == 3 && tc.args[2] == "--json" {
			var doc map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil || stderr.Len() != 0 {
				t.Fatalf("json=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
			}
		}
	}
}

func runWithClosedOutputPipe(t *testing.T, command *exec.Cmd, closeStderr bool) error {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	if closeStderr {
		command.Stderr = writer
	} else {
		command.Stdout = writer
	}
	if err := command.Start(); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return command.Wait()
}

func TestBuiltBinaryClosedOutputPipesReturnInternalStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("broken-pipe signal behavior is Unix-specific")
	}
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	path := graphFile(t, oneNode)
	for _, tc := range []struct {
		name        string
		args        []string
		closeStderr bool
	}{
		{"human run stdout", []string{"run", path}, false},
		{"JSON run stdout", []string{"run", path, "--json"}, false},
		{"validation stderr", []string{"graph", "validate", path}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := exec.Command(binary, tc.args...)
			if tc.closeStderr {
				command.Stdout = io.Discard
			} else {
				command.Stderr = io.Discard
			}
			err := runWithClosedOutputPipe(t, command, tc.closeStderr)
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 {
				t.Fatalf("closed output pipe exit=%v, want status 1", err)
			}
		})
	}
}

func TestBuiltBinaryWatchOutputFailureCancelsShellChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("broken-pipe signal and process checks are Unix-specific")
	}
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	childPIDPath := filepath.Join(t.TempDir(), "child.pid")
	graph := strings.Replace(oneNode, `"echo"]`, `"write-wait", `+strconv.Quote(childPIDPath)+`]`, 1)
	path := graphFile(t, graph)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "run", path, "--watch")
	command.Stderr = io.Discard
	err := runWithClosedOutputPipe(t, command, false)
	pidData, readErr := os.ReadFile(childPIDPath)
	if readErr != nil {
		t.Fatalf("read child PID after CLI exit %v: %v", err, readErr)
	}
	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Kill() }()
	if exit, ok := command.ProcessState.Sys().(syscall.WaitStatus); !ok || !exit.Exited() || exit.ExitStatus() != 1 {
		t.Fatalf("watch output failure exit=%v, want status 1", command.ProcessState)
	}
	if err := child.Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("shell child %d remained alive after CLI output failure", pid)
	}
}

// roadmap: F0.3-AC2, F0.4-AC1
func TestBuiltCLIStartsIndependentShellProcessesBeforeEitherFinishes(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	release := filepath.Join(t.TempDir(), "release")
	defer func() { _ = os.WriteFile(release, []byte("go"), 0600) }()
	definition := `schema_version: 0
id: parallel-process-proof
inputs: {}
nodes:
  left:
    type: shell
    config:
      command: "@TEST_BIN@"
      args: ["-test.run=^TestCLIHelperProcess$", "--", "barrier", "left", @RELEASE@]
      env: {AGENTPLANE_CLI_HELPER: "1"}
    outputs:
      stdout: {type: string}
  right:
    type: shell
    config:
      command: "@TEST_BIN@"
      args: ["-test.run=^TestCLIHelperProcess$", "--", "barrier", "right", @RELEASE@]
      env: {AGENTPLANE_CLI_HELPER: "1"}
    outputs:
      stdout: {type: string}
  join:
    type: shell
    inputs:
      left_text: {type: string, required: true}
      right_text: {type: string, required: true}
    config:
      command: "@TEST_BIN@"
      args: ["-test.run=^TestCLIHelperProcess$", "--", "join"]
      env: {AGENTPLANE_CLI_HELPER: "1"}
      input_env: {left_text: LEFT_TEXT, right_text: RIGHT_TEXT}
    outputs:
      stdout: {type: string}
edges:
  - {from: left.outputs.stdout, to: join.inputs.left_text}
  - {from: right.outputs.stdout, to: join.inputs.right_text}
outputs:
  result: {from: join.outputs.stdout}
`
	path := graphFile(t, strings.ReplaceAll(definition, "@RELEASE@", strconv.Quote(release)))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "run", path, "--watch", "--max-concurrency", "2")
	var stdout bytes.Buffer
	command.Stdout = &stdout
	pipe, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Process.Kill(); _ = command.Wait() })
	ready := make(chan string, 2)
	scanned := make(chan struct{})
	var stderr strings.Builder
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			stderr.WriteString(line + "\n")
			for _, id := range []string{"left", "right"} {
				if strings.Contains(line, "["+id+" stderr] branch-ready:"+id) {
					ready <- id
				}
			}
		}
	}()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case id := <-ready:
			seen[id] = true
		case <-scanned:
			t.Fatalf("CLI exited before both child processes were ready: %v, stderr=%q", seen, stderr.String())
		case <-ctx.Done():
			t.Fatalf("both child processes did not start before release: %v", seen)
		}
	}
	if err := os.WriteFile(release, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-scanned:
	case <-ctx.Done():
		t.Fatal("CLI did not finish after releasing both child processes")
	}
	if err := command.Wait(); err != nil || !strings.Contains(stdout.String(), "[join stdout] left|right") || !strings.Contains(stderr.String(), "Run succeeded") {
		t.Fatalf("CLI run: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestBuiltBinaryInterruptCancelsRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interrupt test uses Unix process signals")
	}
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	graph := strings.Replace(oneNode, `"echo"]`, `"wait"]`, 1)
	path := graphFile(t, graph)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "run", path, "--watch")
	command.Stdout = io.Discard
	pipe, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Process.Kill(); _ = command.Wait() })
	ready := make(chan int, 1)
	scanned := make(chan struct{})
	var stderr strings.Builder
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			stderr.WriteString(scanner.Text() + "\n")
			const marker = "[work stderr] child-ready:"
			if index := strings.Index(scanner.Text(), marker); index >= 0 {
				pid, err := strconv.Atoi(strings.TrimSpace(scanner.Text()[index+len(marker):]))
				if err == nil {
					ready <- pid
					continue
				}
			}
		}
		select {
		case ready <- 0:
		default:
		}
	}()
	var childPID int
	select {
	case childPID = <-ready:
		if childPID == 0 {
			t.Fatal("watch ended before the child reported readiness")
		}
	case <-ctx.Done():
		t.Fatal("watch did not show the active child")
	}
	child, err := os.FindProcess(childPID)
	if err != nil {
		t.Fatalf("child %d was not alive before interrupt: %v", childPID, err)
	}
	childGone := false
	defer func() {
		if !childGone {
			_ = child.Kill()
		}
	}()
	if err := child.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("child %d was not alive before interrupt: %v", childPID, err)
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	<-scanned
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 130 {
		t.Fatalf("interrupt exit=%v", err)
	}
	if !strings.Contains(stderr.String(), "Run cancelled") {
		t.Fatalf("interrupt did not render terminal event: %q", stderr.String())
	}
	if err := child.Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("child %d is still alive after CLI interrupt", childPID)
	}
	childGone = true
}

func TestBuiltBinarySIGTERMCancelsRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM test uses Unix process signals")
	}
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	graph := strings.Replace(oneNode, `"echo"]`, `"wait"]`, 1)
	path := graphFile(t, graph)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "run", path, "--watch")
	command.Stdout = io.Discard
	pipe, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Process.Kill(); _ = command.Wait() })
	ready := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			const marker = "[work stderr] child-ready:"
			if index := strings.Index(scanner.Text(), marker); index >= 0 {
				pid, err := strconv.Atoi(strings.TrimSpace(scanner.Text()[index+len(marker):]))
				if err == nil {
					ready <- pid
					return
				}
			}
		}
		ready <- 0
	}()
	var childPID int
	select {
	case childPID = <-ready:
		if childPID == 0 {
			t.Fatal("watch ended before the child reported readiness")
		}
	case <-ctx.Done():
		t.Fatal("watch did not show the active child")
	}
	child, err := os.FindProcess(childPID)
	if err != nil {
		t.Fatalf("child %d was not alive before SIGTERM: %v", childPID, err)
	}
	childGone := false
	defer func() {
		if !childGone {
			_ = child.Kill()
		}
	}()
	if err := child.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("child %d was not alive before SIGTERM: %v", childPID, err)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 130 {
		t.Fatalf("SIGTERM exit=%v", err)
	}
	if err := child.Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("child %d is still alive after CLI SIGTERM", childPID)
	}
	childGone = true
}
