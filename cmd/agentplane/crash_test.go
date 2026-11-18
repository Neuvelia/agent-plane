package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	agentruntime "agentplane/internal/runtime"
	"agentplane/internal/shell"
	"agentplane/internal/store"
)

// F3.2 task 5: crash recovery and persist-before-deliver, proven across real
// processes. The child is this test binary re-executed; the test kills it with
// Process.Kill (SIGKILL; TerminateProcess on Windows) after reading its own
// output, so nothing polls and nothing sleeps for synchronization.

const crashHelperEnv = "AGENTPLANE_CRASH_HELPER"

// crashGraph runs `fast` to completion in the first batch; `slow` consumes its
// output, so it starts in the second batch, prints its pid and then blocks. The
// run is therefore live, with fast's node_finished already durable, when the
// parent is killed.
const crashGraph = `schema_version: 0
id: crash-test
inputs: {}
nodes:
  fast:
    type: shell
    config:
      command: "@TEST_BIN@"
      args: ["-test.run=^TestCLIHelperProcess$", "--", "echo"]
      env: {AGENTPLANE_CLI_HELPER: "1"}
    outputs:
      stdout: {type: string}
  slow:
    type: shell
    inputs:
      upstream: {type: string, required: true}
    config:
      command: "@TEST_BIN@"
      args: ["-test.run=^TestCLIHelperProcess$", "--", "wait"]
      env: {AGENTPLANE_CLI_HELPER: "1"}
    outputs:
      stdout: {type: string}
edges:
  - {from: fast.outputs.stdout, to: slow.inputs.upstream}
outputs:
  held: {from: slow.outputs.stdout}
`

func helperArgs() []string {
	for i, arg := range os.Args {
		if arg == "--" {
			return os.Args[i+1:]
		}
	}
	return nil
}

// TestCrashHelperProcess runs the CLI with the arguments after `--` and the
// real stdout and stderr, as a separate process the test can kill.
func TestCrashHelperProcess(t *testing.T) {
	if os.Getenv(crashHelperEnv) != "cli" {
		return
	}
	os.Exit(runCommand(context.Background(), helperArgs(), os.Stdout, os.Stderr))
}

// TestCrashEventsHelperProcess composes runtime.Run with a store.Writer and
// prints every live event as one JSON line, so the test sees exactly what a
// subscriber would have been shown.
func TestCrashEventsHelperProcess(t *testing.T) {
	if os.Getenv(crashHelperEnv) != "events" {
		return
	}
	args := helperArgs()
	if len(args) != 2 {
		os.Exit(90)
	}
	text, err := os.ReadFile(args[0])
	if err != nil {
		os.Exit(91)
	}
	def, err := yamlgraph.Parse(text)
	if err != nil {
		os.Exit(92)
	}
	writer := store.Store{Dir: args[1]}.NewWriter(text)
	live := make(chan events.Event)
	go func() {
		encoder := json.NewEncoder(os.Stdout)
		for e := range live {
			if encoder.Encode(e) != nil {
				return
			}
		}
	}()
	_, _ = agentruntime.Run(context.Background(), def, nil, shell.Executor{}, agentruntime.Options{
		GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 4,
		Persist: writer.Append, LiveEvents: live,
	})
	os.Exit(0)
}

// crashChild is a helper process whose output the test reads line by line.
type crashChild struct {
	cmd   *exec.Cmd
	lines <-chan string
}

// startCrashChild re-executes this test binary as the named helper test and
// returns the child's chosen output stream as lines (closed at EOF). The child
// is killed and reaped when the test ends.
func startCrashChild(t *testing.T, helperTest, mode string, onStderr bool, args ...string) *crashChild {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^" + helperTest + "$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), crashHelperEnv+"="+mode)
	var pipe io.ReadCloser
	var err error
	if onStderr {
		pipe, err = cmd.StderrPipe()
	} else {
		pipe, err = cmd.StdoutPipe()
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 1024)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(pipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	child := &crashChild{cmd: cmd, lines: lines}
	t.Cleanup(child.kill)
	return child
}

// kill ends the child like kill -9 and waits until the operating system has
// released everything it held, including the run lock.
func (c *crashChild) kill() {
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
}

func (c *crashChild) next(t *testing.T) string {
	t.Helper()
	select {
	case line, ok := <-c.lines:
		if !ok {
			t.Fatal("the child exited before the awaited output")
		}
		return line
	case <-time.After(60 * time.Second): // a hang guard, not a synchronization
		t.Fatal("timed out waiting for the child")
		return ""
	}
}

func onlyRunID(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, entry := range entries {
		if !entry.IsDir() || !store.ValidRunID(entry.Name()) {
			continue
		}
		if id != "" {
			t.Fatal("more than one run in the store")
		}
		id = entry.Name()
	}
	if id == "" {
		t.Fatal("no run in the store")
	}
	return id
}

