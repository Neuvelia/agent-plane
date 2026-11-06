//go:build darwin || linux

package process

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Supervise is called only by the fixed private executable mode. It emits no
// diagnostics of its own to target stdout/stderr.
func Supervise(control io.Reader, stdout, stderr io.Writer) int {
	statusFile := statusFileFromEnvironment()
	status := targetStatus{Code: -1, Failure: "supervisor"}
	defer func() {
		writeTargetStatus(statusFile, status)
		if statusFile != nil {
			_ = statusFile.Close()
		}
	}()
	decoder := json.NewDecoder(io.LimitReader(control, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	var request wireSpec
	if err := decoder.Decode(&request); err != nil || request.Spec.Path == "" || (request.ExtraFileCount != 0 && request.ExtraFileCount != 2) {
		return 125
	}
	spec := request.Spec
	lost := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, control); close(lost) }()
	select {
	case <-lost:
		return 125
	default:
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Env = append([]string{}, spec.Env...)
	cmd.Dir = spec.Dir
	cmd.Stdin = bytes.NewReader(spec.Stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if request.ExtraFileCount == 2 {
		cmd.ExtraFiles = []*os.File{os.NewFile(3, "owned-extra-3"), os.NewFile(4, "owned-extra-4")}
		defer cmd.ExtraFiles[0].Close()
		defer cmd.ExtraFiles[1].Close()
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		status.Failure = "start"
		status.Reason = "executable is unavailable"
		if spec.Dir != "" {
			if info, checkErr := os.Stat(spec.Dir); checkErr != nil || !info.IsDir() {
				status.Reason = "working directory is unavailable"
			}
		}
		return 125
	}
	status.Started = true
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	abandoned := false
	select {
	case waitErr = <-done:
	case <-lost:
		abandoned = true
	}
	cleanupGroup(cmd.Process.Pid)
	if abandoned {
		return finishAbandonedTarget(done, time.After(time.Second), &status)
	}
	if waitErr == nil || errors.Is(waitErr, exec.ErrWaitDelay) {
		status.Code, status.Failure = 0, ""
		return 0
	}
	if state := cmd.ProcessState; state != nil && state.ExitCode() >= 0 {
		status.Code, status.Failure = state.ExitCode(), ""
		return state.ExitCode()
	}
	status.Failure = "signal"
	return 125
}

// A cancellation is verified only after Wait has reaped the target. If Wait
// remains blocked, the parent must see supervisor failure, not cancellation.
func finishAbandonedTarget(done <-chan error, timeout <-chan time.Time, status *targetStatus) int {
	select {
	case <-done:
		status.Failure = "cancelled"
	case <-timeout:
		status.Failure = "supervisor"
	}
	return 125
}

func cleanupGroup(pid int) {
	if errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	timer := time.NewTimer(300 * time.Millisecond)
	<-timer.C
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
