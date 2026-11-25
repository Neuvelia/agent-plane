package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestExecutionErrorRedactedMasksCauseAndKeepsDiagnosticCode(t *testing.T) {
	mask := func(s string) string { return strings.ReplaceAll(s, "SECRET", "***") }
	original := &ExecutionError{NodeID: "n", AdapterID: "a", Kind: ErrorStart, Cause: errors.New("boom SECRET")}
	var copied *ExecutionError
	if !errors.As(original.Redacted(mask), &copied) {
		t.Fatal("Redacted must return an *ExecutionError")
	}
	if strings.Contains(copied.Error(), "SECRET") || copied.Kind != ErrorStart || copied.NodeID != "n" || copied.DiagnosticCode() != original.DiagnosticCode() {
		t.Fatalf("copy = %#v / %q", copied, copied)
	}
	if !strings.Contains(original.Error(), "SECRET") {
		t.Fatal("original mutated")
	}
}
