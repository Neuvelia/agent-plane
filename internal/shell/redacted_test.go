package shell

import (
	"errors"
	"strings"
	"testing"
)

func TestExecutionErrorRedactedMasksEvidenceAndKeepsTypedFields(t *testing.T) {
	mask := func(s string) string { return strings.ReplaceAll(s, "SECRET", "***") }
	original := &ExecutionError{NodeID: "n", Kind: FailureExit, Result: Result{Stdout: "a SECRET", Stderr: "b SECRET", ExitCode: 3}, Cause: errors.New("died SECRET")}
	var copied *ExecutionError
	if !errors.As(original.Redacted(mask), &copied) {
		t.Fatal("Redacted must return an *ExecutionError")
	}
	if copied.Result.Stdout != "a ***" || copied.Result.Stderr != "b ***" || copied.Error() != `shell node "n": exit: died ***` {
		t.Fatalf("copy = %#v / %q", copied, copied)
	}
	if copied.Kind != FailureExit || copied.Result.ExitCode != 3 || copied.NodeID != "n" {
		t.Fatalf("typed fields changed: %#v", copied)
	}
	if original.Result.Stdout != "a SECRET" || strings.Contains(copied.Cause.Error(), "SECRET") {
		t.Fatal("original mutated or cause still raw")
	}
}

func TestExecutionErrorRedactedWithCutMasksOnlyTruncatedRetainedText(t *testing.T) {
	mask := func(s string) string { return "mask(" + s + ")" }
	cut := func(s string) string { return "cut(" + s + ")" }
	marker := omissionMarker("stdout", 7, 4)
	original := &ExecutionError{NodeID: "n", Kind: FailureOutput, Result: Result{Stdout: "head" + marker, StdoutOmitted: 7, Stderr: "complete"}}
	var copied *ExecutionError
	if !errors.As(original.RedactedWithCut(mask, cut), &copied) {
		t.Fatal("RedactedWithCut must return an *ExecutionError")
	}
	if copied.Result.Stdout != "cut(head)"+marker || copied.Result.Stderr != "mask(complete)" || copied.Result.StdoutOmitted != 7 {
		t.Fatalf("copy = %#v", copied.Result)
	}
}

func TestExecutionErrorRedactedKeepsTheCaptureLimitCause(t *testing.T) {
	limit := &CaptureLimitError{Output: "stdout", Stream: "stdout", Limit: 1000, Omitted: 500}
	original := &ExecutionError{NodeID: "n", Kind: FailureOutput, Cause: limit}
	redacted := original.Redacted(func(string) string { return "***" })
	var kept *CaptureLimitError
	if !errors.As(redacted, &kept) || *kept != *limit || kept == limit {
		t.Fatalf("cause = %#v", kept)
	}
	var diagnostic interface{ DiagnosticCode() string }
	if !errors.As(redacted, &diagnostic) || diagnostic.DiagnosticCode() != CaptureDiagnostic {
		t.Fatal("the diagnostic code must survive redaction")
	}
}
