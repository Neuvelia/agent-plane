package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestCheckGitArgsRefusesPathsOutsideTheCheckout(t *testing.T) {
	tests := map[string][]string{
		"implicit no-index":      {"diff", "/etc/passwd", "/dev/null"},
		"proc environ":           {"diff", "--", "/proc/self/environ", "README.md"},
		"climbing relative path": {"diff", "../../.runner", "README.md"},
		"climbing inside a path": {"show", "HEAD", "--", "docs/../../x"},
		"home path":              {"diff", "~/.ssh/id_rsa", "README.md"},
		"option value":           {"diff", "--relative=/etc"},
		"short option value":     {"diff", "-O/etc/passwd"},
		"short option climbing":  {"diff", "-O../x"},
		"explicit no-index":      {"diff", "--no-index", "a", "b"},
		"output file":            {"log", "--output=x"},
		"external diff":          {"diff", "--ext-diff"},
		"textconv":               {"show", "--textconv", "HEAD"},
		"other subcommand":       {"config", "--list"},
		"global option first":    {"-C", "/", "diff"},
		"no subcommand":          {},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if err := checkGitArgs(args); !errors.Is(err, errGitRefused) {
				t.Fatalf("checkGitArgs(%q) = %v, want errGitRefused", args, err)
			}
		})
	}
}

func TestCheckGitArgsAllowsReviewCommands(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tests := [][]string{
		{"diff", "--find-renames", sha + "..." + sha},
		{"diff", "--find-renames", sha, sha, "--", "internal/runtime/run.go"},
		{"diff", sha + ".." + sha, "--", "docs/zażółć gęślą.md"},
		{"show", sha + ":internal/runtime/run.go"},
		{"log", "--oneline", "-n", "20", sha},
		{"ls-files", "internal/"},
	}
	for _, args := range tests {
		if err := checkGitArgs(args); err != nil {
			t.Errorf("checkGitArgs(%q) = %v, want nil", args, err)
		}
	}
}

func TestGuardedGitDoesNotRunRefusedCommands(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	var out strings.Builder
	streams := gitStreams{stdin: strings.NewReader(""), stdout: &out, stderr: &out}
	if err := guardedGit(gitPath, []string{"diff", "/etc/hostname", "/dev/null"}, streams); !errors.Is(err, errGitRefused) {
		t.Fatalf("got %v, want errGitRefused", err)
	}
	if out.Len() != 0 {
		t.Fatalf("git ran and printed %q", out.String())
	}
	if err := guardedGit(gitPath, []string{"log", "-n", "1", "--oneline"}, streams); err != nil {
		t.Fatalf("an allowed command failed: %v", err)
	}
}
