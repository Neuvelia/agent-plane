package process

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSupervisorFailureAndMissingStatusAreOwnershipErrors(t *testing.T) {
	for _, raw := range []string{"", `{"started":true,"code":-1,"failure":"cancelled"`} {
		status, err := readTargetStatus(strings.NewReader(raw))
		var supervisor *SupervisorFailure
		if !errors.As(err, &supervisor) || status != (targetStatus{}) {
			t.Fatalf("invalid status %q = %+v, %v", raw, status, err)
		}
	}
	var outcome = Outcome{ExitCode: -1}
	err := applyTargetStatus(&outcome, time.Now(), targetStatus{Started: true, Code: -1, Failure: "supervisor"}, errors.New("helper exit"))
	var supervisor *SupervisorFailure
	if !errors.As(err, &supervisor) || outcome.StartedAt.IsZero() {
		t.Fatalf("post-start supervisor failure = %+v, %v", outcome, err)
	}
}
