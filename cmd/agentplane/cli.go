package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/agent/codex"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
	"agentplane/internal/store"
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
	if len(args) >= 1 && args[0] == "serve" {
		return serveCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) >= 2 && args[0] == "runs" && (args[1] == "list" || args[1] == "show") {
		return runsCommand(args[1], args[2:], stdout, stderr)
	}
	if wantsJSON(args) {
		return usageError("agentplane", errors.New("unknown command"), true, stdout, stderr)
	}
	if _, err := fmt.Fprintln(stderr, "usage: agentplane graph validate PATH [--json] | agentplane run PATH [--input key=value]... [--redact-env NAME]... [--approve NODE]... [--reject NODE]... [--store-dir DIR] [--record-values] [--max-concurrency N] [--watch | --json] | agentplane serve [--store-dir DIR] [--redact-env NAME]... [--record-values] [--max-concurrency N] [--codex-bin ABSOLUTE_PATH] | agentplane runs list [--store-dir DIR] [--json] | agentplane runs show RUN_ID [--store-dir DIR] [--json] | agentplane version"); err != nil {
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
	def, _, err := readDefinitionBytes(path)
	return def, err
}

// readDefinitionBytes also returns the exact bytes that were parsed so a run
// store can snapshot the definition that actually executed.
func readDefinitionBytes(path string) (*graph.Definition, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read graph file %q: %w", path, err)
	}
	def, err := graph.Parse(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse graph file %q: %w", path, err)
	}
	return def, data, nil
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
	validation := validateGraph(def)
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
	adapterID := fs.String("adapter", "", "explicit installed adapter (codex)")
	codexBin := fs.String("codex-bin", "", "absolute Codex CLI binary path")
	watch := fs.Bool("watch", false, "show events as they occur")
	concurrency := fs.Int("max-concurrency", 2, "maximum concurrent nodes")
	var inputs inputFlags
	var redactEnv inputFlags
	var approveNodes, rejectNodes inputFlags
	storeDir := fs.String("store-dir", "", "persist the run under this directory (events.jsonl + graph.yaml)")
	recordValues := fs.Bool("record-values", false, "record bounded, redacted graph input and accepted output values as events")
	fs.Var(&approveNodes, "approve", "approve a human node by ID; repeatable")
	fs.Var(&rejectNodes, "reject", "reject a human node by ID; repeatable")
	fs.Var(&inputs, "input", "graph input as key=value; repeat for multiple inputs")
	fs.Var(&redactEnv, "redact-env", "redact a named environment value from event logs")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usageError("run", err, wantsJSON(args), stdout, stderr)
	}
	if (*adapterID != "" && *adapterID != "codex") || (*adapterID == "codex" && !filepath.IsAbs(*codexBin)) || (*adapterID == "" && *codexBin != "") {
		return usageError("run", errors.New("use --adapter codex with --codex-bin ABSOLUTE_PATH"), *jsonMode, stdout, stderr)
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
	def, graphBytes, err := readDefinitionBytes(path)
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
	decisions, err := decisionProvider(def, approveNodes, rejectNodes, stdinInteractive(), os.Stdin, stderr)
	if err != nil {
		return usageError("run", err, *jsonMode, stdout, stderr)
	}
	opts := runtime.Options{MaxConcurrency: *concurrency, LogRedactions: redactions, Decisions: decisions, RecordValues: *recordValues}
	var writer persister
	if *storeDir != "" {
		runStore := store.Store{Dir: *storeDir}
		if err := runStore.Check(); err != nil {
			return commandError("run", "error", "internal", err, nil, *jsonMode, stdout, stderr, exitInternal)
		}
		if graphHoldsSecret(def, graphBytes, redactions) {
			return commandError("run", "error", "graph", errGraphHoldsSecret, nil, *jsonMode, stdout, stderr, exitGraph)
		}
		writer = openPersister(runStore, graphBytes)
		opts.Persist = writer.Append
	}
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
	var executor runtime.Executor = shell.Executor{}
	for _, node := range def.Nodes {
		if node.Type != "shell" || node.Loop != nil {
			executor = nodeExecutor{}
			break
		}
	}
	if *adapterID == "codex" {
		executor = nodeExecutor{agents: agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"codex": codex.Adapter{Binary: *codexBin}}}}
	}
	result, err := runtime.Run(runCtx, def, values, executor, opts)
	if writer != nil {
		if closeErr := writer.Close(); closeErr != nil && result.PersistError == nil {
			result.PersistError = closeErr
		}
	}
	// A persistence failure never hides what the run did: the result is still
	// shown, and the command exits 1 with the run ID and the partial log path.
	var persistFailure error
	if err == nil && result.PersistError != nil {
		persistFailure = fmt.Errorf("run %s was not persisted (a partial log may exist under %s): %w", result.RunID, filepath.Join(*storeDir, string(result.RunID)), result.PersistError)
	}
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
	if persistFailure != nil {
		code = exitInternal
	}
	if *jsonMode {
		doc := documentForRun(result, allDiagnostics)
		if persistFailure != nil {
			doc.Error = &cliError{Kind: "internal", Message: persistFailure.Error()}
		}
		return writeJSON(stdout, stderr, doc, code)
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
	if persistFailure != nil {
		if _, err := fmt.Fprintln(stderr, persistFailure); err != nil {
			return presentationError(err, stderr)
		}
	}
	return code
}

// persister is the run store's write side, a seam so a mid-run write failure
// can be exercised without relying on filesystem permissions.
type persister interface {
	Append(events.Event) error
	Close() error
}

var openPersister = func(s store.Store, graph []byte) persister { return s.NewWriter(graph) }

var errGraphHoldsSecret = errors.New("graph file contains the value of a configured --redact-env secret; remove it from the graph before storing the run")

// graphHoldsSecret guards the store's byte copy of the graph: a secret embedded
// in the graph would be duplicated into the store. Graphs must reference
// secrets, never contain them (AGENTS.md). `run --store-dir` and `serve` share it.
func graphHoldsSecret(def *graph.Definition, graphBytes []byte, redactions []string) bool {
	for _, secret := range redactions {
		if secret != "" && (bytes.Contains(graphBytes, []byte(secret)) || definitionHoldsSecret(def, secret)) {
			return true
		}
	}
	return false
}

// definitionHoldsSecret reports whether any string in the parsed definition -
// values, map keys such as node and port IDs, edge references, bindings - holds
// the secret. It walks the whole structure by reflection so no field can be
// forgotten, and it compares parsed values, so YAML escapes, folding and unicode
// escapes cannot hide a secret from the byte-level check on the stored snapshot.
func definitionHoldsSecret(def *graph.Definition, secret string) bool {
	var walk func(v reflect.Value) bool
	walk = func(v reflect.Value) bool {
		switch v.Kind() {
		case reflect.String:
			return strings.Contains(v.String(), secret)
		case reflect.Interface, reflect.Pointer:
			return !v.IsNil() && walk(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if walk(v.Index(i)) {
					return true
				}
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				if walk(k) || walk(v.MapIndex(k)) {
					return true
				}
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() && walk(v.Field(i)) {
					return true
				}
			}
		}
		return false
	}
	return def != nil && walk(reflect.ValueOf(def))
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
