package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		glob, name string
		want       bool
	}{
		{"internal/runtime/**", "internal/runtime/loop/attempt.go", true},
		{"internal/runtime/**", "internal/runtimex/a.go", false},
		{"internal/*/AGENTS.md", "internal/shell/AGENTS.md", true},
		{"internal/*/AGENTS.md", "internal/agent/codex/AGENTS.md", false},
		{"**/*_test.go", "a_test.go", true},
		{"**/*_test.go", "internal/gate/check_test.go", true},
		{"**/*_test.go", "internal/gate/check.go", false},
		{".github/**", ".github/workflows/ci.yml", true},
		{"docs/graph-spec.md", "docs/graph-spec.md", true},
		{"docs/graph-spec.md", "docs/graph-specxmd", false},
	}
	for _, test := range tests {
		if got := matchGlob(test.glob, test.name); got != test.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", test.glob, test.name, got, test.want)
		}
	}
}

func TestParseRule(t *testing.T) {
	source := "---\ninclude:\n  - \"internal/runtime/**\"\n  - internal/app/**\n---\n\n# Runtime\n\nRules.\n"
	parsed, err := parseRule("r.md", source)
	if err != nil {
		t.Fatalf("parseRule: %v", err)
	}
	want := []string{"internal/runtime/**", "internal/app/**"}
	if !reflect.DeepEqual(parsed.include, want) || parsed.body != "# Runtime\n\nRules." {
		t.Fatalf("got %+v", parsed)
	}
}

func TestParseRuleRejectsMissingFrontMatter(t *testing.T) {
	for name, source := range map[string]string{
		"no front matter": "# Rules\n",
		"unterminated":    "---\ninclude:\n  - a/**\n",
		"no globs":        "---\ninclude:\n---\nbody\n",
	} {
		if _, err := parseRule(name, source); !errors.Is(err, errInvalidRule) {
			t.Errorf("%s: got %v, want errInvalidRule", name, err)
		}
	}
}

func TestSelectRulesKeepsOnlyMatchingRules(t *testing.T) {
	rules := []rule{
		{path: "runtime.md", include: []string{"internal/runtime/**"}},
		{path: "desktop.md", include: []string{"desktop/**"}},
	}
	selected := selectRules(rules, []string{"internal/runtime/run.go", "README.md"})
	if len(selected) != 1 || selected[0].path != "runtime.md" {
		t.Fatalf("got %+v", selected)
	}
}

func TestAgentsPathsIncludesRootAndAncestorsOfChangedFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"AGENTS.md", "internal/agent/AGENTS.md", "internal/agent/codex/AGENTS.md", "desktop/AGENTS.md"} {
		writeFile(t, root, name, "x")
	}
	got := agentsPaths(root, []string{"internal/agent/codex/run.go"})
	want := []string{"AGENTS.md", "internal/agent/AGENTS.md", "internal/agent/codex/AGENTS.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
