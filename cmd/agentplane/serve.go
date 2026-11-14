package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"agentplane/internal/api"
	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
	"agentplane/internal/store"
)

// serveStdin is replaced in tests. The server exits when it is closed, which is
// how a desktop shell that spawned it takes it down with itself.
var serveStdin io.Reader = os.Stdin

// serveCommand starts the loopback API (ADR-0019). It prints exactly one JSON
// line {"port":N,"token":"..."} on stdout for the parent process and nothing
// else there; the token is never logged or placed in a URL.
func serveCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("store-dir", defaultStoreDir, "run store directory")
	concurrency := fs.Int("max-concurrency", 2, "maximum concurrent nodes per run")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usageError("serve", err, false, stdout, stderr)
	}
	if *concurrency < 1 {
		return usageError("serve", errors.New("--max-concurrency must be at least 1"), false, stdout, stderr)
	}
	runStore := store.Store{Dir: *dir}
	if err := runStore.Check(); err != nil {
		return commandError("serve", "error", "internal", err, nil, false, stdout, stderr, exitInternal)
	}
	token, err := api.NewToken()
	if err != nil {
		return commandError("serve", "error", "internal", err, nil, false, stdout, stderr, exitInternal)
	}
	listener, err := api.Listen()
	if err != nil {
		return commandError("serve", "error", "internal", err, nil, false, stdout, stderr, exitInternal)
	}
	defer listener.Close()
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdin := serveStdin // read once, before the announcement a parent waits for
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		cancel()
	}()
	server := &api.Server{
		Token: token, Store: runStore, Hub: api.NewHub(),
		Validate: validateForAPI,
		Start:    starter(runStore, *concurrency),
	}
	tcp, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return commandError("serve", "error", "internal", errors.New("listener is not TCP"), nil, false, stdout, stderr, exitInternal)
	}
	line, err := json.Marshal(map[string]any{"port": tcp.Port, "token": token})
	if err != nil {
		return commandError("serve", "error", "internal", err, nil, false, stdout, stderr, exitInternal)
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", line); err != nil {
		return presentationError(err, stderr)
	}
	if err := server.Serve(serveCtx, listener); err != nil {
		return commandError("serve", "error", "internal", err, nil, false, stdout, stderr, exitInternal)
	}
	return exitOK
}

func validateForAPI(path string) (string, []graph.Diagnostic, error) {
	def, err := readDefinition(path)
	if err != nil {
		return "", nil, err
	}
	validation := graph.Validate(def)
	status := "valid"
	if validation.HasErrors() {
		status = "invalid"
	}
	return status, validation.Diagnostics, nil
}

// starter composes the same validation and execution as `agentplane run`, and
// persists every event to the run store before the hub (and so any client) can
// see it.
func starter(runStore store.Store, concurrency int) api.StartFunc {
	return func(ctx context.Context, graphPath string, inputs []string, persist func(events.Event) error, decisions runtime.DecisionProvider) error {
		def, graphBytes, err := readDefinitionBytes(graphPath)
		if err != nil {
			return &api.StartError{Status: http.StatusUnprocessableEntity, Message: err.Error()}
		}
		validation := graph.Validate(def)
		if validation.HasErrors() {
			return &api.StartError{Status: http.StatusUnprocessableEntity, Message: "graph is invalid", Diagnostics: validation.Diagnostics}
		}
		if err := validateInputSyntax(inputs); err != nil {
			return &api.StartError{Status: http.StatusBadRequest, Message: err.Error()}
		}
		values, valueValidation, err := parseInputs(def, inputs)
		if err != nil {
			return &api.StartError{Status: http.StatusBadRequest, Message: err.Error()}
		}
		plan, compiled := graph.CompileBindings(def)
		if !compiled.Valid() {
			return &api.StartError{Status: http.StatusUnprocessableEntity, Message: "graph bindings are invalid", Diagnostics: compiled.Diagnostics}
		}
		inputValidation := plan.ValidateGraphInputs(values)
		if valueValidation.HasErrors() || inputValidation.HasErrors() {
			all := append(append([]graph.Diagnostic{}, valueValidation.Diagnostics...), inputValidation.Diagnostics...)
			return &api.StartError{Status: http.StatusUnprocessableEntity, Message: "graph inputs are invalid", Diagnostics: all}
		}
		writer := runStore.NewWriter(graphBytes)
		sink := func(e events.Event) error {
			werr := writer.Append(e)
			if perr := persist(e); werr == nil {
				werr = perr
			}
			return werr
		}
		opts := runtime.Options{MaxConcurrency: concurrency, Decisions: decisions, Persist: sink}
		result, err := runtime.Run(ctx, def, values, serveExecutor(def), opts)
		if closeErr := writer.Close(); closeErr != nil && result.PersistError == nil {
			result.PersistError = closeErr
		}
		if err != nil {
			var preflight *runtime.PreflightError
			if errors.As(err, &preflight) {
				return &api.StartError{Status: http.StatusUnprocessableEntity, Message: "preflight failed", Diagnostics: preflight.Diagnostics.Diagnostics}
			}
			return err
		}
		return result.PersistError
	}
}

// serveExecutor mirrors `run` without an agent adapter: shell, gate and human
// graphs are served; an agent node fails with the adapter's own error.
func serveExecutor(def *graph.Definition) runtime.Executor {
	for _, node := range def.Nodes {
		if node.Type != "shell" || node.Loop != nil {
			return nodeExecutor{}
		}
	}
	return shell.Executor{}
}
