package cliproc

import (
	"errors"
	"io"

	"agentplane/internal/proctree"
)

// Spec describes one child process. Binary is executed directly with Args; no
// shell is involved. Env is passed to os/exec unchanged: a nil Env inherits the
// parent environment, so callers that must not leak it pass an empty non-nil
// slice.
type Spec struct {
	Binary string
	Args   []string
	Env    []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	// Policy tunes how the process tree is stopped. The zero value selects
	// proctree's defaults.
	Policy proctree.Policy
}

// Stage names where a Run failure happened, so a provider adapter can map it
// to its own diagnostic code and message.
type Stage string

const (
	// StageStart means the process could not be started.
	StageStart Stage = "start"
	// StageShutdown means the owned process tree could not be terminated.
	StageShutdown Stage = "shutdown"
	// StageCancelled means the context ended and the tree was terminated.
	StageCancelled Stage = "cancelled"
	// StageUnsupported means the platform cannot own a process tree.
	StageUnsupported Stage = "unsupported_platform"
)

// Error is a classified process failure. Cause is the retained underlying
// error. Termination is the termination record when one is known.
type Error struct {
	Stage       Stage
	Cause       error
	Termination *proctree.Outcome
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return "cliproc " + string(e.Stage)
	}
	return "cliproc " + string(e.Stage) + ": " + e.Cause.Error()
}

// Unwrap exposes Cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }

func newError(stage Stage, cause error, outcome *proctree.Outcome) error {
	return &Error{Stage: stage, Cause: cause, Termination: outcome}
}

// AsError returns the classified failure inside err, if any.
func AsError(err error) (*Error, bool) {
	var typed *Error
	ok := errors.As(err, &typed)
	return typed, ok
}
