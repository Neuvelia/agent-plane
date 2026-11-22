package events

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func secretNameResult(kind CheckKind, name string) CheckResult {
	return CheckResult{Version: 1, CheckID: string(kind), Kind: kind, Verdict: CheckRed, Reason: "secret_found_in_workspace",
		Scope:    CheckScope{CheckedInputs: []string{"changed_files"}},
		Evidence: []GateEvidence{{Kind: EvidenceKind(kind), Source: EvidenceSource(kind), Path: "dir/a.txt", SecretName: name}}}
}

func TestGateEvidenceSecretNameRoundTrips(t *testing.T) {
	e := policyEvent(SchemaVersion, GateCheck, secretNameResult(CheckSecretInWorkspace, "api_token"))
	data, err := json.Marshal(e)
	if err != nil || !strings.Contains(string(data), `"secret_name":"api_token"`) {
		t.Fatalf("marshal: %v %s", err, data)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(got.Payload, e.Payload) {
		t.Fatalf("round trip: %v", err)
	}
}

func TestGateEvidenceSecretNameRejectedOutsideItsKindAndSchema(t *testing.T) {
	if _, err := json.Marshal(policyEvent(SchemaPolicy-1, GateCheck, secretNameResult(CheckSecretInWorkspace, "api_token"))); err == nil {
		t.Fatal("secret_name must require the policy schema")
	}
	for _, kind := range []CheckKind{CheckExecutionPolicy, CheckChangedFilesScope, CheckReportedFileScope} {
		p := secretNameResult(kind, "api_token")
		if kind == CheckChangedFilesScope {
			p.ChangedFiles = &ChangedFilesScopeDetails{Mode: "deny", AllowedPaths: []string{}, BaselineHead: "h"}
		}
		if err := p.Validate(); err == nil {
			t.Fatalf("%s must reject secret_name", kind)
		}
	}
}

func TestGateEvidenceSecretNameMustFollowTheNameGrammar(t *testing.T) {
	for _, name := range []string{"API_TOKEN", "1abc", "a-b", "a b", "a\x00", "é", strings.Repeat("a", 65)} {
		if err := secretNameResult(CheckSecretInWorkspace, name).Validate(); err == nil {
			t.Fatalf("name %q must be rejected", name)
		}
	}
	if err := secretNameResult(CheckSecretInWorkspace, strings.Repeat("a", 64)).Validate(); err != nil {
		t.Fatalf("64 characters is the bound: %v", err)
	}
}

func TestGateEvidenceSecretNameRejectsNullOnTheWire(t *testing.T) {
	var e GateEvidence
	if err := json.Unmarshal([]byte(`{"kind":"secret_in_workspace","source":"secret_in_workspace","secret_name":null}`), &e); err == nil {
		t.Fatal("null secret_name must be rejected")
	}
}

func TestCloneGateKeepsSecretName(t *testing.T) {
	p := secretNameResult(CheckSecretInWorkspace, "api_token")
	c := CloneGate(p)
	c.Evidence[0].SecretName = "other"
	if p.Evidence[0].SecretName != "api_token" {
		t.Fatal("clone shares evidence with the original")
	}
	if got := ClonePayload(p).(GateCheckPayload).Evidence[0].SecretName; got != "api_token" {
		t.Fatalf("ClonePayload dropped secret_name: %q", got)
	}
}

func TestEvidenceWithoutSecretNameKeepsItsBytes(t *testing.T) {
	data, err := json.Marshal(GateEvidence{Kind: "command", Source: "command"})
	if err != nil || strings.Contains(string(data), "secret_name") {
		t.Fatalf("absent secret_name must not be written: %s %v", data, err)
	}
}