func listedRun(t *testing.T, dir string) store.Summary {
	t.Helper()
	code, out, errOut := cli(t, "runs", "list", "--store-dir", dir, "--json")
	if code != 0 {
		t.Fatalf("runs list: code=%d %s", code, errOut)
	}
	var doc struct {
		Runs []store.Summary `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Runs) != 1 {
		t.Fatalf("runs list output %q: %v", out, err)
	}
	return doc.Runs[0]
}

// storedLines returns the events of a run as decoded JSON objects, one per line.
func storedLines(t *testing.T, dir, id string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, id, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatal("events.jsonl must end on a newline-terminated line")
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("stored line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

var childReady = regexp.MustCompile(`child-ready:(\d+)`)

func TestKillNineAfterNodeFinishedLeavesDurablePrefix(t *testing.T) {
	dir := t.TempDir()
	child := startCrashChild(t, "TestCrashHelperProcess", "cli", true, "run", graphFile(t, crashGraph), "--store-dir", dir, "--watch")
	fastDone, orphan := false, 0
	for !fastDone || orphan == 0 {
		line := child.next(t)
		if strings.HasPrefix(line, "fast succeeded") {
			fastDone = true
		}
		if m := childReady.FindStringSubmatch(line); m != nil {
			orphan, _ = strconv.Atoi(m[1])
		}
	}
	t.Cleanup(func() {
		if p, err := os.FindProcess(orphan); err == nil {
			_ = p.Kill()
		}
	})
	id := onlyRunID(t, dir)
	if got := listedRun(t, dir); got.Status != store.StatusRunning {
		t.Fatalf("a live run in another process reads %q, want %q", got.Status, store.StatusRunning)
	}

	child.kill()

	if runtime.GOOS != "windows" {
		if p, err := os.FindProcess(orphan); err != nil || p.Signal(syscall.Signal(0)) != nil {
			t.Fatalf("the node process %d must outlive the killed CLI, or this test proves nothing about the lock", orphan)
		}
	}
	// The orphan is alive and was started by the dead writer: if it had inherited
	// the run lock descriptor the run would still read "running".
	got := listedRun(t, dir)
	if got.Status != store.StatusInterrupted || got.InterruptedAtSequence == 0 || got.InterruptedAtSequence != got.LastSequence {
		t.Fatalf("after kill -9 the run must be interrupted at its last durable sequence: %+v", got)
	}
	started, finished := map[string]bool{}, map[string]bool{}
	for _, e := range storedLines(t, dir, id) {
		node, _ := e["node_id"].(string)
		switch e["type"] {
		case string(events.NodeStarted):
			started[node] = true
		case string(events.NodeFinished):
			finished[node] = true
		case string(events.RunFinished):
			t.Fatal("no terminal event may be invented for a killed run")
		}
	}
	if !finished["fast"] || !started["slow"] || finished["slow"] {
		t.Fatalf("started=%v finished=%v", started, finished)
	}
}

func TestNoStreamedNonProgressEventIsMissingFromDiskAfterKill(t *testing.T) {
	dir := t.TempDir()
	child := startCrashChild(t, "TestCrashEventsHelperProcess", "events", false, graphFile(t, crashGraph), dir)
	var streamed []map[string]any
	for {
		var e map[string]any
		if err := json.Unmarshal([]byte(child.next(t)), &e); err != nil {
			t.Fatal(err)
		}
		streamed = append(streamed, e)
		if e["type"] == string(events.NodeFinished) && e["node_id"] == "fast" {
			break
		}
	}
	child.kill()
	for line := range child.lines { // whatever the child had already printed
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		streamed = append(streamed, e)
	}
	stored := map[float64]map[string]any{}
	for _, e := range storedLines(t, dir, onlyRunID(t, dir)) {
		stored[e["sequence"].(float64)] = e
	}
	checked := 0
	for _, e := range streamed {
		if events.IsProgress(events.Type(e["type"].(string))) {
			continue // progress and log chunks are synced later by design
		}
		checked++
		if !reflect.DeepEqual(stored[e["sequence"].(float64)], e) {
			t.Fatalf("streamed %v event %v is not on disk as shown: %v", e["type"], e["sequence"], stored[e["sequence"].(float64)])
		}
	}
	if checked < 3 {
		t.Fatalf("only %d non-progress events were streamed; the test proved too little", checked)
	}
}

func TestStoredEventsEqualLiveEventsForAFinishedRun(t *testing.T) {
	requireDemoTools(t)
	dir := t.TempDir()
	code, out, errOut := cli(t, "run", demoGraphPath(t), "--approve", "approve", "--store-dir", dir, "--json")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errOut)
	}
	var doc struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Events) == 0 {
		t.Fatalf("no events in the CLI document: %v", err)
	}
	stored := storedLines(t, dir, onlyRunID(t, dir))
	if !reflect.DeepEqual(doc.Events, stored) {
		t.Fatalf("the stored sequence (%d events) differs from the live sequence (%d events)", len(stored), len(doc.Events))
	}
}
