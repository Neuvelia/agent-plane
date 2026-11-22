package events

import (
	"encoding/json"
	"testing"
)

func appliedPermissionUnderUnappliedPolicy() PolicyEvaluatedPayload {
	p := evaluatedPayload()
	p.Mode = PolicyModeEnforce
	p.Applied = false
	p.Permissions[1].Applied = true
	return p
}

func TestPolicyAppliedPermissionRequiresAppliedEvent(t *testing.T) {
	p := appliedPermissionUnderUnappliedPolicy()
	if err := p.Validate(); err == nil {
		t.Fatal("a permission applied under an unapplied policy was accepted")
	}
	if _, err := json.Marshal(policyEvent(SchemaVersion, PolicyEvaluated, p)); err == nil {
		t.Fatal("event with an applied permission and applied=false was serialized")
	}
	p.Applied = true
	data, err := json.Marshal(policyEvent(SchemaVersion, PolicyEvaluated, p))
	if err != nil {
		t.Fatalf("consistent applied flags rejected: %v", err)
	}
	bad := []byte(`{"event_schema_version":14,"type":"policy_evaluated","payload":` +
		string(mustPayloadJSON(t, appliedPermissionUnderUnappliedPolicy())) + `}`)
	var decoded Event
	if err := json.Unmarshal(bad, &decoded); err == nil {
		t.Fatal("log line with an applied permission and applied=false was decoded")
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("consistent log line rejected on read: %v", err)
	}
}

func TestPolicyAppliedEventAllowsNoAppliedPermission(t *testing.T) {
	p := evaluatedPayload()
	p.Mode = PolicyModeEnforce
	p.Applied = true
	if err := p.Validate(); err != nil {
		t.Fatalf("enforce without applied permissions rejected: %v", err)
	}
}

func mustPayloadJSON(t *testing.T, p PolicyEvaluatedPayload) []byte {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
