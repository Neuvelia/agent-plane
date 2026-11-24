package events

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func policyEvent(version int, typ Type, payload Payload) Event {
	e := Event{SchemaVersion: version, Type: typ, Timestamp: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Sequence: 5, RunID: "run-1", GraphID: "g", GraphVersion: "sha256-v0:x", NodeID: "worker", NodeRunID: "nr", Payload: payload}
	return e
}

func evaluatedPayload() PolicyEvaluatedPayload {
	return PolicyEvaluatedPayload{
		Mode:          PolicyModeAudit,
		Applied:       false,
		ScopeRootKind: PolicyRootWorktree,
		ScopeRoot:     "/cache/agentplane/wt",
		Permissions: []PolicyPermission{
			{Name: PolicyPermissionCommands, Requested: true, Value: []string{"go"}, State: PolicyStateUnobserved},
			{Name: PolicyPermissionNetwork, Requested: true, Value: []string{"off"}, State: PolicyStatePrevented},
			{Name: PolicyPermissionPathsWrite, Requested: true, Value: []string{"out/**"}, State: PolicyStateContained},
			{Name: PolicyPermissionShell, Requested: false, Value: []string{}, State: PolicyStateDelegated},
		},
		Secrets:         []PolicySecret{{Name: "gh_token", Provider: "env", Env: "GH_TOKEN", Resolved: true}},
		RedactionValues: 7,
	}
}

func violationPayload() PolicyViolationPayload {
	return PolicyViolationPayload{Permission: PolicyPermissionPathsWrite, Reason: PolicyReasonOutOfScopeWrite, Outcome: PolicyOutcomeRecorded, Evidence: []string{"a.txt", "dir/b.txt"}, Omitted: 3}
}

func policyFacts() []Event {
	v := policyEvent(SchemaVersion, PolicyViolation, violationPayload())
	v.AttemptID = "att"
	return []Event{policyEvent(SchemaVersion, PolicyEvaluated, evaluatedPayload()), v}
}

func TestPolicyEventsRequireNewSchema(t *testing.T) {
	if SchemaPolicy != SchemaVersion {
		t.Fatalf("policy facts must be introduced by the current schema: %d vs %d", SchemaPolicy, SchemaVersion)
	}
	for _, below := range []int{SchemaPolicy - 1, 11} {
		for _, e := range policyFacts() {
			e.SchemaVersion = below
			if _, err := json.Marshal(e); err == nil {
				t.Fatalf("%s must not be serialized under schema %d", e.Type, below)
			}
			e.SchemaVersion = SchemaPolicy
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatalf("%s under schema %d: %v", e.Type, SchemaPolicy, err)
			}
			old := strings.Replace(string(data), fmt.Sprintf(`"event_schema_version":%d`, SchemaPolicy), fmt.Sprintf(`"event_schema_version":%d`, below), 1)
			var got Event
			if json.Unmarshal([]byte(old), &got) == nil {
				t.Fatalf("%s must not decode under schema %d", e.Type, below)
			}
		}
	}
	e := policyFacts()[0]
	e.SchemaVersion = SchemaPolicy + 1
	if _, err := json.Marshal(e); err == nil {
		t.Fatal("a schema newer than the runner is unsupported")
	}
}

