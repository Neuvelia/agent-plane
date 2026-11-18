package attemptdiff

import (
	"context"
	"errors"

	"agentplane/internal/domain/events"
	"agentplane/internal/gitstate"
)

// Error is the only error type of this package. Code is a member of the closed
// diagnostic vocabulary of attempt_diff summaries. Messages never embed Git
// stderr, absolute local paths or repository text.
type Error struct {
	Code    string
	Message string
	cause   error
	limit   bool // Git output exceeded its bound
}

func (e *Error) Error() string { return "attempt diff: " + e.Message }
func (e *Error) Unwrap() error { return e.cause }

func fail(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

// DiagnosticCode maps any error from this package or from its context to the
// diagnostic code a summary event carries.
func DiagnosticCode(err error) string {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return events.DiffTimeout
	}
	return events.DiffGitFailed
}

// classifyGit turns a gitstate failure into a typed error without Git's text.
func classifyGit(err error) *Error {
	var run *gitstate.RunError
	if errors.As(err, &run) {
		switch run.Class {
		case gitstate.RunStart:
			return fail(events.DiffGitUnavailable, "git is unavailable", err)
		case gitstate.RunTimeout:
			return fail(events.DiffTimeout, "git timed out", err)
		case gitstate.RunOutputLimit:
			failed := fail(events.DiffGitFailed, "git output exceeds its limit", err)
			failed.limit = true
			return failed
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(events.DiffTimeout, "git timed out", err)
	}
	return fail(events.DiffGitFailed, "git failed", err)
}

// unavailable maps a workspace that cannot be inspected as a Git worktree (not
// a repository, no commit, not its root) to E_DIFF_GIT_UNAVAILABLE (FR18); only
// a time bound keeps its own code.
func unavailable(err error) *Error {
	if classified := classifyGit(err); classified.Code == events.DiffTimeout {
		return classified
	}
	return fail(events.DiffGitUnavailable, "workspace is not an inspectable Git worktree", err)
}
// DiagnosticCode lets a consumer that cannot import this package read the
// closed diagnostic code of a capture failure.
func (e *Error) DiagnosticCode() string { return e.Code }
