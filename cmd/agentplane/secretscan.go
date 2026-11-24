package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"agentplane/internal/adapter/secretscan"
	"agentplane/internal/domain/secrets"
	"agentplane/internal/gitstate"
	"agentplane/internal/runtime"
)

// gitWorkspaceScanner joins the two observation sources of F3.8 requirement 13:
// internal/gitstate says which files a node changed (ignored files included,
// because a leaked token usually lands in an ignored .env) and
// internal/adapter/secretscan reads them. It is composed here because adapters
// never import each other. Any failure to obtain Git evidence is reported as
// secrets.ErrUnobservable: the scan is then "unobserved", never "clean".
type gitWorkspaceScanner struct {
	files secretscan.Scanner
}

// configureSecretScan gives a run the Git-backed scanner. Nodes without a
// secret grant never reach it, so a graph without secrets does no Git work.
func configureSecretScan(options *runtime.Options) {
	options.WorkspaceScanner = gitWorkspaceScanner{}
}

// Begin captures the Git state of root. The root must be exactly the top of a
// worktree with a HEAD (an isolated worktree, or a Shell cwd at the repository
// root); a subdirectory is unobserved in v0.
func (g gitWorkspaceScanner) Begin(ctx context.Context, root string) (runtime.ScanWindow, error) {
	absolute, err := absoluteScanRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", secrets.ErrUnobservable, err)
	}
	baseline, err := gitstate.Capture(ctx, absolute)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", secrets.ErrUnobservable, err)
	}
	return gitScanWindow{baseline: baseline, files: g.files}, nil
}

// absoluteScanRoot resolves the empty root to the runner's working directory;
// the environment is read here, never in the runtime.
func absoluteScanRoot(root string) (string, error) {
	if root == "" {
		return os.Getwd()
	}
	return filepath.Abs(root)
}

type gitScanWindow struct {
	baseline gitstate.Baseline
	files    secretscan.Scanner
}

// Scan reads the files changed since Begin and looks for the granted values.
func (w gitScanWindow) Scan(ctx context.Context, targets []secrets.ScanTarget) (secrets.ScanReport, error) {
	changed, err := gitstate.Changed(ctx, w.baseline)
	if err != nil {
		return secrets.ScanReport{}, fmt.Errorf("%w: %w", secrets.ErrUnobservable, err)
	}
	return w.files.ScanFiles(ctx, w.baseline.Root, changed, targets, secrets.DefaultScanLimits())
}
