package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/testenv"
)

// shellLoopGraph builds fast + flaky(loop) -> merge. script is the flaky
// producer's sh -c body; it receives AGENTPLANE_ATTEMPT.
func shellLoopGraph(script string, maxAttempts int) string {
	quoted, _ := json.Marshal(script)
	return `schema_version: 0
id: shell-loop
inputs: {}
nodes:
  fast:
    type: shell
    config: {command: /usr/bin/printf, args: [fast-ok]}
    outputs: {stdout: {type: string, required: true}}
  flaky:
    type: shell
    config: {command: /bin/sh, args: ["-c", ` + string(quoted) + `], stdout_json: true}
    outputs: {json: {type: json, required: true}}
    loop:
      max_attempts: ` + itoa(maxAttempts) + `
      gate:
        inputs: {candidate: {type: json, required: true}}
        config: {check_id: ready, kind: json_predicate, input: candidate, field: [ready], operator: eq, expected: true}
      bindings: [{from: candidate.outputs.json, to: gate.inputs.candidate}]
  merge:
    type: shell
    config: {command: /usr/bin/printf, args: [merged]}
    inputs: {left: {type: string, required: true}, right: {type: json, required: true}}
edges:
  - {from: fast.outputs.stdout, to: merge.inputs.left}
  - {from: flaky.outputs.json, to: merge.inputs.right}
outputs: {}
`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func runShellLoop(t *testing.T, graph string) (int, struct {
	Events  []events.Event `json:"events"`
	Failure struct {
		Kind string `json:"kind"`
	} `json:"failure"`
}) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(path, []byte(graph), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), []string{"run", path, "--json", "--max-concurrency", "4"}, &out, &errout)
	var doc struct {
		Events  []events.Event `json:"events"`
		Failure struct {
			Kind string `json:"kind"`
		} `json:"failure"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("%v stdout=%s stderr=%s", err, &out, &errout)
	}
	return code, doc
}

func attemptsByNode(evs []events.Event) map[string][]string {
	got := map[string][]string{}
	for _, e := range evs {
		if e.Type == events.AttemptFinished {
			got[e.NodeID] = append(got[e.NodeID], e.Payload.(events.AttemptFinishedPayload).Outcome)
		}
	}
	return got
}

func TestShellLoopRedThenGreenRerunsOnlyTheLoopedNode(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/printf", "/bin/sh")
	code, doc := runShellLoop(t, shellLoopGraph(`if [ "$AGENTPLANE_ATTEMPT" = 1 ]; then echo '{"ready":false}'; else echo '{"ready":true}'; fi`, 3))
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	got := attemptsByNode(doc.Events)
	if want := []string{"rejected", "accepted"}; strings.Join(got["flaky"], ",") != strings.Join(want, ",") {
		t.Fatalf("flaky attempts %v", got)
	}
	if len(got["fast"]) != 0 || len(got["merge"]) != 0 {
		t.Fatalf("unlooped nodes must not have attempts: %v", got)
	}
	starts := map[string]int{}
	ids := map[events.AttemptID]bool{}
	for _, e := range doc.Events {
		if e.Type == events.NodeStarted {
			starts[e.NodeID]++
		}
		if e.Type == events.AttemptStarted {
			ids[e.AttemptID] = true
		}
	}
	if starts["fast"] != 1 || starts["merge"] != 1 || len(ids) != 2 {
		t.Fatalf("starts=%v attempt ids=%d", starts, len(ids))
	}
}

func TestShellLoopExhaustionNeedsIntervention(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/printf", "/bin/sh")
	code, doc := runShellLoop(t, shellLoopGraph(`echo '{"ready":false}'`, 2))
	if code != 4 || doc.Failure.Kind != "needs_intervention" {
		t.Fatalf("code=%d kind=%q", code, doc.Failure.Kind)
	}
	if got := attemptsByNode(doc.Events)["flaky"]; strings.Join(got, ",") != "rejected,needs_intervention" {
		t.Fatalf("%v", got)
	}
	for _, e := range doc.Events {
		if e.Type == events.NodeStarted && e.NodeID == "merge" {
			t.Fatal("dependent of an exhausted loop must not start")
		}
	}
}

func TestShellLoopNonZeroExitIsFatalNotRetried(t *testing.T) {
	code, doc := runShellLoop(t, shellLoopGraph(`exit 7`, 3))
	if code != 4 {
		t.Fatalf("code=%d", code)
	}
	n := 0
	for _, e := range doc.Events {
		if e.Type == events.AttemptStarted {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a failed process must not be retried, attempts=%d", n)
	}
	if doc.Failure.Kind == "needs_intervention" {
		t.Fatal("operational failure must not look like an exhausted budget")
	}
}

func TestShellEnvRejectsReservedAttemptVariableWithAClearMessage(t *testing.T) {
	graph := strings.Replace(shellLoopGraph(`echo '{"ready":true}'`, 3), "stdout_json: true}", "stdout_json: true, env: {AGENTPLANE_ATTEMPT: '9'}}", 1)
	path := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(path, []byte(graph), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), []string{"run", path, "--json"}, &out, &errout)
	if code == 0 || !strings.Contains(out.String(), "reserved") {
		t.Fatalf("code=%d out=%s", code, &out)
	}
}

// Downstream must see the green candidate's value and never a red one.
func TestShellLoopDownstreamReceivesTheGreenValueOnly(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/printenv", "/bin/sh")
	graph := shellLoopGraph(`if [ "$AGENTPLANE_ATTEMPT" = 1 ]; then echo '{"ready":false,"v":"RED"}'; else echo '{"ready":true,"v":"GREEN"}'; fi`, 3)
	graph = strings.Replace(graph, "outputs: {json: {type: json, required: true}}", "outputs: {json: {type: json, required: true}, stdout: {type: string, required: true}}", 1)
	graph = strings.Replace(graph, `config: {command: /usr/bin/printf, args: [merged]}
    inputs: {left: {type: string, required: true}, right: {type: json, required: true}}`, `config: {command: /usr/bin/printenv, args: [GOT], input_env: {right: GOT}}
    inputs: {left: {type: string, required: true}, right: {type: string, required: true}}`, 1)
	graph = strings.Replace(graph, "flaky.outputs.json, to: merge.inputs.right", "flaky.outputs.stdout, to: merge.inputs.right", 1)
	code, doc := runShellLoop(t, graph)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var seen strings.Builder
	for _, e := range doc.Events {
		if c, ok := e.Payload.(events.LogChunkPayload); ok && e.NodeID == "merge" {
			seen.Write(c.Data)
		}
	}
	if !strings.Contains(seen.String(), "GREEN") || strings.Contains(seen.String(), "RED") {
		t.Fatalf("merge saw %q", seen.String())
	}
}

func TestShellLoopRunThatIsAlreadyCancelledStartsNoAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.yaml")
	os.WriteFile(path, []byte(shellLoopGraph(`echo '{"ready":true}'`, 3)), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errout bytes.Buffer
	code := runCommand(ctx, []string{"run", path, "--json"}, &out, &errout)
	if code != 130 {
		t.Fatalf("code=%d out=%s", code, &out)
	}
	if strings.Contains(out.String(), `"type":"attempt_started"`) {
		t.Fatal("a cancelled run must not start an attempt")
	}
}
