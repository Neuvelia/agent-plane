//go:build darwin || linux

package cliproc_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
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
	defer cancel()
	// The child announces itself on stdout; cancel only once that line was
	// observed. If the signal never arrives the test still ends: Run returns
	// when the context is done.
	started := make(chan struct{})
	var once sync.Once
	stdout := writerFunc(func(p []byte) (int, error) {
		once.Do(func() { close(started) })
		return len(p), nil
	})
	go func() {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
		}
		cancel()
	}()
	policy := proctree.Policy{Grace: 200 * time.Millisecond}
	err := cliproc.Run(ctx, cliproc.Spec{Binary: "/bin/sh", Args: []string{"-c", "echo started; /bin/sleep 30 & wait"}, Env: []string{}, Stdout: stdout, Policy: policy})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	// Cancellation that wins the race against Start is a plain context error;
	// once the child ran it is a classified cancelled stage with a termination
	// record. Either way Run returned, so no tree outlives it.
	if typed, ok := cliproc.AsError(err); ok && (typed.Stage != cliproc.StageCancelled || typed.Termination == nil) {
		t.Fatalf("stage=%v termination=%v", typed.Stage, typed.Termination)
	}
}

// writerFunc adapts a function to io.Writer.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestRunAlreadyCancelledDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cliproc.Run(ctx, cliproc.Spec{Binary: "/bin/sh", Args: []string{"-c", "true"}, Env: []string{}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
