//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/proctree"
)

// Every invocation, including bounded probes, owns its process tree through
// proctree. A descendant that left the group is out of scope.
func runProcess(ctx context.Context, binary string, args, env []string, dir string, stdin io.Reader, stdout io.Writer, policy proctree.Policy) error {
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := ctx.Err(); err != nil {
		return err
	}
	tree, err := proctree.Start(cmd)
	if err != nil {
		return fail("start", "cannot start Codex CLI", err)
	}
	exited := make(chan struct{})
	stopped := watchCancellation(ctx, tree, policy, exited)
	waitErr := tree.Wait()
	close(exited)
	c := <-stopped
	if !c.requested {
		// The leader ended on its own; sweep descendants it left in the group.
		c.outcome, c.err = tree.Terminate(context.Background(), policy)
	}
	return classify(ctx, waitErr, c)
}

// classify decides the failure from the termination record, not the exit
// status: a child that traps the request may exit 0 or nonzero.
func classify(ctx context.Context, waitErr error, c cancellation) error {
	var leak *proctree.LeakError
	switch {
	case errors.As(c.err, &leak):
		return withTermination(fail("shutdown", "cannot terminate owned process group", errors.Join(ctx.Err(), c.err)), c.outcome)
	case c.err != nil:
		return fail("shutdown", "cannot terminate owned process group", c.err)
	case c.requested && (waitErr != nil || c.outcome.Method != proctree.MethodAlreadyExited):
		return withTermination(fail("cancelled", "execution cancelled", errors.Join(ctx.Err(), context.Cause(ctx))), c.outcome)
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

func withTermination(err error, outcome proctree.Outcome) error {
	var typed *agent.ExecutionError
	if errors.As(err, &typed) {
		typed.Termination = &outcome
	}
	return err
}
