package agent

import "fmt"

type ErrorKind string

const (
	ErrorUnavailable  ErrorKind = "unavailable"
	ErrorDetection    ErrorKind = "detection"
	ErrorInvalidInput ErrorKind = "invalid_input"
	ErrorStart        ErrorKind = "start"
	ErrorExecution    ErrorKind = "execution"
	ErrorUnsupported  ErrorKind = "unsupported_capability"
)

// ExecutionError retains a useful category and original cause without
// placing provider details in runtime lifecycle events.
type ExecutionError struct {
	NodeID    string
	AdapterID AdapterID
	Kind      ErrorKind
	Cause     error
}

func (e *ExecutionError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("agent node %q adapter %q: %s", e.NodeID, e.AdapterID, e.Kind)
	}
	return fmt.Sprintf("agent node %q adapter %q: %s: %v", e.NodeID, e.AdapterID, e.Kind, e.Cause)
}
func (e *ExecutionError) Unwrap() error { return e.Cause }
