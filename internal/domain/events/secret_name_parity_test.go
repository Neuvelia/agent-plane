package events_test

import (
	"strings"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/secrets"
)

// Placement: domain/events may not import domain/secrets (it owns its own copy
// of the name grammar, see validEvidenceSecretName), so the parity of the two
// copies is pinned from an external test package of events. domain -> domain is
// a legal edge under AGENTS.md R1, no production import is added, and the test
// sits next to the grammar it guards instead of in a catch-all package.
//
// secrets.ValidName is the reference; every events path that accepts a secret
// name must agree with it on every input below.

func parityInputs() map[string]string {
	return map[string]string{
		"empty":          "",
		"single":         "a",
		"upper":          "A",
		"leading digit":  "1a",
		"underscore":     "a_b",
		"hyphen":         "a-b",
		"64 chars":       strings.Repeat("a", 64),
		"65 chars":       strings.Repeat("a", 65),
		"utf8":           "é",
		"utf8 suffix":    "aé",
		"nul":            "a\x00",
		"trailing nl":    "a\n",
		"leading nl":     "\na",
		"space":          "a b",
		"leading space":  " a",
		"leading under":  "_a",
		"digits":         "a0123456789",
		"mixed case":     "aB",
		"dot":            "a.b",
		"slash":          "a/b",
		"nul only":       "\x00",
		"64 with digits": "a" + strings.Repeat("9", 63),
	}
}

func policyAcceptsName(name string) bool {
	p := events.PolicyEvaluatedPayload{
		Mode:          events.PolicyModeAudit,
		ScopeRootKind: events.PolicyRootWorktree,
		ScopeRoot:     "/cache/agentplane/wt",
		Permissions: []events.PolicyPermission{
			{Name: events.PolicyPermissionCommands, Requested: false, Value: []string{}, State: events.PolicyStateUnobserved},
		},
		Secrets: []events.PolicySecret{{Name: name, Provider: "env", Env: "GH_TOKEN", Resolved: true}},
	}
	return p.Validate() == nil
}

func gateEvidenceAcceptsName(name string) bool {
	r := events.CheckResult{Version: 1, CheckID: string(events.CheckSecretInWorkspace), Kind: events.CheckSecretInWorkspace,
		Verdict: events.CheckRed, Reason: "secret_found_in_workspace",
		Scope: events.CheckScope{CheckedInputs: []string{"changed_files"}},
		Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckSecretInWorkspace),
			Source: events.EvidenceSource(events.CheckSecretInWorkspace), Path: "dir/a.txt", SecretName: name}}}
	return r.Validate() == nil
}

func TestPolicySecretNameGrammarMatchesDomainSecrets(t *testing.T) {
	if !policyAcceptsName("a") {
		t.Fatal("fixture is invalid: a legal name must be accepted")
	}
	for label, name := range parityInputs() {
		if got, want := policyAcceptsName(name), secrets.ValidName(name); got != want {
			t.Errorf("%s (%q): policy_evaluated accepts=%v, secrets.ValidName=%v", label, name, got, want)
		}
	}
}

// An empty GateEvidence.SecretName means "absent". Whether an absent name is
// acceptable depends on the verdict (a red secret_in_workspace result must name
// its secret since the workspace scan wiring), so that one input is skipped and
// every other input must match ValidName.
func TestGateEvidenceSecretNameGrammarMatchesDomainSecrets(t *testing.T) {
	if !gateEvidenceAcceptsName("a") {
		t.Fatal("fixture is invalid: a legal name must be accepted")
	}
	for label, name := range parityInputs() {
		if name == "" {
			continue
		}
		if got, want := gateEvidenceAcceptsName(name), secrets.ValidName(name); got != want {
			t.Errorf("%s (%q): gate_check evidence accepts=%v, secrets.ValidName=%v", label, name, got, want)
		}
	}
}
