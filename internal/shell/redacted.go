package shell

import "strings"

// maskedCause stands in for a cause whose text may spell a secret. It keeps
// only masked message text, so walking the chain reaches no raw evidence.
type maskedCause struct{ message string }

func (c maskedCause) Error() string { return c.message }

// Redacted returns a copy for run results (F3.8, ADR-0029): process output and
// cause text pass through mask, while NodeID, Kind, exit code, timestamps and
// termination stay typed for callers that branch on them. The receiver is not
// modified. It cannot mask a partial secret at a capture cut; callers that can
// use RedactedWithCut.
func (e *ExecutionError) Redacted(mask func(string) string) error {
	return e.RedactedWithCut(mask, mask)
}

// RedactedWithCut is Redacted that masks the retained text of a truncated
// stream with cut, which also hides a secret the capture limit cut in two
// (ADR-0039). The omission marker is appended unchanged.
func (e *ExecutionError) RedactedWithCut(mask, cut func(string) string) error {
	copied := *e
	copied.Result.Stdout = maskStream(e.Result.Stdout, e.Result.StdoutOmitted, mask, cut)
	copied.Result.Stderr = maskStream(e.Result.Stderr, e.Result.StderrOmitted, mask, cut)
	if e.Cause != nil {
		copied.Cause = maskedCause{message: mask(e.Cause.Error())}
	}
	return &copied
}

func maskStream(text string, omitted int64, mask, cut func(string) string) string {
	at := strings.LastIndex(text, omissionMarkerPrefix)
	if omitted == 0 || at < 0 {
		return mask(text)
	}
	return cut(text[:at]) + text[at:]
}
