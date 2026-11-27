//go:build darwin || linux

package cliproc

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"agentplane/internal/proctree"
)

// Run executes spec and waits for it. Every invocation, including bounded
// probes, owns its process tree through proctree. A descendant that left the
// group is out of scope. Failures to start, to terminate and cancellation are
// returned as *Error; any other failure is the child's wait error.
func Run(ctx context.Context, spec Spec) error {
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Env = spec.Env
	cmd.Dir = spec.Dir
	cmd.Stdin = spec.Stdin
	cmd.Stdout = spec.Stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := ctx.Err(); err != nil {
		return err
	}
	tree, err := proctree.Start(cmd)
	if err != nil {
		return newError(StageStart, err, nil)
	}
	exited := make(chan struct{})
	stopped := watchCancellation(ctx, tree, spec.Policy, exited)
	waitErr := tree.Wait()
	close(exited)
	c := <-stopped
	if !c.requested {
		// The leader ended on its own; sweep descendants it left in the group.
		c.outcome, c.err = tree.Terminate(context.Background(), spec.Policy)
	}
	return classify(ctx, waitErr, c)
}

// classify decides the failure from the termination record, not the exit
// status: a child that traps the request may exit 0 or nonzero.
func classify(ctx context.Context, waitErr error, c cancellation) error {
	var leak *proctree.LeakError
	switch {
	case errors.As(c.err, &leak):
		return newError(StageShutdown, errors.Join(ctx.Err(), c.err), &c.outcome)
	case c.err != nil:
		return newError(StageShutdown, c.err, nil)
	case c.requested && (waitErr != nil || c.outcome.Method != proctree.MethodAlreadyExited):
		return newError(StageCancelled, errors.Join(ctx.Err(), context.Cause(ctx)), &c.outcome)
	case errors.Is(waitErr, exec.ErrWaitDelay):
		return nil // leader succeeded; inherited pipes were bounded
	}
	return waitErr
}

type cancellation struct {
	requested bool
	outcome   proctree.Outcome
	err       error
}

// watchCancellation terminates the tree when ctx is done before exited closes.
// The result channel delivers once, so the caller reads it without a race.
func watchCancellation(ctx context.Context, tree *proctree.Tree, policy proctree.Policy, exited <-chan struct{}) <-chan cancellation {
	result := make(chan cancellation, 1)
	go func() {
		var c cancellation
		select {
		case <-ctx.Done():
			c.requested = true
			c.outcome, c.err = tree.Terminate(context.Background(), policy)
		case <-exited:
		}
		result <- c
	}()
	return result
}
