// Package process runs fixed local commands under a sidecar-owned supervisor.
// It does not expose a graph-controllable process service.
package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

const supervisorFlag = "--internal-process-supervisor"
const maxRequestBytes = 8 << 20

// Spec is sent through the private sidecar-to-supervisor pipe, never argv.
// Env is the complete target environment; no ambient values are added.
type Spec struct {
	Path  string   `json:"path"`
	Args  []string `json:"args"`
	Env   []string `json:"env"`
	Dir   string   `json:"dir"`
	Stdin []byte   `json:"stdin,omitempty"`
	// ExtraFiles is limited to the two read descriptors used by fixed diff.
	// It is carried through OS inheritance, never serialized in the request.
	ExtraFiles []*os.File `json:"-"`
}

type wireSpec struct {
	Spec
	ExtraFileCount int `json:"extra_file_count,omitempty"`
}

type Outcome struct {
	ExitCode  int
	StartedAt time.Time
	EndedAt   time.Time
}

// Run keeps the control pipe open for the target's lifetime. If the caller
// dies, pipe EOF causes the supervisor to terminate its owned target group.
func Run(ctx context.Context, supervisorBinary string, spec Spec, stdout, stderr io.Writer) (Outcome, error) {
	outcome := Outcome{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return outcome, err
	}
	if supervisorBinary == "" {
		return outcome, errors.New("supervisor executable is missing")
	}
	if spec.Path == "" {
		return outcome, errors.New("target executable is missing")
	}
	if len(spec.ExtraFiles) != 0 && len(spec.ExtraFiles) != 2 {
		return outcome, errors.New("owned process requires exactly two extra descriptors")
	}
	if len(spec.ExtraFiles) > 0 && runtime.GOOS == "windows" {
		return outcome, errors.New("owned process extra descriptors are unsupported on Windows")
	}
	var request bytes.Buffer
	if err := json.NewEncoder(&request).Encode(wireSpec{Spec: spec, ExtraFileCount: len(spec.ExtraFiles)}); err != nil {
		return outcome, fmt.Errorf("encode owned process request: %w", err)
	}
	if request.Len() > maxRequestBytes {
		return outcome, errors.New("owned process request exceeds limit")
	}
	read, write, err := os.Pipe()
	if err != nil {
		return outcome, fmt.Errorf("create supervisor pipe: %w", err)
	}
	defer write.Close()
	cmd := exec.Command(supervisorBinary, supervisorFlag)
	cmd.Stdin = read
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	cmd.WaitDelay = 2 * time.Second
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		read.Close()
		return outcome, fmt.Errorf("create supervisor status pipe: %w", err)
	}
	defer statusRead.Close()
	defer statusWrite.Close()
	closeInherited, err := configureStatus(cmd, statusWrite, spec.ExtraFiles)
	if err != nil {
		read.Close()
		return outcome, fmt.Errorf("configure supervisor status: %w", err)
	}
	closeInherited = sync.OnceFunc(closeInherited)
	defer closeInherited()
	if err := cmd.Start(); err != nil {
		read.Close()
		return outcome, fmt.Errorf("start process supervisor: %w", err)
	}
	read.Close()
	closeInherited()
	_ = statusWrite.Close()
	supervisorStarted := time.Now()
	type statusResult struct {
		status targetStatus
		err    error
	}
	statusDone := make(chan statusResult, 1)
	go func() { status, err := readTargetStatus(statusRead); statusDone <- statusResult{status, err} }()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	sent := make(chan error, 1)
	go func() { _, err := write.Write(request.Bytes()); sent <- err }()
	var sendErr error
	finish := func(waitErr error) (targetStatus, error) {
		outcome.EndedAt = time.Now()
		select {
		case reply := <-statusDone:
			if reply.err != nil {
				return reply.status, fmt.Errorf("read process supervisor status: %w", reply.err)
			}
			return reply.status, applyTargetStatus(&outcome, supervisorStarted, reply.status, waitErr)
		case <-time.After(2 * time.Second):
			_ = statusRead.Close()
			return targetStatus{}, &SupervisorFailure{Reason: "status channel did not close"}
		}
	}
	for {
		select {
		case err := <-sent:
			sent = nil
			if err != nil {
				sendErr = err
				write.Close()
			}
		case err := <-wait:
			_, statusErr := finish(err)
			if sendErr != nil && statusErr == nil {
				return outcome, fmt.Errorf("send owned process request: %w", sendErr)
			}
			return outcome, statusErr
		case <-ctx.Done():
			write.Close()
			select {
			case err := <-wait:
				status, statusErr := finish(err)
				if statusErr != nil && status.Failure != "cancelled" && status.Failure != "signal" {
					return outcome, &JoinFailure{}
				}
				return outcome, ctx.Err()
			case <-time.After(3 * time.Second):
				// Killing a stuck supervisor here could orphan its target.
				return outcome, &JoinFailure{}
			}
		}
	}
}
