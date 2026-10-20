package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"agentplane/internal/desktop"
	"agentplane/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	stateDir, err := desktopStateDir(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "desktop service: invalid startup options")
		os.Exit(1)
	}
	registry, err := desktop.NewRegistry(stateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "desktop service: local registry unavailable")
		os.Exit(1)
	}
	defer registry.Close()
	handlers, err := desktop.NewRegistryHandlers(registry)
	if err != nil {
		fmt.Fprintln(os.Stderr, "desktop service: handlers unavailable")
		os.Exit(1)
	}
	server := desktop.NewServer(version.String(), handlers)
	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		// Never print a frame, graph, path, environment value or process output.
		fmt.Fprintln(os.Stderr, "desktop service: protocol I/O failed")
		os.Exit(1)
	}
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
