package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
)

func TestPreflightRedactionBoundFailsLoudly(t *testing.T) {
	t.Run("more than 64 secrets", func(t *testing.T) {
		declared := map[string]string{}
		assigned := map[string]string{}
		resolver := &fakeResolver{values: map[string]string{}}
		for i := 0; i <= secrets.MaxSecrets; i++ {
			name, variable := fmt.Sprintf("s%d", i), fmt.Sprintf("REF_VAR_%d_CANARY", i)
			declared[name] = "env/" + variable
			assigned[variable] = name
			resolver.values["env/"+variable] = fmt.Sprintf("value-%d-%s", i, secretCanary)
		}
		def := secretDefinition(declared, map[string]map[string]string{"n": assigned})
		exec := newReceiver()
		diagnostics := requireSecretPreflightFailure(t, def, exec, resolver, "secret_redaction_bound_exceeded")
		if strings.Contains(diagnostics[0].Message, "value-") {
			t.Errorf("message must not hold a value: %q", diagnostics[0].Message)
		}
	})
	t.Run("one secret above 16 KiB", func(t *testing.T) {
		resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": strings.Repeat("x", secrets.MaxSecretBytes+1)}}
		diagnostics := requireSecretPreflightFailure(t, ghDefinition(), newReceiver(), resolver, "secret_redaction_bound_exceeded")
		if !strings.Contains(diagnostics[0].Message, `"gh_token"`) {
			t.Errorf("message must name the secret: %q", diagnostics[0].Message)
		}
	})
}

func TestPreflightKeepsStructuredRedactionBoundError(t *testing.T) {
	resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": strings.Repeat("x", secrets.MaxSecretBytes+1)}}
	sink := &persisted{}
	_, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), sink.options(resolver))
	if strings.Contains(err.Error(), "invalid redactions") {
		t.Errorf("a structured diagnostic must not gain a misleading prefix: %v", err)
	}
	if got := preflightDiagnostics(t, err); len(got) != 1 || got[0].Code != "secret_redaction_bound_exceeded" {
		t.Errorf("diagnostics = %+v", got)
	}
}

func TestPreflightTemplateCoversRawAndEncodedForms(t *testing.T) {
	sink := &persisted{}
	prepared, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), sink.options(ghResolver()))
	if err != nil {
		t.Fatal(err)
	}
	for _, form := range secrets.Forms(secretCanary) {
		got := redactAll(prepared.redactionTemplate.clone(), []byte("before "+string(form)+" after"), 0)
		if strings.Contains(string(got), string(form)) {
			t.Errorf("form %q survived the run template: %q", form, got)
		}
	}
}

func TestPreflightKeepsRedactEnvValues(t *testing.T) {
	sink := &persisted{}
	options := sink.options(ghResolver())
	options.LogRedactions = []string{"from-redact-env"}
	prepared, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), options)
	if err != nil {
		t.Fatal(err)
	}
	got := string(redactAll(prepared.redactionTemplate.clone(), []byte("a from-redact-env b "+secretCanary), 0))
	if strings.Contains(got, "from-redact-env") || strings.Contains(got, secretCanary) {
		t.Errorf("both sources must be redacted: %q", got)
	}
}

func TestPreflightGrantsOnlyAssignedNodes(t *testing.T) {
	sink := &persisted{}
	prepared, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), sink.options(ghResolver()))
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.grants) != 1 {
		t.Fatalf("grants for %d nodes, want only the assigned one", len(prepared.grants))
	}
	env, ok := prepared.grants["a"]
	if !ok || strings.Join(env.Names(), ",") != "GH_TOKEN" || env.Environ()[0] != "GH_TOKEN="+secretCanary {
		t.Errorf("grant for node a = %v", env.Names())
	}
	if rendered := fmt.Sprintf("%+v %#v", prepared.grants, prepared.grants); strings.Contains(rendered, secretCanary) {
		t.Errorf("formatting the grants leaked the value: %s", rendered)
	}
}

// loopReceiver is an observed secret receiver that accepts any loop node
// configuration, so a test can enroll a unit without a real Agent adapter.
type loopReceiver struct{ observedReceiver }

func (*loopReceiver) ValidateNodeConfig(string, graph.NodeDefinition) error { return nil }

func (*loopReceiver) PreflightNode(context.Context, string, graph.NodeDefinition) error { return nil }

// unitDefinitionWithScope adds an enrolled loop Agent unit whose intended scope
// holds the given allowed_paths entry to the shell graph that receives a secret.
func unitDefinitionWithScope(entry string) *graph.Definition {
	def := ghDefinition()
	def.Units = []string{"work"}
	def.Nodes["work"] = graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"adapter": "fake", "task": "edit", "allowed_paths": []string{entry}}, Outputs: map[string]graph.PortDefinition{"raw_metadata": {Type: graph.ValueTypeJSON, Required: true}}, Loop: &graph.LoopDefinition{MaxAttempts: 2, Gate: graph.LoopGateDefinition{Inputs: map[string]graph.PortDefinition{"candidate": {Type: graph.ValueTypeJSON, Required: true}}, Config: graph.NodeConfig{"check_id": "scope", "kind": "changed_files_scope", "input": "candidate", "mode": "report", "allowed_paths": []string{entry}}}, Bindings: []graph.LoopBinding{{From: "candidate.outputs.raw_metadata", To: "gate.inputs.candidate"}}}}
	return def
}

// The unit scope is redacted with the final template, the one that already
// holds the resolved secrets, not with the --redact-env-only template.
func TestPreflightUnitScopeIsRedactedWithResolvedSecrets(t *testing.T) {
	sink := &persisted{}
	prepared, err := preflightRun(context.Background(), unitDefinitionWithScope("src/"+secretCanary+"/**"), nil, &loopReceiver{*newObservedReceiver()}, sink.options(ghResolver()))
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.scopeRedacted["work"] {
		t.Error("a scope holding a secret must be marked redacted")
	}
	for _, entry := range prepared.scopes["work"] {
		for _, form := range secrets.Forms(secretCanary) {
			if strings.Contains(entry, string(form)) {
				t.Errorf("unit scope entry %q holds secret form %q", entry, form)
			}
		}
	}
}
