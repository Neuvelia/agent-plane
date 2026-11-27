//go:build darwin || linux

package cliproc_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"agentplane/internal/agent/cliproc"
	"agentplane/internal/proctree"
)

func TestRunCapturesStdoutAndStdin(t *testing.T) {
	var out bytes.Buffer
	err := cliproc.Run(context.Background(), cliproc.Spec{Binary: "/bin/sh", Args: []string{"-c", "cat"}, Env: []string{}, Stdin: bytes.NewBufferString("hello"), Stdout: &out})
	if err != nil || out.String() != "hello" {
		t.Fatalf("out=%q err=%v", out.String(), err)
	}
}

func TestRunReportsStartAndExitFailures(t *testing.T) {
	err := cliproc.Run(context.Background(), cliproc.Spec{Binary: "/nonexistent/binary", Env: []string{}})
	if typed, ok := cliproc.AsError(err); !ok || typed.Stage != cliproc.StageStart || typed.Cause == nil {
		t.Fatalf("start error=%v", err)
	}
	err = cliproc.Run(context.Background(), cliproc.Spec{Binary: "/bin/sh", Args: []string{"-c", "exit 3"}, Env: []string{}})
	if _, ok := cliproc.AsError(err); ok || err == nil {
		t.Fatalf("exit error=%v", err)
	}
}

func TestRunCancellationTerminatesTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	policy := proctree.Policy{Grace: 200 * time.Millisecond}
	err := cliproc.Run(ctx, cliproc.Spec{Binary: "/bin/sh", Args: []string{"-c", "/bin/sleep 30 & wait"}, Env: []string{}, Policy: policy})
	typed, ok := cliproc.AsError(err)
	if !ok || typed.Stage != cliproc.StageCancelled || typed.Termination == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestRunAlreadyCancelledDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cliproc.Run(ctx, cliproc.Spec{Binary: "/bin/sh", Args: []string{"-c", "true"}, Env: []string{}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
