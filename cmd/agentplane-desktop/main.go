package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"agentplane/internal/desktop"
	"agentplane/internal/history"
	"agentplane/internal/process"
	"agentplane/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--internal-process-supervisor" {
		if len(os.Args) != 2 {
			os.Exit(125)
		}
		os.Exit(process.Supervise(os.Stdin, os.Stdout, os.Stderr))
	}
	ctx, stop := serviceContext()
	defer stop()
	if err := runDesktopService(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		// Never print a frame, graph, path, environment value or process output.
		fmt.Fprintln(os.Stderr, "desktop service: local service unavailable")
		os.Exit(1)
	}
}

func serviceContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// runDesktopService joins owned work on every return path, including protocol
// EOF and pipe errors. main may exit only after these defers have run.
func runDesktopService(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	stateDir, err := desktopStateDir(args)
	if err != nil {
		return err
	}
	registry, err := desktop.NewRegistry(stateDir)
	if err != nil {
		return err
	}
	defer registry.Close()
	handlers, err := desktop.NewRegistryHandlers(registry)
	if err != nil {
		return err
	}
	documents := desktop.NewDocumentStore(registry, stateDir)
	supervisorBinary, err := os.Executable()
	if err != nil {
		return err
	}
	preflight := desktop.NewPreflightServiceWithSupervisor(registry, documents, nil, supervisorBinary)
	for name, handler := range desktop.NewDocumentHandlers(documents, preflight) {
		handlers[name] = handler
	}
	historyDir, err := registry.HistoryDirectory()
	if err != nil {
		return err
	}
	historyStore, err := history.Open(ctx, historyDir)
	if err != nil {
		// Keep editing available, but every new Start must fail closed until a
		// private writable history store can be opened on a later service run.
		historyStore = nil
	}
	var historyWriter desktop.HistoryLaunchWriter
	if historyStore != nil {
		defer historyStore.Close()
		historyWriter = historyStore
	}
	runs := desktop.NewRunServiceWithHistory(ctx, preflight.PrepareStart, nil, historyWriter, err)
	for name, handler := range desktop.NewRunHandlers(runs) {
		handlers[name] = handler
	}
	for name, handler := range desktop.NewHistoryHandlers(historyStore, runs, registry) {
		handlers[name] = handler
	}
	server := desktop.NewServer(version.String(), handlers)
	return serveOwned(ctx, server, runs, input, output)
}

func serveOwned(ctx context.Context, server *desktop.Server, runs *desktop.RunService, input io.Reader, output io.Writer) error {
	defer runs.Close()
	return server.Serve(ctx, input, output)
}

func desktopStateDir(args []string) (string, error) {
	if len(args) == 2 && args[0] == "--state-dir" && filepath.IsAbs(args[1]) {
		return filepath.Clean(args[1]), nil
	}
	if len(args) != 0 {
		return "", fmt.Errorf("invalid arguments")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "AgentPlane"), nil
}
