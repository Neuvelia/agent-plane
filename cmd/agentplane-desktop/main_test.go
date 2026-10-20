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
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/desktop"
	"agentplane/internal/events"
	"agentplane/internal/graph"
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
	process := exec.Command(binary, "--state-dir", filepath.Join(t.TempDir(), "state"))
	process.Stdin = &input
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	if err := process.Run(); err != nil {
		t.Fatalf("sidecar: %v: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", stderr.String())
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
		!bytes.Contains(responses[1].Result, []byte(`"runs.events"`)) {
		t.Fatalf("hello response = %#v", responses[1])
	}
	if responses[2].RequestID != "validation" || responses[2].Error == nil || responses[2].Error.Code != "not_found" {
		t.Fatalf("validation response = %#v", responses[2])
	}
	if _, err := desktop.ReadFrame(&stdout); err != io.EOF {
		t.Fatalf("stdout contains non-protocol data: %v", err)
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
