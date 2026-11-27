package gate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/proctree"
	rt "agentplane/internal/runtime"
)

// commandEvidenceBound limits the whole diff evidence step when
// Executor.EvidenceBound is zero.
const commandEvidenceBound = 30 * time.Second

// openCompared rejects symlinks in every component before opening read-only.
// This is path preflight, not protection from hostile concurrent path mutation.
func openCompared(cwd, relative string) (*os.File, error) {
	q, err := normalize(relative)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve cwd: %w", pathCause(err))
	}
	if canonical != cwd {
		return nil, errors.New("cwd must not contain symlinks")
	}
	root, err := os.Stat(cwd)
	if err != nil || !root.IsDir() {
		return nil, errors.New("cwd must be an existing directory")
	}
	candidate := filepath.Join(cwd, filepath.FromSlash(q))
	for cur := candidate; cur != cwd; cur = filepath.Dir(cur) {
		s, e := os.Lstat(cur)
		if e != nil {
			return nil, fmt.Errorf("inspect comparison path: %w", pathCause(e))
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("comparison path must not contain symlinks")
		}
		if cur == candidate && !s.Mode().IsRegular() {
			return nil, errors.New("comparison input must be a regular file")
		}
	}
	f, err := os.Open(candidate)
	if err != nil {
		return nil, fmt.Errorf("open comparison input: %w", pathCause(err))
	}
	s, err := f.Stat()
	if err != nil || !s.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("opened comparison input must be regular")
	}
	return f, nil
}
func (e Executor) command(ctx context.Context, c config, in map[string]graph.Value, o rt.ExecutionObservation) (events.GateCheckPayload, error) {
	var p events.GateCheckPayload
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return p, errors.New("read-only diff unsupported on this platform")
	}
	left, right, err := openOperands(c, in)
	if err != nil {
		return p, err
	}
	defer left.Close()
	defer right.Close()
	capture := o.NewCheckCapture
	if capture == nil {
		capture = rt.NewCheckCapture
	}
	stdout, stderr := capture(), capture()
	bound := e.EvidenceBound
	if bound <= 0 {
		bound = commandEvidenceBound
	}
	bounded, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	cmd := e.diffCommand(c.Cwd, left, right)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	exit, err := runDiffTree(bounded, cmd, e.Termination)
	out, no := stdout.Finish()
	errs, ne := stderr.Finish()
	if err != nil {
		if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
			// A timeout is not evidence about the candidate; never a verdict.
			return p, fmt.Errorf("diff evidence exceeded %s: %w", bound, errors.Join(context.DeadlineExceeded, err))
		}
		return p, err
	}
	p = events.GateCheckPayload{CheckID: c.CheckID, Kind: events.CheckKind(c.Kind), Verdict: "green", Reason: "files match", Evidence: []events.GateEvidence{{Source: "command", ExitCode: &exit, Stdout: out, Stderr: errs, StdoutOmitted: no, StderrOmitted: ne}}}
	if exit == 1 {
		p.Verdict = "red"
		p.Reason = "files differ"
	}
	return p, nil
}

func openOperands(c config, in map[string]graph.Value) (left, right *os.File, err error) {
	l, ok := in[c.Left].AsString()
	if !ok {
		return nil, nil, fmt.Errorf("input %q must resolve to a string path", c.Left)
	}
	r, ok := in[c.Right].AsString()
	if !ok {
		return nil, nil, fmt.Errorf("input %q must resolve to a string path", c.Right)
	}
	left, err = openCompared(c.Cwd, l)
	if err != nil {
		return nil, nil, err
	}
	right, err = openCompared(c.Cwd, r)
	if err != nil {
		closeAll([]*os.File{left})
		return nil, nil, err
	}
	return left, right, nil
}

// diffCommand builds the only process this Gate starts: fixed operands and
// preopened descriptors, a controlled environment, and no context, because
// proctree owns termination of the whole tree.
func (e Executor) diffCommand(cwd string, left, right *os.File) *exec.Cmd {
	binary := "/usr/bin/diff"
	if e.diffPath != "" {
		binary = e.diffPath
	}
	cmd := exec.Command(binary, "/dev/fd/3", "/dev/fd/4")
	cmd.Dir = cwd
	cmd.Env = []string{"LC_ALL=C"}
	cmd.ExtraFiles = []*os.File{left, right}
	cmd.WaitDelay = time.Second
	return cmd
}

