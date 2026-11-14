package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"agentplane/internal/store"
)

const defaultStoreDir = ".agentplane/runs"

// runsCommand reads the local run store (ADR-0019). It never executes a graph.
func runsCommand(sub string, args []string, stdout, stderr io.Writer) int {
	name := "runs " + sub
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonMode := fs.Bool("json", false, "emit one JSON document")
	dir := fs.String("store-dir", defaultStoreDir, "run store directory")
	var runID string
	if sub == "show" {
		if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
			return usageError(name, errors.New("runs show requires a run ID first"), wantsJSON(args), stdout, stderr)
		}
		runID, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usageError(name, err, *jsonMode || wantsJSON(args), stdout, stderr)
	}
	runStore := store.Store{Dir: *dir}
	if sub == "list" {
		runs, err := runStore.List()
		if err != nil {
			return commandError(name, "error", "internal", err, nil, *jsonMode, stdout, stderr, exitInternal)
		}
		if *jsonMode {
			return writeJSON(stdout, stderr, cliDocument{Command: name, Status: "ok", Diagnostics: nonNilDiagnostics(nil), Runs: &runs}, exitOK)
		}
		for _, r := range runs {
			if _, err := fmt.Fprintf(stdout, "%s  %s  %-11s %s  %d events\n", r.RunID, r.StartedAt.Format("2006-01-02T15:04:05Z"), r.Status, r.GraphID, r.EventCount); err != nil {
				return presentationError(err, stderr)
			}
		}
		return exitOK
	}
	record, err := runStore.Read(runID)
	if err != nil {
		return commandError(name, "error", "input", err, nil, *jsonMode, stdout, stderr, exitRun)
	}
	if *jsonMode {
		evs := record.Events
		return writeJSON(stdout, stderr, cliDocument{Command: name, Status: record.Summary.Status, GraphID: record.Summary.GraphID, Diagnostics: nonNilDiagnostics(nil), Events: &evs, Truncated: record.Summary.Truncated}, exitOK)
	}
	renderer := newEventRenderer(stdout, stderr, false)
	for _, event := range record.Events {
		if err := renderer.Event(event); err != nil {
			return presentationError(err, stderr)
		}
	}
	if record.Summary.Truncated {
		if _, err := fmt.Fprintln(stderr, "warning: event log ends with an incomplete line"); err != nil {
			return presentationError(err, stderr)
		}
	}
	return exitOK
}
