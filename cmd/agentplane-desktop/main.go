package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"agentplane/internal/desktop"
	"agentplane/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	server := desktop.NewServer(version.String(), nil)
	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		// Never print a frame, graph, path, environment value or process output.
		fmt.Fprintln(os.Stderr, "desktop service: protocol I/O failed")
		os.Exit(1)
	}
}
