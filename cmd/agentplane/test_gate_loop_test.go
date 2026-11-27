package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
)

// TestGateLoopHelper is the child process of the test_command loop tests
// (ADR-0042). produce writes a state file that is wrong on attempt 1; check
// exits non-zero unless the state file says "good".
func TestGateLoopHelper(_ *testing.T) {
	if os.Getenv("AGENTPLANE_GATE_LOOP_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) != 2 {
		os.Exit(90)
	}
	switch args[0] {
	case "produce":
		state := "bad"
		if os.Getenv("AGENTPLANE_ATTEMPT") != "1" {
			state = "good"
		}
		if os.WriteFile(args[1], []byte(state), 0600) != nil {
			os.Exit(91)
		}
		_, _ = os.Stdout.WriteString(`{"done":true}`)
	case "check":
		data, err := os.ReadFile(args[1])
		if err != nil {
			os.Exit(92)
		}
		if string(data) != "good" {
			_, _ = os.Stderr.WriteString("assertion failed: state is " + string(data))
			os.Exit(3)
		}
		_, _ = os.Stdout.WriteString("1 passed")
	case "crash":
		os.Exit(7)
	case "none":
		// the downstream node only has to run
	default:
		os.Exit(93)
	}
	os.Exit(0)
}

// testGateLoopGraph builds producer(loop) -> sink. The loop's embedded Gate is a
// test_command running checkCommand; the producer runs produceMode.
func testGateLoopGraph(t *testing.T, produceMode, checkCommand string, maxAttempts int) string {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	q := strconv.Quote
	helperArgs := func(mode string) string {
		return `[` + q("-test.run=^TestGateLoopHelper$") + `, "--", ` + q(mode) + `, ` + q(state) + `]`
	}
	return `schema_version: 0
id: test-gate-loop
inputs: {}
nodes:
  producer:
    type: shell
    config:
      command: ` + q(os.Args[0]) + `
      args: ` + helperArgs(produceMode) + `
      env: {AGENTPLANE_GATE_LOOP_HELPER: "1"}
      stdout_json: true
    outputs: {json: {type: json, required: true}}
    loop:
      max_attempts: ` + strconv.Itoa(maxAttempts) + `
      gate:
        inputs: {candidate: {type: json, required: true}}
        config:
          check_id: unit-tests
          kind: test_command
          command: ` + q(checkCommand) + `
          args: ` + helperArgs("check") + `
          cwd: ` + q(dir) + `
          env: {AGENTPLANE_GATE_LOOP_HELPER: "1"}
          side_effects: workspace_writes
          input: candidate
      bindings: [{from: candidate.outputs.json, to: gate.inputs.candidate}]
  sink:
    type: shell
    config: {command: ` + q(os.Args[0]) + `, args: ["-test.run=^TestGateLoopHelper$", "--", "none", "none"], env: {AGENTPLANE_GATE_LOOP_HELPER: "1"}}
    inputs: {accepted: {type: json, required: true}}
edges:
  - {from: producer.outputs.json, to: sink.inputs.accepted}
outputs: {}
`
}

func gateChecks(doc []events.Event) []events.GateCheckPayload {
	var out []events.GateCheckPayload
	for _, e := range doc {
		if e.Type == events.GateCheck {
			out = append(out, e.Payload.(events.GateCheckPayload))
		}
	}
	return out
}

// roadmap: F1.4-AC1, F1.4b-AC3
func TestTestCommandLoopFailingTestIsRedThenCorrectedToGreen(t *testing.T) {
	code, doc := runShellLoop(t, testGateLoopGraph(t, "produce", os.Args[0], 3))
	if code != 0 {
		t.Fatalf("code=%d failure=%+v", code, doc.Failure)
	}
	if got := strings.Join(attemptsByNode(doc.Events)["producer"], ","); got != "rejected,accepted" {
		t.Fatalf("producer attempts = %s", got)
	}
	checks := gateChecks(doc.Events)
	if len(checks) != 2 {
		t.Fatalf("checks = %d", len(checks))
	}
	red, green := checks[0], checks[1]
	if red.Kind != events.CheckTestCommand || red.Verdict != events.CheckRed || *red.Evidence[0].ExitCode != 3 || !strings.Contains(string(red.Evidence[0].Stderr), "assertion failed") {
		t.Fatalf("red check = %+v", red)
	}
	if !strings.Contains(red.Reason, "side_effects: workspace_writes") {
		t.Fatalf("the declared side-effect policy must appear in the trace: %q", red.Reason)
	}
	if green.Verdict != events.CheckGreen || *green.Evidence[0].ExitCode != 0 || string(green.Evidence[0].Stdout) != "1 passed" {
		t.Fatalf("green check = %+v", green)
	}
}

// roadmap: F1.4b-AC3
func TestTestCommandLoopExhaustionNeedsIntervention(t *testing.T) {
	// A budget of one attempt leaves no room to correct the failing first state.
	code, doc := runShellLoop(t, testGateLoopGraph(t, "produce", os.Args[0], 1))
	if code != 4 || doc.Failure.Kind != "needs_intervention" {
		t.Fatalf("code=%d kind=%q", code, doc.Failure.Kind)
	}
	for _, e := range doc.Events {
		if e.Type == events.NodeStarted && e.NodeID == "sink" {
			t.Fatal("a dependent of an exhausted loop must not start")
		}
	}
}

// roadmap: F1.4b-AC4
func TestTestCommandLoopProducerNonZeroExitStaysFatal(t *testing.T) {
	code, doc := runShellLoop(t, testGateLoopGraph(t, "crash", os.Args[0], 3))
	if code != 4 || doc.Failure.Kind == "needs_intervention" {
		t.Fatalf("code=%d kind=%q", code, doc.Failure.Kind)
	}
	if n := len(attemptsByNode(doc.Events)["producer"]); n > 1 {
		t.Fatalf("a failed producer must not be retried, attempts=%d", n)
	}
	if len(gateChecks(doc.Events)) != 0 {
		t.Fatal("the check must not run for a failed producer")
	}
}

// roadmap: F1.4b-AC5
func TestTestCommandThatCannotStartIsAnErrorNotARedVerdict(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-runner")
	code, doc := runShellLoop(t, testGateLoopGraph(t, "produce", missing, 3))
	if code != 4 || doc.Failure.Kind == "needs_intervention" {
		t.Fatalf("code=%d kind=%q", code, doc.Failure.Kind)
	}
	if len(gateChecks(doc.Events)) != 0 {
		t.Fatal("a runner that cannot start must not produce a verdict")
	}
}

// roadmap: F1.4b-AC7
func TestStandaloneTestCommandGateIsRejected(t *testing.T) {
	graph := `schema_version: 0
id: standalone
inputs: {}
nodes:
  check:
    type: gate
    inputs: {data: {type: string, required: true}}
    config: {check_id: tests, kind: test_command, command: go, args: [test], cwd: ` + strconv.Quote(t.TempDir()) + `, side_effects: none, input: data}
edges: []
outputs: {}
`
	code, out, errOut := invoke(t, context.Background(), "graph", "validate", graphFile(t, graph))
	if code == 0 || !strings.Contains(out+errOut, "valid only as a loop's embedded Gate") {
		t.Fatalf("code=%d out=%s err=%s", code, out, errOut)
	}
}
