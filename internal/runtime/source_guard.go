package runtime

import (
	"context"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/gitstate"
)

// The source guard (ADR-0024, F3.1 requirement 19) observes the user's source
// checkout before an isolated run and compares it afterwards. A worktree is not
// a sandbox, so this detects, unattributed, an agent that wrote outside it. It
// never fails or alters the run.

// sourceGuardTimeout bounds each source inspection. gitstate.Capture hashes
// ignored files, so a large ignored tree can be slow; ADR-0016 records that.
const sourceGuardTimeout = 30 * time.Second

// sourceGuard is the source checkout observed before run_started. A failed
// capture is remembered and reported as indeterminate, never as unchanged.
type sourceGuard struct{ baseline *gitstate.Baseline }

func captureSourceGuard(ctx context.Context, root string) sourceGuard {
	bounded, cancel := context.WithTimeout(ctx, sourceGuardTimeout)
	defer cancel()
	baseline, err := gitstate.Capture(bounded, root)
	if err != nil {
		return sourceGuard{}
	}
	return sourceGuard{baseline: &baseline}
}

// reportSourceChanges compares the source with its pre-run capture and emits an
// unattributed run-level observation. It never fails or alters the run, and
// runs detached from cancellation so a cancelled run is still observed.
func (m *runMachine) reportSourceChanges(ctx context.Context) {
	if !m.isolation.active() {
		return
	}
	payload, changed := sourceObservation(ctx, m.isolation.guard.baseline, gitstate.Changed, sourceGuardTimeout)
	if changed {
		m.appendEvent(events.Event{Type: events.SourceWorkspaceChanged, Payload: payload})
	}
}

// sourceObservation inspects detached from cancellation (a cancelled run is
// still observed) but bounded by timeout. Any failure, including an invalid
// payload, degrades to indeterminate; it reports false only for an unchanged
// source.
func sourceObservation(ctx context.Context, baseline *gitstate.Baseline, inspect func(context.Context, gitstate.Baseline) ([]string, error), timeout time.Duration) (events.SourceWorkspaceChangedPayload, bool) {
	indeterminate := events.SourceWorkspaceChangedPayload{Paths: []string{}, Indeterminate: true}
	if baseline == nil {
		return indeterminate, true
	}
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	changed, err := inspect(bounded, *baseline)
	if err != nil {
		return indeterminate, true
	}
	if len(changed) == 0 {
		return events.SourceWorkspaceChangedPayload{}, false
	}
	payload := events.SourceWorkspaceChangedPayload{}
	payload.Paths, payload.Omitted = events.BoundWorkspacePaths(changed)
	if payload.Validate() != nil {
		return indeterminate, true
	}
	return payload, true
}
