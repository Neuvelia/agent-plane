package run

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestReasonOfFindsTypedCauseOnly(t *testing.T) {
	typed := CancelCause{Reason: CancelRunTimeout}
	for _, test := range []struct {
		name  string
		cause error
		want  CancelReason
		ok    bool
	}{
		{"value cause", typed, CancelRunTimeout, true},
		{"pointer cause", &CancelCause{Reason: CancelInterrupt}, CancelInterrupt, true},
		{"wrapped typed cause", fmt.Errorf("terminating node: %w", CancelCause{Reason: CancelNodeTimeout, Node: "n"}), CancelNodeTimeout, true},
		{"text equal to a reason", errors.New("run_timeout"), "", false},
		{"typed cause flattened into text", errors.New("cause: " + typed.Error()), "", false},
		{"context canceled", context.Canceled, "", false},
		{"deadline exceeded", context.DeadlineExceeded, "", false},
		{"unknown reason", CancelCause{Reason: "because"}, "", false},
		{"empty reason", CancelCause{}, "", false},
		{"nil", nil, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ReasonOf(test.cause)
			if got != test.want || ok != test.ok {
				t.Fatalf("ReasonOf(%v) = %q, %v; want %q, %v", test.cause, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestReasonOfReadsContextCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(CancelCause{Reason: CancelSiblingFailed, Node: "left"})
	got, ok := ReasonOf(context.Cause(ctx))
	if !ok || got != CancelSiblingFailed {
		t.Fatalf("ReasonOf(context.Cause) = %q, %v", got, ok)
	}
	plain, cancelPlain := context.WithCancel(context.Background())
	cancelPlain()
	if got, ok := ReasonOf(context.Cause(plain)); ok {
		t.Fatalf("a plain cancel produced reason %q", got)
	}
}

func TestCancelReasonVocabularyIsClosed(t *testing.T) {
	want := []CancelReason{CancelInterrupt, CancelUserCancel, CancelRunTimeout, CancelSiblingFailed, CancelNodeTimeout}
	for _, r := range want {
		if !r.Valid() {
			t.Fatalf("%q is not valid", r)
		}
	}
	for _, r := range []CancelReason{"", "timeout", "Interrupt", "run_timeout "} {
		if r.Valid() {
			t.Fatalf("%q must not be valid", r)
		}
	}
}
