package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// builtCLI builds the agentplane binary once per test.
func builtCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	return binary
}

// runReason is the part of the --json run document the exit-code tests read.
type runReason struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Nodes   []struct {
		NodeID string `json:"node_id"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"nodes"`
}

func decodeRunReason(t *testing.T, stdout []byte) runReason {
	t.Helper()
	var doc runReason
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("--json output is not one document: %v %q", err, stdout)
	}
	return doc
}

func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("want a non-zero exit, got %v", err)
	}
	return exit.ExitCode()
}

// A run deadline ends the built CLI with exit 124 and reason run_timeout in
// --json and in the human output (F3.4 C criterion).
//
// roadmap: F3.4-AC2
func TestBuiltBinaryRunTimeoutExits124WithReason(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wait helper is terminated with Unix process-tree semantics")
	}
	binary := builtCLI(t)
	graph := strings.Replace(oneNode, `"echo"]`, `"wait"]`, 1)
	graph = strings.Replace(graph, "inputs: {}\n", "inputs: {}\nlimits: {run_timeout_seconds: 1}\n", 1)
	path := graphFile(t, graph)
	for _, mode := range []string{"--json", "--watch"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var stdout, stderr bytes.Buffer
			command := exec.CommandContext(ctx, binary, "run", path, mode)
			command.Stdout, command.Stderr = &stdout, &stderr
			if code := exitCodeOf(t, command.Run()); code != exitRunTimeout {
				t.Fatalf("exit %d, want %d; stderr=%q", code, exitRunTimeout, stderr.String())
			}
			if mode == "--watch" {
				if !strings.Contains(stderr.String(), "run_timeout") {
					t.Fatalf("human output lacks the reason: %q", stderr.String())
				}
				return
			}
			doc := decodeRunReason(t, stdout.Bytes())
			if doc.Status != "cancelled" || doc.Reason != "run_timeout" || len(doc.Nodes) != 1 || doc.Nodes[0].Reason != "run_timeout" {
				t.Fatalf("document = %+v", doc)
			}
		})
	}
}

// SIGINT during a --json run exits 130 with reason interrupt in the document.
//
// roadmap: F3.4-AC2
func TestBuiltBinaryInterruptReportsInterruptReasonInJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interrupt test uses Unix process signals")
	}
	binary := builtCLI(t)
	ready := filepath.Join(t.TempDir(), "ready")
	graph := strings.Replace(oneNode, `"echo"]`, `"write-wait", `+jsonString(ready)+`]`, 1)
	path := graphFile(t, graph)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, binary, "run", path, "--json")
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	awaitFile(ctx, t, ready)
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if code := exitCodeOf(t, command.Wait()); code != exitInterrupted {
		t.Fatalf("exit %d, want %d; stderr=%q", code, exitInterrupted, stderr.String())
	}
	doc := decodeRunReason(t, stdout.Bytes())
	if doc.Status != "cancelled" || doc.Reason != "interrupt" {
		t.Fatalf("document = %+v", doc)
	}
}

// awaitFile waits until the helper child has written path: the child's own
// signal that it is running, so the interrupt reaches a live node.
func awaitFile(ctx context.Context, t *testing.T, path string) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the node never started: %s missing", path)
		case <-tick.C:
		}
	}
}

// The remedy for an unavailable adapter names only steps of the entry point
// that runs the graph (F1.2-AC2).
func TestAgentRemedyMatchesTheEntryPoint(t *testing.T) {
	if got := cliAgentRemedy("codex"); !strings.Contains(got, "--codex-bin") {
		t.Fatalf("run remedy = %q", got)
	}
	if got := cliAgentRemedy("other"); got != "" {
		t.Fatalf("run remedy for an adapter without flags = %q", got)
	}
	if got := serveAgentRemedy("other"); strings.Contains(got, "--codex-bin") || !strings.Contains(got, "desktop engine") {
		t.Fatalf("serve remedy = %q", got)
	}
	// Once --adapter codex registered the adapter, the remedy must not tell the
	// user to pass the flags they already passed.
	if got := registeredAgentRemedy("codex"); strings.Contains(got, "--adapter") || !strings.Contains(got, "installed and runs") {
		t.Fatalf("registered codex remedy = %q", got)
	}
}
