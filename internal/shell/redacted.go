package shell

// maskedCause stands in for a cause whose text may spell a secret. It keeps
// only masked message text, so walking the chain reaches no raw evidence.
type maskedCause struct{ message string }

func (c maskedCause) Error() string { return c.message }

// Redacted returns a copy for run results (F3.8, ADR-0029): process output and
// cause text pass through mask, while NodeID, Kind, exit code, timestamps and
// termination stay typed for callers that branch on them. The receiver is not
// modified.
func (e *ExecutionError) Redacted(mask func(string) string) error {
	copied := *e
	copied.Result.Stdout = mask(e.Result.Stdout)
	copied.Result.Stderr = mask(e.Result.Stderr)
	if e.Cause != nil {
		copied.Cause = maskedCause{message: mask(e.Cause.Error())}
	}
	return &copied
}
