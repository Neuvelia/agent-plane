package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

// AttemptDiffer opens the per-run capture session behind opt-in attempt_diff
// events (F2.8b, ADR-0026). It reports facts about the workspace; the runtime
// owns when to snapshot, the run budget and the event order, and capture never
// changes an execution outcome.
type AttemptDiffer interface {
	Open(ctx context.Context) (DiffSession, error)
}

// DiffSnapshot is an opaque workspace observation produced and consumed by the
// same DiffSession.
type DiffSnapshot interface{}

// DiffRedactor masks configured secrets in bytes handed to a session.
type DiffRedactor interface {
	Redact(text []byte) (redacted []byte, changed bool)
}

// AttemptCapture is what one attempt changed: file events in path order and
// the summary, whose run-budget totals the coordinator may still adjust.
type AttemptCapture struct {
	Files   []events.AttemptDiffPayload
	Summary events.AttemptDiffPayload
}

// DiffSession holds one run's capture state (scratch copies) until Close.
type DiffSession interface {
	Snapshot(ctx context.Context, workspace string) (DiffSnapshot, error)
	Diff(ctx context.Context, before, after DiffSnapshot, redactor DiffRedactor) (AttemptCapture, error)
	Close() error
}

// diffRecorder is coordinator-owned like valueRecorder: nil when capture is
// off, so a run without the option does no Git or filesystem work at all.
type diffRecorder struct {
	session  DiffSession
	redactor diffRedactor
	budget   int // remaining patch bytes of the run, mutated only by the coordinator
}

// diffRedactor adapts the run's log redactor. Each call uses a fresh clone, so
// concurrent workers share only the immutable secret list.
type diffRedactor struct{ template *logRedactor }

func (d diffRedactor) Redact(text []byte) ([]byte, bool) {
	c := d.template.clone()
	out := append([]byte{}, c.Write(events.Stdout, text)...)
	out = append(out, c.Flush(events.Stdout)...)
	return out, !bytes.Equal(out, text)
}

// attemptDiffResult is a worker's capture of one attempt. after is carried to
// the next attempt as its before snapshot.
type attemptDiffResult struct {
	after   DiffSnapshot
	capture AttemptCapture
	code    string
}

const codeAttemptDiffPreflight = "E_ATTEMPT_DIFF_PREFLIGHT"

// openAttemptDiffs runs last in preflight so a rejected run leaves no scratch.
func openAttemptDiffs(ctx context.Context, d *graph.Definition, options Options, redactions []string, template *logRedactor) (*diffRecorder, error) {
	if options.AttemptDiffs == nil {
		return nil, nil
	}
	fail := func(err error) error {
		return &run.PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{Severity: graph.SeverityError, Code: codeAttemptDiffPreflight, GraphID: d.ID, Message: err.Error(), Suggestion: "use UTF-8 log redactions and a usable capture session"}}}}
	}
	if err := preflightUTF8Redactions("attempt-diff", redactions, template); err != nil {
		return nil, fail(err)
	}
	session, err := options.AttemptDiffs.Open(ctx)
	if err != nil {
		return nil, fail(fmt.Errorf("attempt-diff capture could not start: %w", err))
	}
	return &diffRecorder{session: session, redactor: diffRedactor{template}, budget: events.MaxRunDiffBytes}, nil
}

func (r *diffRecorder) close() {
	if r != nil {
		_ = r.session.Close()
	}
}

func (r *diffRecorder) captures(n graph.NodeDefinition) bool {
	return r != nil && n.Type == nodeTypeAgent && n.Loop != nil
}

// produceWithDiff runs the Agent producer between a before and an after
// snapshot. Capture is observation only: every failure becomes a summary-only
// result and never touches the producer's outcome (ADR-0026 decision 6).
// carried is the previous attempt's after snapshot, nil on the first attempt.
func (m *runMachine) produceWithDiff(ctx context.Context, n graph.NodeDefinition, carried DiffSnapshot, produce func() completedNode) completedNode {
	if !m.diffs.captures(n) {
		return produce()
	}
	workspace, _ := n.Config["workspace"].(string)
	before, beforeErr := carried, error(nil)
	if before == nil {
		before, beforeErr = m.diffs.session.Snapshot(ctx, workspace)
	}
	work := produce()
	if work.err != nil || ctx.Err() != nil {
		return work
	}
	work.diff = m.diffs.capture(ctx, workspace, before, beforeErr)
	return work
}

func (r *diffRecorder) capture(ctx context.Context, workspace string, before DiffSnapshot, beforeErr error) *attemptDiffResult {
	if beforeErr != nil {
		return &attemptDiffResult{code: diagnosticOf(beforeErr)}
	}
	after, err := r.session.Snapshot(ctx, workspace)
	if err != nil {
		return &attemptDiffResult{code: diagnosticOf(err)}
	}
	capture, err := r.session.Diff(ctx, before, after, r.redactor)
	if err != nil {
		return &attemptDiffResult{after: after, code: diagnosticOf(err)}
	}
	return &attemptDiffResult{after: after, capture: capture}
}

// diagnosticOf maps a session error to the closed diagnostic vocabulary
// through an optional DiagnosticCode method.
func diagnosticOf(err error) string {
	var coded interface{ DiagnosticCode() string }
	if errors.As(err, &coded) {
		if code := coded.DiagnosticCode(); code != "" {
			return code
		}
	}
	return events.DiffGitFailed
}

// emitAttemptDiff runs on the coordinator after node_evidence and before
// attempt_finished. It applies the run budget, then emits file events in path
// order and the summary last (ADR-0026).
func (m *runMachine) emitAttemptDiff(id string, attempt events.AttemptID, result *attemptDiffResult) {
	if result == nil || m.diffs == nil {
		return
	}
	node := m.nodes[id]
	files, summary := m.diffs.fit(result)
	emit := func(p events.AttemptDiffPayload) {
		m.appendEvent(events.Event{Type: events.AttemptDiff, NodeID: id, NodeRunID: node.NodeRunID, AttemptID: attempt, Payload: p})
	}
	for _, f := range files {
		emit(f)
	}
	emit(summary)
}

// fit charges patch bytes to the run budget; a patch that does not fit becomes
// omitted_reason budget with its line counts kept. Invalid adapter output is
// replaced by a summary-only result rather than panicking the run.
func (r *diffRecorder) fit(result *attemptDiffResult) ([]events.AttemptDiffPayload, events.AttemptDiffPayload) {
	summary := result.capture.Summary
	if result.code != "" {
		summary = events.AttemptDiffPayload{DiagnosticCode: result.code, OmittedReason: events.DiffOmitUnreadable}
	}
	files := make([]events.AttemptDiffPayload, 0, len(result.capture.Files))
	if result.code == "" {
		for _, f := range result.capture.Files {
			if len(f.Patch) > r.budget {
				summary.PatchBytes -= len(f.Patch)
				f.Patch, f.Truncated, f.OmittedReason = "", false, events.DiffOmitBudget
			}
			r.budget -= len(f.Patch)
			files = append(files, f)
		}
	}
	if !validAttemptDiff(files, summary) {
		return nil, events.AttemptDiffPayload{DiagnosticCode: events.DiffGitFailed, OmittedReason: events.DiffOmitUnreadable}
	}
	return files, summary
}

func validAttemptDiff(files []events.AttemptDiffPayload, summary events.AttemptDiffPayload) bool {
	if summary.Path != "" || summary.Validate() != nil {
		return false
	}
	for _, f := range files {
		if f.Path == "" || f.Validate() != nil {
			return false
		}
	}
	return true
}
