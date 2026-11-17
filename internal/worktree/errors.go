package worktree

import (
	"errors"

	"agentplane/internal/gitstate"
)

// Code classifies a worktree failure for callers and trace consumers. Messages
// are written for the person who must fix the situation and never embed raw Git
// diagnostics, which are repository-controlled text.
type Code string

// Failure codes of this package.
const (
	CodeWorkspace    Code = "workspace"     // source is not a usable non-bare Git worktree root
	CodeGitVersion   Code = "git_version"   // Git missing or older than 2.38
	CodeDirtyTracked Code = "dirty_tracked" // tracked files have uncommitted changes
	CodeSubmodule    Code = "submodule"     // source contains a gitlink
	CodeFilterDriver Code = "filter_driver" // repository configures a Git filter driver
	CodeWorktreeRoot Code = "worktree_root" // unsafe or unusable worktree root
	CodeCreate       Code = "worktree_create"
	CodeSnapshot     Code = "snapshot"
	CodeNestedRepo   Code = "nested_repository" // a unit's worktree contains a nested repository
	CodeIntegrate    Code = "integrate"
	CodeRemove       Code = "worktree_remove"
	CodeInvalidInput Code = "invalid_input"
)

// Error is the error type of this package.
type Error struct {
	Code    Code
	Message string
	cause   error
}

func (e *Error) Error() string { return "worktree: " + e.Message }
func (e *Error) Unwrap() error { return e.cause }

func fail(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

func asRunError(err error, target **gitstate.RunError) bool { return errors.As(err, target) }

func asWorktreeError(err error, target **Error) bool { return errors.As(err, target) }
