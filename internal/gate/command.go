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

	"agentplane/internal/events"
	"agentplane/internal/graph"
	run "agentplane/internal/runtime"
)

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
func command(ctx context.Context, c config, in map[string]graph.Value, o run.ExecutionObservation) (events.GateCheckPayload, error) {
	var p events.GateCheckPayload
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return p, errors.New("read-only diff unsupported on this platform")
	}
	l, ok := in[c.Left].AsString()
	if !ok {
		return p, fmt.Errorf("input %q must resolve to a string path", c.Left)
	}
	r, ok := in[c.Right].AsString()
	if !ok {
		return p, fmt.Errorf("input %q must resolve to a string path", c.Right)
	}
	left, err := openCompared(c.Cwd, l)
	if err != nil {
		return p, err
	}
	defer left.Close()
	right, err := openCompared(c.Cwd, r)
	if err != nil {
		return p, err
	}
	defer right.Close()
	capture := o.NewCheckCapture
	if capture == nil {
		capture = run.NewCheckCapture
	}
	stdout, stderr := capture(), capture()
	// Only these fixed operands and descriptors reach the trusted OS operation.
	cmd := exec.CommandContext(ctx, "/usr/bin/diff", "/dev/fd/3", "/dev/fd/4")
	cmd.Dir = c.Cwd
	cmd.Env = []string{"LC_ALL=C"}
	cmd.ExtraFiles = []*os.File{left, right}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	exit, err := runDiff(ctx, cmd)
	out, no := stdout.Finish()
	errs, ne := stderr.Finish()
	if err != nil {
		return p, err
	}
	p = events.GateCheckPayload{CheckID: c.CheckID, Kind: c.Kind, Verdict: "green", Reason: "files match", Evidence: []events.GateEvidence{{Source: "command", ExitCode: &exit, Stdout: out, Stderr: errs, StdoutOmitted: no, StderrOmitted: ne}}}
	if exit == 1 {
		p.Verdict = "red"
		p.Reason = "files differ"
	}
	return p, nil
}

// runDiff separates lifecycle failures from the two valid comparison exits.
func runDiff(ctx context.Context, cmd *exec.Cmd) (int, error) {
	// Own the pipes: exec.Cmd.Wait can otherwise hide copy errors behind exit 1.
	targets := []io.Writer{cmd.Stdout, cmd.Stderr}
	readers := make([]*os.File, 0, 2)
	writers := make([]*os.File, 0, 2)
	for range 2 {
		r, w, e := os.Pipe()
		if e != nil {
			for _, f := range append(readers, writers...) {
				f.Close()
			}
			return 0, fmt.Errorf("create diff evidence pipe: %w", e)
		}
		readers = append(readers, r)
		writers = append(writers, w)
	}
	defer func() {
		for _, f := range readers {
			f.Close()
		}
	}()
	cmd.Stdout = writers[0]
	cmd.Stderr = writers[1]
	err := cmd.Start()
	for _, f := range writers {
		f.Close()
	}
	if err != nil {
		return 0, fmt.Errorf("start diff: %w", err)
	}
	drained := make(chan error, 2)
	for i, r := range readers {
		target := targets[i]
		if target == nil {
			target = io.Discard
		}
		go func(r *os.File, w io.Writer) { defer r.Close(); _, e := io.Copy(w, r); drained <- e }(r, target)
	}
	err = cmd.Wait()
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
			for _, r := range readers {
				r.Close()
			}
			for ; remaining > 0; remaining-- {
				<-drained
			}
			drainErr = errors.New("diff evidence drain exceeded one second")
		}
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if drainErr != nil {
		return 0, fmt.Errorf("drain diff evidence: %w", drainErr)
	}
	if err == nil {
		return 0, nil
	}
	var e *exec.ExitError
	if errors.As(err, &e) && e.ExitCode() == 1 {
		return 1, nil
	}
	return 0, fmt.Errorf("diff execution failed: %w", err)
}

func pathCause(err error) error {
	var p *os.PathError
	if errors.As(err, &p) {
		return p.Err
	}
	return err
}
