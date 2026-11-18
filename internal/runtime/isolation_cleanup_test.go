package runtime_test

import (
	"context"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

func TestIsolationHungCleanupIsBoundedAndReported(t *testing.T) {
	w := &fakeWorkspaces{root: t.TempDir(), removeBlocks: true}
	options := isolationOptions(w)
	options.CleanupTimeout = 20 * time.Millisecond
	result, err := runtime.Run(context.Background(), isolatedLoop(t.TempDir()), nil, compose(okAdapter()), options)
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	released := eventsOf(result, events.WorkspaceReleased)
	if len(released) != 1 {
		t.Fatalf("%d released events", len(released))
	}
	p := released[0].Payload.(events.WorkspaceReleasedPayload)
	if p.Reason != events.ReleaseCleanupError || p.CleanupError != "remove_timeout" {
		t.Fatalf("payload %+v", p)
	}
}
