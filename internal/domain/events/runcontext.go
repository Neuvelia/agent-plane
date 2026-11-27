package events

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var repoIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// SchemaRunContext is the first event schema (15, ADR-0044) that carries:
//   - the optional provenance fields of run_started (repo_name, repo_id, repo_branch,
//     repo_commit, runtime_version; F2.9, F3.6);
//   - the gate_check kind test_command (F1.4b, ADR-0042);
//   - executor_abandoned (F3.4, ADR-0044).
//
// An older schema never carries them, so a reader of schema 14 rejects a newer
// log with "unsupported event schema version" instead of an unknown field.
const SchemaRunContext = 15

// ExecutorAbandoned records that the scheduler stopped waiting for an executor
// that did not return within the abandon bound after a timeout cancellation.
// Its goroutine, and any process it owns, may still be running after the run
// ends; the event makes that visible instead of a silent clean cancel.
const ExecutorAbandoned Type = "executor_abandoned"

// MaxRunContextText bounds each run_started provenance field in bytes.
const MaxRunContextText = 256

// ExecutorAbandonedPayload names the cancel reason that the executor ignored
// and the bound the scheduler waited. It holds no paths or command lines.
type ExecutorAbandonedPayload struct {
	Reason  string `json:"reason"`
	BoundMS int64  `json:"bound_ms"`
}

func (ExecutorAbandonedPayload) eventPayload() {}

func requireRunContextSchema(e Event, what string) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < SchemaRunContext {
		return fmt.Errorf("%s requires schema %d", what, SchemaRunContext)
	}
	return nil
}

// validateRunStarted rejects provenance fields below schema 15 and bounds them.
// repo_name is a base name, never a path.
func validateRunStarted(e Event, p RunStartedPayload) error {
	fields := []string{p.RepoName, p.RepoID, p.RepoBranch, p.RepoCommit, p.RuntimeVersion}
	if strings.Join(fields, "") == "" {
		return nil
	}
	if err := requireRunContextSchema(e, "run_started provenance"); err != nil {
		return err
	}
	for _, text := range fields {
		if len(text) > MaxRunContextText || !utf8.ValidString(text) || strings.ContainsAny(text, "\x00\n") {
			return errors.New("invalid run_started provenance field")
		}
	}
	if strings.ContainsAny(p.RepoName, `/\`) {
		return errors.New("run_started repo_name must be a base name")
	}
	if p.RepoID != "" && !repoIDPattern.MatchString(p.RepoID) {
		return errors.New("run_started repo_id must be 16 lowercase hex digits")
	}
	return nil
}

func validateExecutorAbandoned(e Event, p ExecutorAbandonedPayload) error {
	if err := requireRunContextSchema(e, string(ExecutorAbandoned)); err != nil {
		return err
	}
	if e.NodeID == "" || e.NodeRunID == "" {
		return errors.New("executor_abandoned requires node and node-run IDs")
	}
	if p.Reason != ReasonNodeTimeout && p.Reason != ReasonRunTimeout {
		return fmt.Errorf("invalid executor_abandoned reason %q", p.Reason)
	}
	if p.BoundMS < 0 || p.BoundMS > MaxProcessMS {
		return errors.New("executor_abandoned bound_ms out of range")
	}
	return nil
}
