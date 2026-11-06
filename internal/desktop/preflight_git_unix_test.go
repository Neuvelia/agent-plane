//go:build darwin || linux

package desktop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"agentplane/internal/gitstate"
)

func TestInstalledGitCheckUsesConfiguredExecutable(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		command := exec.Command("/usr/bin/git", args...)
		command.Dir = root
		command.Env = gitstate.InspectionEnvironment()
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("initialize repository: %v: %s", err, output)
		}
	}
	selected := filepath.Join(t.TempDir(), "git-wrapper")
	if err := os.WriteFile(selected, []byte("#!/bin/sh\nexec /usr/bin/git \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	check := installedCapabilities{}
	if err := check.CheckGit(context.Background(), root, selected); err != nil {
		t.Fatalf("configured alternate Git was rejected: %v", err)
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	if err := check.CheckGit(context.Background(), root, selected); err == nil {
		t.Fatal("missing configured Git fell back to default")
	}
}
