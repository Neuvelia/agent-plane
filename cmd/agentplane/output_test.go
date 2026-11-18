package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
)

func TestWatchDisplaysPartialLogBeforeNodeFinishes(t *testing.T) {
	var out, errOut bytes.Buffer
	renderer := newEventRenderer(&out, &errOut, true)
	renderer.Event(events.Event{Type: events.LogChunk, NodeID: "worker", Payload: events.LogChunkPayload{Stream: events.Stdout, Data: []byte("partial")}})
	if !strings.Contains(out.String(), "[worker stdout] partial") {
		t.Fatalf("partial log was buffered: %q", out.String())
	}
}

func TestWatchReplaysRetainedCancellationTailOnce(t *testing.T) {
	started := events.Event{Sequence: 1, Type: events.RunStarted, RunID: "run", Payload: events.RunStartedPayload{}}
	finished := events.Event{Sequence: 2, Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "cancelled"}}
	live := make(chan events.Event, 1)
	live <- started
	close(live)
	replay := make(chan []events.Event, 1)
	replay <- []events.Event{started, finished}
	var out, errOut bytes.Buffer
	if err := renderWatchEvents(newEventRenderer(&out, &errOut, true), live, replay, nil); err != nil {
		t.Fatal(err)
	}
	if got := errOut.String(); strings.Count(got, "Run started:") != 1 || strings.Count(got, "Run cancelled") != 1 {
		t.Fatalf("watch output = %q", got)
	}
}

func TestAwaitWatchOutputReturnsWhenCancelledAfterRunFinished(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error)
	returned := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		returned <- awaitWatchOutput(ctx, done, time.Millisecond)
	}()
	<-started
	cancel()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("awaitWatchOutput stayed blocked on an unresponsive renderer after cancellation")
	}
}

func TestAwaitWatchOutputReturnsRenderError(t *testing.T) {
	done := make(chan error, 1)
	want := errors.New("terminal gone")
	done <- want
	if err := awaitWatchOutput(context.Background(), done, time.Millisecond); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
