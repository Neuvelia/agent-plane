package main

import (
	"os"
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
)

// The Claude example is a template: only its placeholder workspace may be a
// configuration error, and environment_refs must not name a variable the
// adapter reserves (HOME), which would fail every run with claude_environment.
func TestClaudeExampleOnlyReportsItsPlaceholderWorkspace(t *testing.T) {
	data, err := os.ReadFile("../../docs/examples/claude-agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "environment_refs: [ANTHROPIC_API_KEY, PATH]") {
		t.Error("example environment_refs must be exactly ANTHROPIC_API_KEY and PATH")
	}
	def, err := yamlgraph.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var errs, warns []string
	for _, d := range validateGraph(def).Diagnostics {
		line := d.Code + " " + d.Location.Path
		if d.Severity == graph.SeverityError {
			errs = append(errs, line)
		} else {
			warns = append(warns, line)
		}
	}
	wantErrs := []string{nodeConfigCode + " nodes.edit.config"}
	wantWarns := []string{
		"W_DECLARED_OUTPUT_UNUSED nodes.edit.outputs.changed_files",
		"W_DECLARED_OUTPUT_UNUSED nodes.edit.outputs.summary",
	}
	if strings.Join(errs, "|") != strings.Join(wantErrs, "|") {
		t.Errorf("errors %v, want %v", errs, wantErrs)
	}
	if strings.Join(warns, "|") != strings.Join(wantWarns, "|") {
		t.Errorf("warnings %v, want %v", warns, wantWarns)
	}
}
