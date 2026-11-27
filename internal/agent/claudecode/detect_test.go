package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

// roadmap: F1.2b-AC4
func TestDetectProbesRunInAPrivateHome(t *testing.T) {
	testenv.RequireDarwinOrLinux(t, "the Claude Code adapter")
	records := t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s' \"$HOME\" >" + records + "/home\n" +
		"printf '%s' \"$(pwd -P)\" >" + records + "/pwd\n" +
		": >\"$HOME/x\"\n: >./x\n" +
		"case \"$*\" in *--version*) echo '2.1.332 (Claude Code)';; *--help*) echo '" + helpFlags + "';; esac\n"
	binary := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	caller := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(caller); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	report, err := Adapter{Workspaces: testWorkspaces{}, Binary: binary}.Detect(context.Background())
	if err != nil || !report.Available {
		t.Fatalf("detect=%+v err=%v", report, err)
	}
	if _, err = os.Stat(filepath.Join(caller, "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a probe wrote into the caller's working directory: %v", err)
	}
	home, _ := os.ReadFile(filepath.Join(records, "home"))
	pwd, _ := os.ReadFile(filepath.Join(records, "pwd"))
	if len(home) == 0 || string(home) != string(pwd) || !strings.HasPrefix(filepath.Base(string(home)), "agentplane-claude-") {
		t.Fatalf("home=%q pwd=%q: probes must run in the private home", home, pwd)
	}
	if _, err = os.Stat(string(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private home was not removed after Detect: %v", err)
	}
}
