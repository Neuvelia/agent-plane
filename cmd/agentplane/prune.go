package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"agentplane/internal/adapter/system"
	"agentplane/internal/store"
)

// cliPrune is the optional `runs prune --json` result. Run directories are the
// only thing prune removes; Git worktrees and refs/agentplane/* belong to the
// workspace isolation feature (F3.1) and are never touched.
//
// A dry run lists the plan under `would_remove` and leaves `removed` empty, so a
// script that reads only `removed` never believes a dry run deleted anything.
type cliPrune struct {
	DryRun      bool              `json:"dry_run"`
	WouldRemove []string          `json:"would_remove"`
	Removed     []string          `json:"removed"`
	Skipped     []string          `json:"skipped"`
	Failed      []cliPruneFailure `json:"failed"`
}

type cliPruneFailure struct {
	RunID string `json:"run_id"`
	Error string `json:"error"`
}

// runsPruneCommand applies the store's own retention (`store.json`) once.
// Exit 0 on success (including a dry run and a store with no retention), 1 when
// any deletion failed, 2 on usage.
func runsPruneCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	const name = "runs prune"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonMode := fs.Bool("json", false, "emit one JSON document")
	dir := fs.String("store-dir", defaultStoreDir, "run store directory")
	dryRun := fs.Bool("dry-run", false, "list the runs that would be removed and remove nothing")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usageError(name, err, *jsonMode || wantsJSON(args), stdout, stderr)
	}
	runStore := store.Store{Dir: *dir}
	plan, err := runStore.PlanPrune(ctx, system.Clock{}.Now())
	if err != nil {
		return commandError(name, "error", "internal", err, nil, *jsonMode, stdout, stderr, exitInternal)
	}
	var result store.PruneResult
	var pruneErr error
	if !*dryRun {
		result, pruneErr = runStore.Prune(ctx, plan)
	}
	code := exitOK
	if pruneErr != nil {
		code = exitInternal
	}
	if *jsonMode {
		doc := cliDocument{Command: name, Status: pruneStatus(pruneErr), Diagnostics: nonNilDiagnostics(nil), Prune: pruneForJSON(result, plan, *dryRun)}
		return writeJSON(stdout, stderr, doc, code)
	}
	if err := renderPrune(stdout, result, plan, *dryRun); err != nil {
		return presentationError(err, stderr)
	}
	if pruneErr != nil {
		if _, err := fmt.Fprintf(stderr, "error: %v\n", pruneErr); err != nil {
			return presentationError(err, stderr)
		}
	}
	return code
}

func pruneStatus(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

func pruneForJSON(result store.PruneResult, plan store.PrunePlan, dryRun bool) *cliPrune {
	out := &cliPrune{DryRun: dryRun, WouldRemove: []string{}, Removed: []string{}, Skipped: []string{}, Failed: []cliPruneFailure{}}
	if dryRun {
		out.WouldRemove = append(out.WouldRemove, plan.Remove...)
	}
	out.Removed = append(out.Removed, result.Removed...)
	out.Skipped = append(out.Skipped, result.Skipped...)
	for _, f := range result.Failed {
		out.Failed = append(out.Failed, cliPruneFailure{RunID: f.RunID, Error: f.Err.Error()})
	}
	return out
}

func renderPrune(w io.Writer, result store.PruneResult, plan store.PrunePlan, dryRun bool) error {
	if len(plan.Remove) == 0 {
		_, err := fmt.Fprintln(w, "Nothing to prune (no retention is configured in store.json, or every run is within it).")
		return err
	}
	verb, listed := "Removed", result.Removed
	if dryRun {
		verb, listed = "Would remove", plan.Remove
	}
	if _, err := fmt.Fprintf(w, "%s %d run(s):\n", verb, len(listed)); err != nil {
		return err
	}
	for _, id := range listed {
		if _, err := fmt.Fprintf(w, "  %s\n", id); err != nil {
			return err
		}
	}
	for _, id := range result.Skipped {
		if _, err := fmt.Fprintf(w, "Skipped %s: a writer holds its lock\n", id); err != nil {
			return err
		}
	}
	for _, f := range result.Failed {
		if _, err := fmt.Fprintf(w, "Failed %s: %v\n", f.RunID, f.Err); err != nil {
			return err
		}
	}
	return nil
}

// pruneOnServeStart applies the store's retention once after `serve` has
// announced itself. With no retention configured the plan is empty and nothing
// happens. It reports to stderr (stdout stays the single announcement line). A
// planning or deletion failure, including an unreadable store.json, is a
// warning and never stops serve; a cancelled ctx is the server shutting down and
// is not reported. Only a failure to write the report is returned.
func pruneOnServeStart(ctx context.Context, runStore store.Store, stderr io.Writer) error {
	plan, err := runStore.PlanPrune(ctx, system.Clock{}.Now())
	if err != nil {
		return warnRetention(stderr, "retention could not be planned", err)
	}
	result, err := runStore.Prune(ctx, plan)
	if len(result.Removed) > 0 {
		if _, writeErr := fmt.Fprintf(stderr, "retention removed %d run(s)\n", len(result.Removed)); writeErr != nil {
			return writeErr
		}
	}
	if err != nil {
		return warnRetention(stderr, "retention could not remove every planned run", err)
	}
	return nil
}

func warnRetention(w io.Writer, what string, err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	_, writeErr := fmt.Fprintf(w, "warning: %s: %v\n", what, err)
	return writeErr
}
