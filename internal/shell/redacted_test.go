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
