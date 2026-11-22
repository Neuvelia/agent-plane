package proctree

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
)

// Prepare adjusts a command before it starts, for example to add namespace
// flags. It runs before proctree sets its own SysProcAttr fields, which are
// merged afterwards and therefore win.
type Prepare func(cmd *exec.Cmd) error

// Tree is a started child and its process tree. The zero value and a nil
// *Tree are valid and mean "nothing was started".
type Tree struct {
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error

	mu sync.Mutex // serializes Terminate
	owner
}

// Start starts cmd and owns its tree. A start failure is returned unchanged so
// callers can classify it (for example *exec.Error).
func Start(cmd *exec.Cmd) (*Tree, error) {
	return StartWith(cmd, nil)
}

// StartWith runs prepare, then starts cmd and owns its tree.
func StartWith(cmd *exec.Cmd, prepare Prepare) (*Tree, error) {
	if cmd == nil || cmd.Process != nil {
		return nil, errors.New("proctree: command is nil or already started")
	}
	if prepare != nil {
		if err := prepare(cmd); err != nil {
			return nil, fmt.Errorf("preparing process: %w", err)
		}
	}
	t := &Tree{cmd: cmd, done: make(chan struct{})}
	if err := t.attach(cmd); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t.started()
	go func() {
		t.waitErr = cmd.Wait()
		close(t.done)
	}()
	return t, nil
}

// Wait returns cmd.Wait's result once the leader has exited and its output
// pipes are drained or abandoned after cmd.WaitDelay. It may be called any
// number of times.
func (t *Tree) Wait() error {
	if t == nil || t.cmd == nil {
		return nil
	}
	<-t.done
	return t.waitErr
}

// Terminate stops the tree: request, bounded grace, force, verify. It is
// idempotent; a nil Tree, an exited tree and repeated calls succeed with
// MethodAlreadyExited. ctx only shortens the grace; it does not decide why the
// process is being stopped.
func (t *Tree) Terminate(ctx context.Context, p Policy) (Outcome, error) {
	if t == nil || t.cmd == nil {
		return Outcome{Method: MethodAlreadyExited, Grace: p.grace()}, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.terminate(ctx, p)
}
