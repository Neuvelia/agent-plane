package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/store"
)

func TestRunMetadataNeverFailsAndAlwaysNamesTheRuntime(t *testing.T) {
	meta := runMetadataFor(filepath.Join(t.TempDir(), "g.yaml"))(context.Background())
	if meta.RuntimeVersion == "" {
		t.Fatal("runtime version missing")
	}
	if strings.ContainsAny(meta.RepoName+meta.RepoBranch+meta.RepoCommit, "\n\x1b") {
		t.Fatalf("control characters in %#v", meta)
	}
}

func TestRunMetadataCancelledContextDegradesToEmptyRepository(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	meta := runMetadataFor(filepath.Join(t.TempDir(), "g.yaml"))(ctx)
	if meta.RepoName != "" || meta.RepoBranch != "" || meta.RepoCommit != "" {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestCleanMetadataBoundsAndRejectsControlCharacters(t *testing.T) {
	if cleanMetadata("a\x1b[31m") != "" {
		t.Fatal("control character accepted")
	}
	if got := cleanMetadata(strings.Repeat("x", 500)); len(got) != maxMetadataField {
		t.Fatalf("len = %d", len(got))
	}
}

func TestSourceSuffix(t *testing.T) {
	cases := map[string]store.Summary{
		"":                    {},
		"  main@0123456789ab": {RepoBranch: "main", RepoCommit: "0123456789abcdef"},
		"  @0123456789ab":     {RepoCommit: "0123456789abcdef"},
		"  dev":               {RepoBranch: "dev"},
	}
	for want, in := range cases {
		if got := sourceSuffix(in); got != want {
			t.Errorf("sourceSuffix(%#v) = %q, want %q", in, got, want)
		}
	}
}

// The repository is the graph's, not the process working directory's.
func TestRunMetadataObservesTheGraphDirectoryNotTheWorkingDirectory(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := filepath.Join(t.TempDir(), "graph-repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(git, "-C", repo, "init", "-q", "-b", "trunk").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	meta := runMetadataFor(filepath.Join(repo, "sub", "g.yaml"))(context.Background())
	if meta.RepoName != "graph-repo" || meta.RepoBranch != "trunk" {
		t.Fatalf("metadata = %#v, want the graph repository", meta)
	}
}
