package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentplane/internal/gitstate"
)

func TestSourceObservationIsDetachedFromCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inspect := func(c context.Context, _ gitstate.Baseline) ([]string, error) {
		if c.Err() != nil {
			return nil, c.Err()
		}
		return []string{"leak.txt"}, nil
	}
	payload, emit := sourceObservation(ctx, &gitstate.Baseline{}, inspect, time.Minute)
	if !emit || payload.Indeterminate || len(payload.Paths) != 1 || payload.Paths[0] != "leak.txt" {
		t.Fatalf("a cancelled run must still be observed: %+v emit=%t", payload, emit)
	}
}

func TestSourceObservationTimeoutAndErrorAreIndeterminate(t *testing.T) {
	blocking := func(c context.Context, _ gitstate.Baseline) ([]string, error) {
		<-c.Done()
		return nil, c.Err()
	}
	failing := func(context.Context, gitstate.Baseline) ([]string, error) { return nil, errors.New("boom") }
	for name, inspect := range map[string]func(context.Context, gitstate.Baseline) ([]string, error){"timeout": blocking, "error": failing} {
		payload, emit := sourceObservation(context.Background(), &gitstate.Baseline{}, inspect, time.Millisecond)
		if !emit || !payload.Indeterminate || len(payload.Paths) != 0 || payload.Validate() != nil {
			t.Fatalf("%s: %+v emit=%t", name, payload, emit)
		}
	}
	if payload, emit := sourceObservation(context.Background(), nil, failing, time.Second); !emit || !payload.Indeterminate {
		t.Fatalf("a missing baseline is indeterminate: %+v", payload)
	}
	if _, emit := sourceObservation(context.Background(), &gitstate.Baseline{}, func(context.Context, gitstate.Baseline) ([]string, error) { return nil, nil }, time.Second); emit {
		t.Fatal("an unchanged source emits nothing")
	}
}