func runDiff(ctx context.Context, cmd *exec.Cmd) (int, error) {
	return runDiffTree(ctx, cmd, proctree.Policy{})
}

// runDiffTree separates lifecycle failures from the two valid comparison exits.
// The process tree it starts never outlives the call: cancellation terminates
// it, and a leader that exits on its own has its group swept.
func runDiffTree(ctx context.Context, cmd *exec.Cmd, policy proctree.Policy) (int, error) {
	waitErr, err := runOwned(ctx, cmd, policy, "diff")
	if err != nil {
		return 0, err
	}
	return diffExit(waitErr)
}

// runOwned runs cmd as an owned process tree with bounded evidence pipes and
// returns the leader's wait error for the caller to classify. what names the
// process in errors.
func runOwned(ctx context.Context, cmd *exec.Cmd, policy proctree.Policy, what string) (waitErr, err error) {
	// Own the pipes: exec.Cmd.Wait can otherwise hide copy errors behind exit 1.
	targets := []io.Writer{cmd.Stdout, cmd.Stderr}
	readers, writers, err := evidencePipes()
	if err != nil {
		return nil, err
	}
	defer closeAll(readers)
	cmd.Stdout, cmd.Stderr = writers[0], writers[1]
	tree, err := startOwned(ctx, cmd, what)
	closeAll(writers)
	if err != nil {
		return nil, err
	}
	drained := make(chan error, 2)
	for i, r := range readers {
		target := targets[i]
		if target == nil {
			target = io.Discard
		}
		go func(r *os.File, w io.Writer) { defer r.Close(); _, e := io.Copy(w, r); drained <- e }(r, target)
	}
	waitErr, termErr := waitOwned(ctx, tree, policy)
	drainErr := awaitDrain(readers, drained, what)
	if termErr != nil {
		return nil, fmt.Errorf("terminate %s process tree: %w", what, errors.Join(ctx.Err(), termErr))
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if drainErr != nil {
		return nil, fmt.Errorf("drain %s evidence: %w", what, drainErr)
	}
	return waitErr, nil
}

func evidencePipes() (readers, writers []*os.File, err error) {
	for range 2 {
		r, w, e := os.Pipe()
		if e != nil {
			closeAll(readers)
			closeAll(writers)
			return nil, nil, fmt.Errorf("create diff evidence pipe: %w", e)
		}
		readers = append(readers, r)
		writers = append(writers, w)
	}
	return readers, writers, nil
}

// closeAll closes pipe ends that are done with; a failed close has no recovery.
func closeAll(files []*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

func startOwned(ctx context.Context, cmd *exec.Cmd, what string) (*proctree.Tree, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tree, err := proctree.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", what, err)
	}
	return tree, nil
}

// waitOwned waits for the leader, terminating the tree if ctx ends first. A
// leader that exited on its own has descendants it left in the group swept.
func waitOwned(ctx context.Context, tree *proctree.Tree, policy proctree.Policy) (waitErr, termErr error) {
	exited := make(chan struct{})
	stopped := watchCancellation(ctx, tree, policy, exited)
	waitErr = tree.Wait()
	close(exited)
	c := <-stopped
	if !c.requested {
		_, c.err = tree.Terminate(context.Background(), policy)
	}
	return waitErr, c.err
}

func awaitDrain(readers []*os.File, drained <-chan error, what string) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	var drainErr error
	for remaining := 2; remaining > 0; remaining-- {
		select {
		case e := <-drained:
			if e != nil {
				drainErr = e
			}
		case <-timer.C:
			closeAll(readers)
			for ; remaining > 0; remaining-- {
				<-drained
			}
			drainErr = fmt.Errorf("%s evidence drain exceeded one second", what)
		}
	}
	return drainErr
}

func diffExit(waitErr error) (int, error) {
	if waitErr == nil {
		return 0, nil
	}
	var e *exec.ExitError
	if errors.As(waitErr, &e) && e.ExitCode() == 1 {
		return 1, nil
	}
	return 0, fmt.Errorf("diff execution failed: %w", waitErr)
}

type cancellation struct {
	requested bool
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
			_, c.err = tree.Terminate(context.Background(), policy)
		case <-exited:
		}
		result <- c
	}()
	return result
}

func pathCause(err error) error {
	var p *os.PathError
	if errors.As(err, &p) {
		return p.Err
	}
	return err
}
