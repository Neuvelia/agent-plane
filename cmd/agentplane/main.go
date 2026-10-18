package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	// Let writes to closed stdout/stderr pipes return EPIPE so the CLI can
	// report its documented output-error status and cancel active work.
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCommand(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
