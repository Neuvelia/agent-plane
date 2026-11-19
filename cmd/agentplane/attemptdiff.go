package main

import (
	"context"
	"fmt"
	"io"

	"agentplane/internal/adapter/attemptdiff"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
)

// recordDiffsNotice states, once per run or server start, what --record-diffs
// puts into events (ADR-0026).
const recordDiffsNotice = "agentplane: --record-diffs records changed file contents of Agent attempts into run events and any run store; only --redact-env values are masked\n"

// attemptDiffer composes internal/adapter/attemptdiff behind runtime.AttemptDiffer.
type attemptDiffer struct{}

func (attemptDiffer) Open(context.Context) (runtime.DiffSession, error) {
	recorder, err := attemptdiff.Open()
	if err != nil {
		return nil, err
	}
	return diffSession{recorder}, nil
}

type diffSession struct{ recorder *attemptdiff.Recorder }

func (s diffSession) Snapshot(ctx context.Context, workspace string) (runtime.DiffSnapshot, error) {
	return s.recorder.Snapshot(ctx, workspace)
}

func (s diffSession) Diff(ctx context.Context, before, after runtime.DiffSnapshot, redactor runtime.DiffRedactor) (runtime.AttemptCapture, error) {
	beforeSnapshot, beforeOK := before.(attemptdiff.Snapshot)
	afterSnapshot, afterOK := after.(attemptdiff.Snapshot)
	if !beforeOK || !afterOK {
		return runtime.AttemptCapture{}, fmt.Errorf("attempt diff: snapshot does not belong to this session")
	}
	attempt, err := s.recorder.Diff(ctx, beforeSnapshot, afterSnapshot, redactor)
	return runtime.AttemptCapture{Files: attempt.Files, Summary: attempt.Summary}, err
}

func (s diffSession) Close() error { return s.recorder.Close() }

// usesLoopAgent reports whether any node is a loop Agent, the only producer
// attempt diffs capture; other graphs never open a session or scratch directory.
func usesLoopAgent(def *graph.Definition) bool {
	for _, node := range def.Nodes {
		if node.Type == "agent" && node.Loop != nil {
			return true
		}
	}
	return false
}

// configureAttemptDiffs enables capture when asked and useful. A graph without
// a loop Agent runs exactly as without the flag.
func configureAttemptDiffs(enabled bool, def *graph.Definition, options *runtime.Options) {
	if enabled && usesLoopAgent(def) {
		options.AttemptDiffs = attemptDiffer{}
	}
}

func announceRecordDiffs(enabled bool, stderr io.Writer) {
	if enabled {
		_, _ = io.WriteString(stderr, recordDiffsNotice)
	}
}
