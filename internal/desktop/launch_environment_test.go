package desktop

import (
	"strings"
	"testing"

	"agentplane/internal/graph"
)

func TestCaptureLaunchEnvironmentPinsNamedValuesAndSealsChanges(t *testing.T) {
	t.Setenv("AGENTPLANE_CODEX_REF", "agent-secret")
	t.Setenv("AGENTPLANE_SHELL_REF", "shell-secret")
	definition := &graph.Definition{Nodes: map[string]graph.NodeDefinition{
		"agent": {Type: "agent", Config: graph.NodeConfig{"environment_refs": []any{"AGENTPLANE_CODEX_REF"}}},
		"shell": {Type: "shell", Config: graph.NodeConfig{"env_allowlist": []any{"AGENTPLANE_SHELL_REF"}, "env": map[string]any{"STATIC": "literal-private"}}},
	}}
	first, err := CaptureLaunchEnvironment(definition)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("01234567890123456789012345678901")
	sealed, err := first.Seal(key)
	if err != nil || !strings.HasPrefix(sealed, "hmac-sha256:") || strings.Contains(sealed, "secret") {
		t.Fatalf("seal = %q, %v", sealed, err)
	}
	t.Setenv("AGENTPLANE_CODEX_REF", "changed")
	t.Setenv("AGENTPLANE_SHELL_REF", "changed")
	if first.CodexValues()["AGENTPLANE_CODEX_REF"] != "agent-secret" ||
		first.ShellValues()["AGENTPLANE_SHELL_REF"] != "shell-secret" {
		t.Fatal("captured values followed ambient changes")
	}
	second, err := CaptureLaunchEnvironment(definition)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := second.Seal(key)
	if err != nil || changed == sealed {
		t.Fatalf("changed values reused seal: %q / %q / %v", sealed, changed, err)
	}
	otherKey, err := first.Seal([]byte("11111111111111111111111111111111"))
	if err != nil || otherKey == sealed {
		t.Fatalf("distinct key reused seal: %q / %q / %v", sealed, otherKey, err)
	}
	redactions := first.Redactions()
	if len(redactions) != 3 || redactions[0] != "agent-secret" || redactions[1] != "literal-private" || redactions[2] != "shell-secret" {
		t.Fatalf("redactions = %#v", redactions)
	}
}

func TestCaptureLaunchEnvironmentRejectsMissingInvalidAndOversizeRefs(t *testing.T) {
	t.Setenv("AGENTPLANE_PRESENT", "ok")
	for _, cfg := range []graph.NodeConfig{
		{"environment_refs": []any{"AGENTPLANE_MISSING_UNSET_9567"}},
		{"environment_refs": []any{"BAD-NAME"}},
		{"environment_refs": []any{3}},
		{"environment_refs": []any{"AGENTPLANE_PRESENT", "AGENTPLANE_MISSING_UNSET_9567"}},
	} {
		definition := &graph.Definition{Nodes: map[string]graph.NodeDefinition{"a": {Type: "agent", Config: cfg}}}
		if _, err := CaptureLaunchEnvironment(definition); err == nil || strings.Contains(err.Error(), "ok") {
			t.Fatalf("accepted invalid ref or leaked value: %#v / %v", cfg, err)
		}
	}
	missingShell := &graph.Definition{Nodes: map[string]graph.NodeDefinition{
		"s": {Type: "shell", Config: graph.NodeConfig{"env_allowlist": []string{"AGENTPLANE_MISSING_UNSET_9567"}}},
	}}
	if _, err := CaptureLaunchEnvironment(missingShell); err == nil {
		t.Fatal("missing Shell allowlist variable was accepted")
	}
	t.Setenv("AGENTPLANE_LONG", strings.Repeat("x", 1<<20))
	definition := &graph.Definition{Nodes: map[string]graph.NodeDefinition{
		"s": {Type: "shell", Config: graph.NodeConfig{"env_allowlist": []any{"AGENTPLANE_LONG"}}},
	}}
	if _, err := CaptureLaunchEnvironment(definition); err == nil || strings.Contains(err.Error(), "xxxxx") {
		t.Fatalf("accepted oversized environment: %v", err)
	}
	refs := make([]string, 65)
	for i := range refs {
		refs[i] = "AGENTPLANE_MANY_" + string(rune('A'+i/26)) + string(rune('A'+i%26))
		t.Setenv(refs[i], "v")
	}
	definition.Nodes["s"] = graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"env_allowlist": refs}}
	if _, err := CaptureLaunchEnvironment(definition); err == nil {
		t.Fatal("more than 64 named variables were accepted")
	}
}

func TestLaunchEnvironmentDoesNotExposeMutableMapOrAcceptWeakKey(t *testing.T) {
	t.Setenv("AGENTPLANE_PINNED", "private")
	definition := &graph.Definition{Nodes: map[string]graph.NodeDefinition{
		"s": {Type: "shell", Config: graph.NodeConfig{"env_allowlist": []string{"AGENTPLANE_PINNED"}}},
	}}
	snapshot, err := CaptureLaunchEnvironment(definition)
	if err != nil {
		t.Fatal(err)
	}
	values := snapshot.ShellValues()
	values["AGENTPLANE_PINNED"] = "mutated"
	if snapshot.ShellValues()["AGENTPLANE_PINNED"] != "private" {
		t.Fatal("mutable map escaped snapshot")
	}
	if _, err := snapshot.Seal(nil); err == nil {
		t.Fatal("accepted unkeyed environment seal")
	}
}
