package gate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

type comparisonRoot interface {
	Open(relative string) (*os.File, error)
	Verify() error
	Close() error
}

// portableCommand reports only the fixed comparison status; it never runs a
// graph-selected program or copies compared bytes into evidence.
func portableCommand(ctx context.Context, c config, in map[string]graph.Value) (events.GateCheckPayload, error) {
	var p events.GateCheckPayload
	l, ok := in[c.Left].AsString()
	if !ok {
		return p, fmt.Errorf("input %q must resolve to a string path", c.Left)
	}
	r, ok := in[c.Right].AsString()
	if !ok {
		return p, fmt.Errorf("input %q must resolve to a string path", c.Right)
	}
	root, err := openComparisonRoot(c.Cwd)
	if err != nil {
		return p, fmt.Errorf("open comparison cwd: %w", err)
	}
	defer root.Close()
	left, err := root.Open(l)
	if err != nil {
		return p, fmt.Errorf("left comparison input: %w", err)
	}
	defer left.Close()
	right, err := root.Open(r)
	if err != nil {
		return p, fmt.Errorf("right comparison input: %w", err)
	}
	defer right.Close()
	if err := root.Verify(); err != nil {
		return p, err
	}
	beforeLeft, err := left.Stat()
	if err != nil {
		return p, fmt.Errorf("stat left comparison input: %w", err)
	}
	beforeRight, err := right.Stat()
	if err != nil {
		return p, fmt.Errorf("stat right comparison input: %w", err)
	}
	var leftBuf, rightBuf [32 * 1024]byte
	equal := true
	for {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		ln, le := io.ReadFull(left, leftBuf[:])
		rn, re := io.ReadFull(right, rightBuf[:])
		if err := readError(le); err != nil {
			return p, fmt.Errorf("read left comparison input: %w", err)
		}
		if err := readError(re); err != nil {
			return p, fmt.Errorf("read right comparison input: %w", err)
		}
		if ln != rn || !bytes.Equal(leftBuf[:ln], rightBuf[:rn]) {
			equal = false
			break
		}
		if ln < len(leftBuf) {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if err := root.Verify(); err != nil {
		return p, err
	}
	for _, item := range []struct {
		file   *os.File
		before os.FileInfo
		label  string
	}{{left, beforeLeft, "left"}, {right, beforeRight, "right"}} {
		if err := verifyComparisonFile(item.file, item.before, item.label); err != nil {
			return p, err
		}
	}
	code := 0
	p = events.GateCheckPayload{CheckID: c.CheckID, Kind: events.CheckCommand, Verdict: "green", Reason: "files match"}
	if !equal {
		code = 1
		p.Verdict = "red"
		p.Reason = "files differ"
	}
	p.Evidence = []events.GateEvidence{{Source: "command", ExitCode: &code}}
	return p, nil
}

func verifyComparisonFile(file *os.File, before os.FileInfo, label string) error {
	after, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s comparison input after read: %w", label, err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		return fmt.Errorf("%s comparison input changed during read", label)
	}
	return nil
}

func readError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}
