//go:build darwin || linux

package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"agentplane/internal/gitstate"
	"agentplane/internal/process"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-process-supervisor" {
		os.Exit(process.Supervise(os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func TestSupervisedWorkspaceIdentityRejectsNonzeroGitExit(t *testing.T) {
	root := workspaceRepo(t)
	selected := filepath.Join(t.TempDir(), "git-wrapper")
	content := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n%%s\\n' %q %q\nexit 7\n", root, filepath.Join(root, ".git"))
	if err := os.WriteFile(selected, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, err := gitstate.WithExecutable(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = gitstate.WithSupervisor(ctx, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := inspectWorkspace(ctx, root); got.known {
		t.Fatalf("nonzero supervised Git exit established workspace identity: %+v", got)
	}
}
