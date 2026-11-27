package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode"

	"agentplane/internal/gitstate"
	"agentplane/internal/runtime"
	"agentplane/internal/version"
)

// maxMetadataField bounds each recorded repository string.
const maxMetadataField = 200

// runMetadataFor returns the run_started metadata provider for a graph file. It
// observes the repository containing the graph's directory, not the process
// working directory: a served run (the desktop sidecar) or a CLI run of a graph
// from another repository would otherwise record the wrong repository (F2.9,
// F3.6). Every Git failure (no Git, not a repository, an unborn HEAD, a timeout)
// leaves its field empty; it never returns an error.
func runMetadataFor(graphPath string) func(context.Context) runtime.RunMetadata {
	return func(ctx context.Context) runtime.RunMetadata {
		meta := runtime.RunMetadata{RuntimeVersion: version.String()}
		dir, err := filepath.Abs(filepath.Dir(graphPath))
		if err != nil {
			return meta
		}
		return observeRepository(ctx, dir, meta)
	}
}

func observeRepository(ctx context.Context, dir string, meta runtime.RunMetadata) runtime.RunMetadata {
	top := gitLine(ctx, dir, "rev-parse", "--show-toplevel")
	if top == "" {
		return meta
	}
	meta.RepoName = cleanMetadata(filepath.Base(top))
	meta.RepoID = repoID(top)
	meta.RepoCommit = gitLine(ctx, dir, "rev-parse", "HEAD")
	meta.RepoBranch = gitLine(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	return meta
}

func gitLine(ctx context.Context, dir string, args ...string) string {
	result, err := gitstate.Run(ctx, dir, gitstate.RunOptions{StdoutLimit: 4096}, args...)
	if err != nil {
		return ""
	}
	return cleanMetadata(strings.TrimSpace(string(result.Stdout)))
}

// cleanMetadata drops control characters and bounds the length so repository
// controlled text cannot inject terminal sequences or bloat the log.
func cleanMetadata(s string) string {
	for _, r := range s {
		if unicode.IsControl(r) {
			return ""
		}
	}
	if len(s) > maxMetadataField {
		s = s[:maxMetadataField]
	}
	return strings.ToValidUTF8(s, "")
}

// repoID identifies a work tree by a hash of its absolute path, so two
// repositories with the same base name stay apart in the run list without the
// path itself entering the log (F2.9). It is a pseudonym, not anonymisation:
// a guessable path can be recovered by hashing candidates (ADR-0044).
func repoID(top string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(top)))
	return hex.EncodeToString(sum[:8])
}
