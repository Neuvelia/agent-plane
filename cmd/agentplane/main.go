package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"agentplane/internal/domain/run"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	// Let writes to closed stdout/stderr pipes return EPIPE so the CLI can
	// report its documented output-error status and cancel active work.
	signal.Ignore(syscall.SIGPIPE)
	// The typed cause lets the runtime report reason "interrupt" (ADR-0028).
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		if _, ok := <-signals; ok {
			cancel(run.CancelCause{Reason: run.CancelInterrupt})
		}
	}()
	return runCommand(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
