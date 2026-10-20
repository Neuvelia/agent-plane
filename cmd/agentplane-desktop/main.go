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
	"agentplane/internal/version"
)

func main() {
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
	preflight := desktop.NewPreflightService(registry, documents, nil)
	for name, handler := range desktop.NewDocumentHandlers(documents, preflight) {
		handlers[name] = handler
	}
	runs := desktop.NewRunService(ctx, preflight.PrepareStart, nil)
	for name, handler := range desktop.NewRunHandlers(runs) {
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