func TestPolicyEventsRoundTripAndBounds(t *testing.T) {
	for _, e := range policyFacts() {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("%s: %v", e.Type, err)
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("%s: %v\n%s", e.Type, err, data)
		}
		if !reflect.DeepEqual(got, e) {
			t.Fatalf("%s round trip differs:\n%#v\n%#v", e.Type, got, e)
		}
		again, _ := json.Marshal(got)
		if string(again) != string(data) {
			t.Fatalf("%s re-encoding differs", e.Type)
		}
	}
	long := func(n int) string { return strings.Repeat("a", n) }
	secrets := func(n int) []PolicySecret {
		out := make([]PolicySecret, n)
		for i := range out {
			out[i] = PolicySecret{Name: fmt.Sprintf("s%03d", i), Provider: "env", Env: fmt.Sprintf("V%03d", i), Resolved: true}
		}
		return out
	}
	evidence := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("f%03d", i)
		}
		return out
	}
	evalWith := func(f func(*PolicyEvaluatedPayload)) Event {
		p := evaluatedPayload()
		f(&p)
		return policyEvent(SchemaVersion, PolicyEvaluated, p)
	}
	violWith := func(f func(*PolicyViolationPayload)) Event {
		p := violationPayload()
		f(&p)
		e := policyEvent(SchemaVersion, PolicyViolation, p)
		return e
	}
	bad := map[string]Event{
		"unknown mode":        evalWith(func(p *PolicyEvaluatedPayload) { p.Mode = "strict" }),
		"unknown root kind":   evalWith(func(p *PolicyEvaluatedPayload) { p.ScopeRootKind = "tmp" }),
		"relative scope root": evalWith(func(p *PolicyEvaluatedPayload) { p.ScopeRoot = "wt" }),
		"nul scope root":      evalWith(func(p *PolicyEvaluatedPayload) { p.ScopeRoot = "/a\x00b" }),
		"scope root too long": evalWith(func(p *PolicyEvaluatedPayload) { p.ScopeRoot = "/" + long(MaxWorkspaceLocalPath) }),
		"unknown permission":  evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[0].Name = "read" }),
		"unknown state":       evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[0].State = "sandboxed" }),
		"permissions unsorted": evalWith(func(p *PolicyEvaluatedPayload) {
			p.Permissions[0], p.Permissions[1] = p.Permissions[1], p.Permissions[0]
		}),
		"duplicate permission":  evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[1].Name = p.Permissions[0].Name }),
		"nil permissions":       evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions = nil }),
		"nil permission value":  evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[0].Value = nil }),
		"value too long":        evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[0].Value = []string{long(MaxPolicyValueBytes + 1)} }),
		"too many values":       evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[0].Value = evidence(MaxPolicyValues + 1) }),
		"applied not requested": evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[3].Applied = true }),
		"applied in audit": evalWith(func(p *PolicyEvaluatedPayload) {
			p.Mode = PolicyModeAudit
			p.Permissions[1].Applied = true
			p.Applied = true
		}),
		"applied on detected": evalWith(func(p *PolicyEvaluatedPayload) {
			p.Mode = PolicyModeEnforce
			p.Permissions[0].State = PolicyStateDetected
			p.Permissions[0].Applied = true
		}),
		"nil secrets":      evalWith(func(p *PolicyEvaluatedPayload) { p.Secrets = nil }),
		"too many secrets": evalWith(func(p *PolicyEvaluatedPayload) { p.Secrets = secrets(MaxPolicySecrets + 1) }),
		"secrets unsorted": evalWith(func(p *PolicyEvaluatedPayload) {
			p.Secrets = []PolicySecret{{Name: "b", Provider: "env", Env: "B"}, {Name: "a", Provider: "env", Env: "A"}}
		}),
		"duplicate secret": evalWith(func(p *PolicyEvaluatedPayload) {
			p.Secrets = []PolicySecret{{Name: "a", Provider: "env", Env: "A"}, {Name: "a", Provider: "env", Env: "B"}}
		}),
		"bad secret name":         evalWith(func(p *PolicyEvaluatedPayload) { p.Secrets[0].Name = "Gh Token" }),
		"bad secret provider":     evalWith(func(p *PolicyEvaluatedPayload) { p.Secrets[0].Provider = "ENV/x" }),
		"bad secret env":          evalWith(func(p *PolicyEvaluatedPayload) { p.Secrets[0].Env = "GH TOKEN=abc" }),
		"empty secret env":        evalWith(func(p *PolicyEvaluatedPayload) { p.Secrets[0].Env = "" }),
		"evaluated has attempt":   func() Event { e := policyFacts()[0]; e.AttemptID = "a"; return e }(),
		"evaluated no node":       func() Event { e := policyFacts()[0]; e.NodeID = ""; return e }(),
		"evaluated no node run":   func() Event { e := policyFacts()[0]; e.NodeRunID = ""; return e }(),
		"violation no node":       func() Event { e := policyFacts()[1]; e.NodeID = ""; return e }(),
		"violation no node run":   func() Event { e := policyFacts()[1]; e.NodeRunID = ""; return e }(),
		"unknown violation perm":  violWith(func(p *PolicyViolationPayload) { p.Permission = "read" }),
		"unknown reason":          violWith(func(p *PolicyViolationPayload) { p.Reason = "because" }),
		"unknown outcome":         violWith(func(p *PolicyViolationPayload) { p.Outcome = "ignored" }),
		"nil evidence":            violWith(func(p *PolicyViolationPayload) { p.Evidence = nil }),
		"too much evidence":       violWith(func(p *PolicyViolationPayload) { p.Evidence = evidence(MaxPolicyEvidence + 1) }),
		"evidence unsorted":       violWith(func(p *PolicyViolationPayload) { p.Evidence = []string{"b", "a"} }),
		"evidence duplicate":      violWith(func(p *PolicyViolationPayload) { p.Evidence = []string{"a", "a"} }),
		"evidence too long":       violWith(func(p *PolicyViolationPayload) { p.Evidence = []string{long(MaxPolicyEvidenceBytes + 1)} }),
		"evidence invalid utf8":   violWith(func(p *PolicyViolationPayload) { p.Evidence = []string{"\xff"} }),
		"evidence nul":            violWith(func(p *PolicyViolationPayload) { p.Evidence = []string{"a\x00"} }),
		"evidence empty":          violWith(func(p *PolicyViolationPayload) { p.Evidence = []string{""} }),
		"blocked without outcome": violWith(func(p *PolicyViolationPayload) { p.Outcome = "" }),
	}
	for name, e := range bad {
		if _, err := json.Marshal(e); err == nil {
			t.Errorf("%s: invalid payload was accepted", name)
		}
	}
	edge := []Event{
		evalWith(func(p *PolicyEvaluatedPayload) { p.Secrets = secrets(MaxPolicySecrets) }),
		evalWith(func(p *PolicyEvaluatedPayload) { p.ScopeRoot = ""; p.ScopeRootKind = PolicyRootCWD }),
		evalWith(func(p *PolicyEvaluatedPayload) { p.Permissions[0].Value = evidence(MaxPolicyValues) }),
		evalWith(func(p *PolicyEvaluatedPayload) {
			p.Mode = PolicyModeEnforce
			p.Applied = true
			p.Permissions[0].State = PolicyStatePrevented
			p.Permissions[0].Applied = true
		}),
		violWith(func(p *PolicyViolationPayload) { p.Evidence = evidence(MaxPolicyEvidence) }),
		violWith(func(p *PolicyViolationPayload) { p.Evidence = []string{long(MaxPolicyEvidenceBytes)} }),
		violWith(func(p *PolicyViolationPayload) {
			p.Reason, p.Outcome, p.Evidence, p.Omitted = PolicyReasonLaunchRefused, PolicyOutcomeBlocked, []string{}, 0
			p.Permission = PolicyPermissionShell
		}),
	}
	for i, e := range edge {
		if _, err := json.Marshal(e); err != nil {
			t.Errorf("edge %d rejected: %v", i, err)
		}
	}
}

