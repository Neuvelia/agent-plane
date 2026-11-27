package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
)

// TestTestCommandHelperProcess is the child process of the test_command tests.
func TestTestCommandHelperProcess(_ *testing.T) {
	if os.Getenv("AGENTPLANE_TEST_COMMAND_HELPER") != "1" {
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
	case "pass":
		fmt.Print("3 passed")
	case "fail":
		fmt.Print("1 failed")
		fmt.Fprint(os.Stderr, "assertion failed")
		os.Exit(1)
	case "exit":
		os.Exit(int(args[1][0] - '0'))
	case "bulk":
		fmt.Print(strings.Repeat("o", 3*events.MaxGateStream))
		fmt.Fprint(os.Stderr, strings.Repeat("e", 3*events.MaxGateStream))
		os.Exit(2)
	case "env":
		fmt.Printf("%s|%s|%s", os.Getenv("ALLOWED"), os.Getenv("EXPLICIT"), os.Getenv("NOT_ALLOWED"))
	case "hang":
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(0)
}

func testCommandNode(t *testing.T, mode string, more ...string) graph.NodeDefinition {
	t.Helper()
	args := append([]string{"-test.run=^TestTestCommandHelperProcess$", "--", mode}, more...)
	n := node(KindTestCommand)
	n.Config["command"] = os.Args[0]
	n.Config["args"] = args
	n.Config["cwd"] = t.TempDir()
	n.Config["env"] = map[string]string{"AGENTPLANE_TEST_COMMAND_HELPER": "1"}
	n.Config["side_effects"] = SideEffectsWorkspace
	n.Config["input"] = "candidate"
	n.Inputs["candidate"] = required(graph.ValueTypeString)
	return n
}

func runTestCommand(t *testing.T, e Executor, n graph.NodeDefinition, o runtime.ExecutionObservation) (events.GateCheckPayload, error) {
	t.Helper()
	var got *events.GateCheckPayload
	o.Check = func(p events.GateCheckPayload) { got = &p }
	_, err := runCheck(n, e, o)
	if got == nil {
		return events.GateCheckPayload{}, err
	}
	return *got, err
}

func runCheck(n graph.NodeDefinition, e Executor, o runtime.ExecutionObservation) (map[string]graph.Value, error) {
	out, _, err := e.ExecuteNodeObserved(context.Background(), "gate", n, map[string]graph.Value{"candidate": graph.NewStringValue("x")}, o)
	return out, err
}

// roadmap: F1.4b-AC1
func TestTestCommandExitZeroIsGreenWithEvidence(t *testing.T) {
	p, err := runTestCommand(t, Executor{}, testCommandNode(t, "pass"), loopGate)
	if err != nil || p.Verdict != events.CheckGreen || p.Kind != events.CheckTestCommand {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	e := p.Evidence[0]
	if *e.ExitCode != 0 || string(e.Stdout) != "3 passed" || e.Source != "test_command" {
		t.Fatalf("evidence=%+v", e)
	}
	if strings.Join(p.Scope.CheckedInputs, ",") != "candidate" {
		t.Fatalf("scope=%v", p.Scope.CheckedInputs)
	}
}

// roadmap: F1.4-AC1, F1.4b-AC1, F1.4b-AC2
func TestTestCommandNonZeroExitIsRedAndBlocksWithBoundedEvidence(t *testing.T) {
	for _, code := range []string{"1", "2", "9"} {
		p, err := runTestCommand(t, Executor{}, testCommandNode(t, "exit", code), loopGate)
		var red RedError
		if !errors.As(err, &red) || p.Verdict != events.CheckRed || *p.Evidence[0].ExitCode != int(code[0]-'0') {
			t.Fatalf("exit %s: p=%+v err=%v", code, p, err)
		}
	}
	p, err := runTestCommand(t, Executor{}, testCommandNode(t, "bulk"), loopGate)
	if err == nil || p.Verdict != events.CheckRed {
		t.Fatalf("p=%+v err=%v", p.Verdict, err)
	}
	e := p.Evidence[0]
	if len(e.Stdout) != events.MaxGateStream || len(e.Stderr) != events.MaxGateStream || e.StdoutOmitted != 2*events.MaxGateStream || e.StderrOmitted != 2*events.MaxGateStream {
		t.Fatalf("evidence must be bounded with omitted counts: %d/%d omitted %d/%d", len(e.Stdout), len(e.Stderr), e.StdoutOmitted, e.StderrOmitted)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("bounded result must be a valid Check Result: %v", err)
	}
}

// roadmap: F1.4b-AC6
func TestTestCommandRecordsTheDeclaredSideEffectPolicyInTheReason(t *testing.T) {
	n := testCommandNode(t, "fail")
	n.Config["side_effects"] = SideEffectsNone
	p, _ := runTestCommand(t, Executor{}, n, loopGate)
	if p.Reason != "command exited 1; declared side_effects: none" {
		t.Fatalf("reason=%q", p.Reason)
	}
}

// roadmap: F1.4b-AC8
func TestTestCommandEnvironmentIsExplicit(t *testing.T) {
	t.Setenv("ALLOWED", "yes")
	t.Setenv("NOT_ALLOWED", "leak")
	n := testCommandNode(t, "env")
	n.Config["env_allowlist"] = []string{"ALLOWED"}
	n.Config["env"] = map[string]string{"AGENTPLANE_TEST_COMMAND_HELPER": "1", "EXPLICIT": "set"}
	p, err := runTestCommand(t, Executor{}, n, loopGate)
	if err != nil || string(p.Evidence[0].Stdout) != "yes|set|" {
		t.Fatalf("stdout=%q err=%v", p.Evidence[0].Stdout, err)
	}
}

// roadmap: F1.4b-AC5
func TestTestCommandTimeoutAndStartFailureAreErrorsNotVerdicts(t *testing.T) {
	p, err := runTestCommand(t, Executor{TestBound: 300 * time.Millisecond}, testCommandNode(t, "hang"), loopGate)
	if !errors.Is(err, context.DeadlineExceeded) || p.Verdict != "" {
		t.Fatalf("timeout: p=%+v err=%v", p, err)
	}
	missing := testCommandNode(t, "pass")
	missing.Config["command"] = missing.Config["cwd"].(string) + "/no-such-runner"
	p, err = runTestCommand(t, Executor{}, missing, loopGate)
	var red RedError
	if err == nil || errors.As(err, &red) || p.Verdict != "" {
		t.Fatalf("start failure must not be a verdict: p=%+v err=%v", p, err)
	}
	gone := testCommandNode(t, "pass")
	gone.Config["cwd"] = gone.Config["cwd"].(string) + "/missing-dir"
	if _, err = runTestCommand(t, Executor{}, gone, loopGate); err == nil || errors.As(err, &red) {
		t.Fatalf("a missing cwd is an operational error: %v", err)
	}
}

// roadmap: F1.4b-AC5
func TestTestCommandCancellationIsNotAVerdict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := Executor{}.ExecuteNodeObserved(ctx, "gate", testCommandNode(t, "hang"), map[string]graph.Value{"candidate": graph.NewStringValue("x")}, loopGate)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond) // let the child start; the assertion below does not depend on timing
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

// roadmap: F1.4b-AC6
func TestTestCommandConfigurationIsRejectedWhenUnsafeOrIncomplete(t *testing.T) {
	cases := map[string]func(n *graph.NodeDefinition){
		"no side_effects declaration": func(n *graph.NodeDefinition) { delete(n.Config, "side_effects") },
		"unknown side_effects":        func(n *graph.NodeDefinition) { n.Config["side_effects"] = "read_only" },
		"empty command":               func(n *graph.NodeDefinition) { n.Config["command"] = "" },
		"relative cwd":                func(n *graph.NodeDefinition) { n.Config["cwd"] = "work" },
		"missing cwd":                 func(n *graph.NodeDefinition) { delete(n.Config, "cwd") },
		"reserved env":                func(n *graph.NodeDefinition) { n.Config["env"] = map[string]string{"agentplane_attempt": "1"} },
		"reserved allowlist":          func(n *graph.NodeDefinition) { n.Config["env_allowlist"] = []string{"AGENTPLANE_ATTEMPT"} },
		"input_env mapping":           func(n *graph.NodeDefinition) { n.Config["input_env"] = map[string]string{"candidate": "X"} },
		"optional input": func(n *graph.NodeDefinition) {
			n.Inputs["candidate"] = graph.PortDefinition{Type: graph.ValueTypeString}
		},
		"undeclared input": func(n *graph.NodeDefinition) { n.Config["input"] = "other" },
		"diff-only field":  func(n *graph.NodeDefinition) { n.Config["left_input"] = "candidate" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			n := testCommandNode(t, "pass")
			mutate(&n)
			if err := (Executor{}).ValidateNodeConfig("gate", n); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
	if err := (Executor{}).ValidateNodeConfig("gate", testCommandNode(t, "pass")); err != nil {
		t.Fatal(err)
	}
}

// loopGate is the observation the runtime passes to a loop's embedded Gate.
var loopGate = runtime.ExecutionObservation{LoopGate: true}

// roadmap: F1.4b-AC7
func TestTestCommandRefusesToRunAsAStandaloneGate(t *testing.T) {
	marker := t.TempDir() + "/ran"
	n := testCommandNode(t, "pass")
	n.Config["command"] = "/bin/sh"
	n.Config["args"] = []string{"-c", "touch " + marker}
	_, err := runTestCommand(t, Executor{}, n, runtime.ExecutionObservation{})
	if !errors.Is(err, ErrTestCommandStandalone) {
		t.Fatalf("err = %v, want ErrTestCommandStandalone", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the command ran: stat %v", statErr)
	}
}
