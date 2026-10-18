package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
	"agentplane/internal/version"
)

const (
	exitOK          = 0
	exitInternal    = 1
	exitUsage       = 2
	exitGraph       = 3
	exitRun         = 4
	exitInterrupted = 130
)

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		if _, err := fmt.Fprintln(stdout, version.String()); err != nil {
			return presentationError(err, stderr)
		}
		return exitOK
	}
	if len(args) >= 2 && args[0] == "graph" && args[1] == "validate" {
		return validateCommand(args[2:], stdout, stderr)
	}
	if len(args) >= 1 && args[0] == "run" {
		return executeCommand(ctx, args[1:], stdout, stderr)
	}
	if wantsJSON(args) {
		return usageError("agentplane", errors.New("unknown command"), true, stdout, stderr)
	}
	if _, err := fmt.Fprintln(stderr, "usage: agentplane graph validate PATH [--json] | agentplane run PATH [--input key=value]... [--redact-env NAME]... [--max-concurrency N] [--watch | --json] | agentplane version"); err != nil {
		return presentationError(err, stderr)
	}
	return exitUsage
}

func commandFlags(name string, args []string) (string, *flag.FlagSet, error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return "", nil, fmt.Errorf("%s requires a graph file path first", name)
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return args[0], fs, nil
}

func readDefinition(path string) (*graph.Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read graph file %q: %w", path, err)
	}
	def, err := graph.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse graph file %q: %w", path, err)
	}
	return def, nil
}

func validateCommand(args []string, stdout, stderr io.Writer) int {
	path, fs, err := commandFlags("graph validate", args)
	if err != nil {
		return usageError("graph validate", err, wantsJSON(args), stdout, stderr)
	}
	jsonMode := fs.Bool("json", false, "emit one JSON document")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usageError("graph validate", err, wantsJSON(args), stdout, stderr)
	}
	def, err := readDefinition(path)
	if err != nil {
		return commandError("graph validate", "error", "graph", err, nil, *jsonMode, stdout, stderr, exitGraph)
	}
	validation := graph.Validate(def)
	status, code := "valid", exitOK
	if validation.HasErrors() {
		status, code = "invalid", exitGraph
	}
	if *jsonMode {
		return writeJSON(stdout, stderr, cliDocument{Command: "graph validate", Status: status, GraphID: def.ID, Diagnostics: nonNilDiagnostics(validation.Diagnostics)}, code)
	}
	if err := renderDiagnostics(stderr, validation.Diagnostics); err != nil {
		return presentationError(err, stderr)
	}
	if _, err := fmt.Fprintf(stderr, "Graph %s: %s\n", def.ID, status); err != nil {
		return presentationError(err, stderr)
	}
	return code
}

func executeCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	path, fs, err := commandFlags("run", args)
	if err != nil {
		return usageError("run", err, wantsJSON(args), stdout, stderr)
	}
	jsonMode := fs.Bool("json", false, "emit one JSON document")
	watch := fs.Bool("watch", false, "show events as they occur")
	concurrency := fs.Int("max-concurrency", 2, "maximum concurrent nodes")
	var inputs inputFlags
	var redactEnv inputFlags
	fs.Var(&inputs, "input", "graph input as key=value; repeat for multiple inputs")
	fs.Var(&redactEnv, "redact-env", "redact a named environment value from event logs")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usageError("run", err, wantsJSON(args), stdout, stderr)
	}
	if *jsonMode && *watch {
		return usageError("run", errors.New("--json and --watch cannot be combined"), true, stdout, stderr)
	}
	if *concurrency < 1 {
		return usageError("run", errors.New("--max-concurrency must be at least 1"), *jsonMode, stdout, stderr)
	}
	if err := validateInputSyntax(inputs); err != nil {
		return usageError("run", err, *jsonMode, stdout, stderr)
	}
	redactions, err := redactionsFromEnv(redactEnv)
	if err != nil {
		return usageError("run", err, *jsonMode, stdout, stderr)
	}
	def, err := readDefinition(path)
	if err != nil {
		return commandError("run", "error", "graph", err, nil, *jsonMode, stdout, stderr, exitGraph)
	}
	validation := graph.Validate(def)
	if validation.HasErrors() {
		return commandError("run", "invalid", "validation", nil, validation.Diagnostics, *jsonMode, stdout, stderr, exitGraph)
	}
	values, valueValidation, err := parseInputs(def, inputs)
	if err != nil {
		return usageError("run", err, *jsonMode, stdout, stderr)
	}
	plan, compiled := graph.CompileBindings(def)
	if !compiled.Valid() {
		return commandError("run", "invalid", "validation", nil, compiled.Diagnostics, *jsonMode, stdout, stderr, exitGraph)
	}
	inputValidation := plan.ValidateGraphInputs(values)
	allDiagnostics := append(append([]graph.Diagnostic(nil), validation.Diagnostics...), valueValidation.Diagnostics...)
	allDiagnostics = append(allDiagnostics, inputValidation.Diagnostics...)
	if valueValidation.HasErrors() || inputValidation.HasErrors() {
		return commandError("run", "invalid", "input", nil, allDiagnostics, *jsonMode, stdout, stderr, exitGraph)
	}
	if !*jsonMode {
		if err := renderDiagnostics(stderr, validation.Diagnostics); err != nil {
			return presentationError(err, stderr)
		}
	}
	opts := runtime.Options{MaxConcurrency: *concurrency, LogRedactions: redactions}
	runCtx := ctx
	var finishWatch func([]events.Event) error
	if *watch {
		watchCtx, cancelWatch := context.WithCancel(ctx)
		defer cancelWatch()
		runCtx = watchCtx
		live := make(chan events.Event)
		opts.LiveEvents = live
		replay := make(chan []events.Event, 1)
		done := make(chan error, 1)
		go func() {
			renderer := newEventRenderer(stdout, stderr, true)
			done <- renderWatchEvents(renderer, live, replay, cancelWatch)
		}()
		finishWatch = func(retained []events.Event) error {
			close(live)
			replay <- retained
			if ctx.Err() == nil {
				return <-done
			}
			select {
			case err := <-done:
				return err
			case <-time.After(100 * time.Millisecond):
				// An unresponsive terminal must not prevent cancellation.
				return nil
			}
		}
	}
	result, err := runtime.Run(runCtx, def, values, shell.Executor{}, opts)
	if finishWatch != nil {
		if writeErr := finishWatch(result.Events); writeErr != nil {
			return presentationError(writeErr, stderr)
		}
	}
	if err != nil {
		var preflight *runtime.PreflightError
		if errors.As(err, &preflight) {
			return commandError("run", "invalid", "input", nil, preflight.Diagnostics.Diagnostics, *jsonMode, stdout, stderr, exitGraph)
		}
		return commandError("run", "error", "internal", err, nil, *jsonMode, stdout, stderr, exitInternal)
	}
	code := exitRun
	if result.State == runtime.RunSucceeded {
		code = exitOK
	}
	if result.State == runtime.RunCancelled {
		code = exitInterrupted
	}
	if *jsonMode {
		return writeJSON(stdout, stderr, documentForRun(result, allDiagnostics), code)
	}
	if !*watch {
		renderer := newEventRenderer(stdout, stderr, false)
		for _, event := range result.Events {
			if err := renderer.Event(event); err != nil {
				return presentationError(err, stderr)
			}
		}
	}
	if result.Failure != nil {
		if err := renderFailure(stderr, result.Failure); err != nil {
			return presentationError(err, stderr)
		}
	}
	return code
}

func wantsJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || arg == "-json" || arg == "--json=true" {
			return true
		}
	}
	return false
}

func usageError(command string, err error, jsonMode bool, stdout, stderr io.Writer) int {
	if err == nil {
		err = errors.New("unexpected positional argument")
	}
	if jsonMode {
		return commandError(command, "error", "usage", err, nil, true, stdout, stderr, exitUsage)
	}
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "%s: %v\n", command, err); writeErr != nil {
			return presentationError(writeErr, stderr)
		}
	}
	return exitUsage
}

func commandError(command, status, kind string, err error, diagnostics []graph.Diagnostic, jsonMode bool, stdout, stderr io.Writer, code int) int {
	if jsonMode {
		doc := cliDocument{Command: command, Status: status, Diagnostics: nonNilDiagnostics(diagnostics)}
		if err != nil {
			doc.Error = &cliError{Kind: kind, Message: err.Error()}
		}
		return writeJSON(stdout, stderr, doc, code)
	}
	if err != nil {
		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
			return presentationError(writeErr, stderr)
		}
	}
	if writeErr := renderDiagnostics(stderr, diagnostics); writeErr != nil {
		return presentationError(writeErr, stderr)
	}
	return code
}

func presentationError(err error, stderr io.Writer) int {
	_, _ = fmt.Fprintf(stderr, "write human output: %v\n", err)
	return exitInternal
}