// A secret is described by name only. These keys are the closed vocabulary; a
// value, length, hash or prefix must be a deliberate, reviewed schema change.
func TestPolicyEventsRejectValueLikeFields(t *testing.T) {
	allowed := map[reflect.Type][]string{
		reflect.TypeOf(PolicySecret{}):           {"name", "provider", "env", "resolved"},
		reflect.TypeOf(PolicyPermission{}):       {"name", "requested", "value", "state", "applied"},
		reflect.TypeOf(PolicyEvaluatedPayload{}): {"mode", "applied", "scope_root_kind", "scope_root", "permissions", "secrets", "redaction_values"},
		reflect.TypeOf(PolicyViolationPayload{}): {"permission", "reason", "outcome", "evidence", "omitted"},
	}
	for typ, want := range allowed {
		var got []string
		for i := 0; i < typ.NumField(); i++ {
			got = append(got, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s json fields changed: %v, want %v", typ.Name(), got, want)
		}
	}
	good, err := json.Marshal(policyFacts()[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{`"value":"hunter2"`, `"length":7`, `"sha256":"ab"`, `"prefix":"gh"`} {
		tampered := strings.Replace(string(good), `"resolved":true`, `"resolved":true,`+extra, 1)
		var e Event
		if json.Unmarshal([]byte(tampered), &e) == nil {
			t.Fatalf("a secret entry with %s must not decode", extra)
		}
	}
	tampered := strings.Replace(string(good), `"redaction_values":7`, `"redaction_values":7,"redacted":["x"]`, 1)
	var e Event
	if json.Unmarshal([]byte(tampered), &e) == nil {
		t.Fatal("an unknown payload field must not decode")
	}
}

func TestPolicyEventsCloneIsDetached(t *testing.T) {
	for _, e := range policyFacts() {
		original := e.Payload
		cloned := ClonePayload(original)
		switch c := cloned.(type) {
		case PolicyEvaluatedPayload:
			c.Permissions[0].Value[0] = "changed"
			c.Permissions[0].State = "changed"
			c.Secrets[0].Name = "changed"
		case PolicyViolationPayload:
			c.Evidence[0] = "changed"
		default:
			t.Fatalf("clone of %s lost its type: %T", e.Type, cloned)
		}
		if !reflect.DeepEqual(original, policyFactsPayload(e.Type)) {
			t.Fatalf("%s: mutating the clone changed the original", e.Type)
		}
	}
}

func policyFactsPayload(t Type) Payload {
	if t == PolicyEvaluated {
		return evaluatedPayload()
	}
	return violationPayload()
}

func TestSchemasBelowPolicyStillDecode(t *testing.T) {
	for n := 1; n < SchemaPolicy; n++ {
		line := fmt.Sprintf(`{"event_schema_version":%d,"type":"run_started","timestamp":"2026-11-01T09:00:01Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":1}}`, n)
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.SchemaVersion != n {
			t.Fatalf("schema %d no longer decodes: %v", n, err)
		}
	}
}

func TestPolicyCheckKindsRoundTripAndGate(t *testing.T) {
	for _, kind := range []CheckKind{CheckExecutionPolicy, CheckSecretInWorkspace} {
		p := CheckResult{Version: 1, CheckID: string(kind), Kind: kind, Verdict: CheckRed, Reason: string(kind) + "_violation", Scope: CheckScope{CheckedInputs: []string{"candidate"}}, Evidence: []GateEvidence{{Kind: EvidenceKind(kind), Source: EvidenceSource(kind), Path: "dir/a.txt"}}}
		if kind == CheckSecretInWorkspace {
			p.Evidence[0].SecretName = "api_token" // a red secret result names its secret
		}
		e := policyEvent(SchemaVersion, GateCheck, p)
		e.AttemptID = "att"
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(got.Payload, e.Payload) {
			t.Fatalf("%s did not round trip: %v", kind, err)
		}
		e.SchemaVersion = SchemaPolicy - 1
		if _, err := json.Marshal(e); err == nil {
			t.Fatalf("%s must require schema %d", kind, SchemaPolicy)
		}
		p.Evidence[0].ExitCode = new(int)
		if _, err := json.Marshal(policyEvent(SchemaVersion, GateCheck, p)); err == nil {
			t.Fatalf("%s carries no process evidence", kind)
		}
		p.Evidence[0].ExitCode = nil
		p.Evidence[0].Source = "git2"
		if _, err := json.Marshal(policyEvent(SchemaVersion, GateCheck, p)); err == nil {
			t.Fatalf("%s needs its own evidence source", kind)
		}
	}
}
