package agent

// maskedCause stands in for a provider cause whose text may spell a secret.
// It keeps the finite DiagnosticCode vocabulary and masked message text only.
type maskedCause struct {
	message string
	code    string
}

func (c maskedCause) Error() string { return c.message }

// DiagnosticCode keeps the safe code of the original cause chain.
func (c maskedCause) DiagnosticCode() string { return c.code }

// Redacted returns a copy for run results (F3.8, ADR-0029): the cause is
// replaced by masked text, while NodeID, AdapterID, Kind, DiagnosticCode and
// the process termination stay available. The receiver is not modified.
func (e *ExecutionError) Redacted(mask func(string) string) error {
	copied := *e
	copied.Termination = e.ProcessTermination()
	if e.Cause != nil {
		copied.Cause = maskedCause{message: mask(e.Cause.Error()), code: e.DiagnosticCode()}
	}
	return &copied
}
