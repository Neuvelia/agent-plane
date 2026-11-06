package process

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

const maxStatusBytes = 1024

// TargetStartError means that the target never started. It never includes
// graph-provided argv, environment values, or the raw process error.
type TargetStartError struct{ Reason string }

func (e *TargetStartError) Error() string { return "target process did not start: " + e.Reason }

// JoinFailure means that cancellation did not prove that the supervisor and
// its target have stopped. It deliberately does not unwrap context.Canceled.
type JoinFailure struct{}

func (*JoinFailure) Error() string {
	return "process supervisor did not join after cancellation; cleanup is unverified"
}

// SupervisorFailure means the helper did not provide a trustworthy final
// target/ownership status. It is distinct from a target start or exit error.
type SupervisorFailure struct{ Reason string }

func (e *SupervisorFailure) Error() string {
	return "process supervisor status is unverified: " + e.Reason
}

type targetStatus struct {
	Started bool   `json:"started"`
	Code    int    `json:"code"`
	Failure string `json:"failure,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (s targetStatus) valid() bool {
	if !s.Started {
		return s.Code == -1 && (s.Failure == "start" || s.Failure == "supervisor")
	}
	if s.Failure == "" {
		return s.Code >= 0
	}
	return s.Code == -1 && (s.Failure == "signal" || s.Failure == "supervisor" || s.Failure == "cancelled")
}

func writeTargetStatus(w io.Writer, status targetStatus) {
	if w != nil {
		_ = json.NewEncoder(w).Encode(status)
	}
}

func readTargetStatus(r io.Reader) (targetStatus, error) {
	var status targetStatus
	data, err := io.ReadAll(io.LimitReader(r, maxStatusBytes+1))
	if err != nil {
		return targetStatus{}, &SupervisorFailure{Reason: "cannot read status"}
	}
	if len(data) == 0 || len(data) > maxStatusBytes || json.Unmarshal(data, &status) != nil || !status.valid() {
		return targetStatus{}, &SupervisorFailure{Reason: "missing or invalid status frame"}
	}
	return status, nil
}

func applyTargetStatus(outcome *Outcome, supervisorStarted time.Time, status targetStatus, waitErr error) error {
	if status.Started {
		outcome.StartedAt = supervisorStarted
	}
	outcome.ExitCode = status.Code
	switch status.Failure {
	case "start":
		if status.Reason != "executable is unavailable" && status.Reason != "working directory is unavailable" {
			return &TargetStartError{Reason: "target launch failed"}
		}
		return &TargetStartError{Reason: status.Reason}
	case "supervisor":
		return &SupervisorFailure{Reason: "helper failed before ownership was verified"}
	case "signal":
		return errors.New("target process ended without an exit code")
	case "cancelled":
		return errors.New("process supervisor stopped the target")
	case "":
		if status.Code == 0 && waitErr == nil {
			return nil
		}
		if status.Code != 0 && waitErr != nil {
			return waitErr
		}
		return &SupervisorFailure{Reason: "helper exit disagrees with target status"}
	default:
		return &SupervisorFailure{Reason: "invalid target status"}
	}
}

func statusFileFromEnvironment() *os.File { return openStatusFileFromEnvironment() }
