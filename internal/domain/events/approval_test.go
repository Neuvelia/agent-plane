package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func approvalEvent(version int, typ Type, payload Payload) Event {
	return Event{SchemaVersion: version, Type: typ, Timestamp: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC), Sequence: 4, RunID: "run", GraphID: "g", GraphVersion: "sha256-v0:x", NodeID: "ask", NodeRunID: "nr", Payload: payload}
}

func TestApprovalEventsRoundTrip(t *testing.T) {
	for _, e := range []Event{
		approvalEvent(SchemaVersion, ApprovalRequested, ApprovalRequestedPayload{Prompt: "ship?", Context: []ApprovalContext{{Name: "summary", Value: "ok"}}}),
		approvalEvent(SchemaVersion, ApprovalDecided, ApprovalDecidedPayload{Decision: ApprovalRejected, Comment: "not yet"}),
	} {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("%s: %v", data, err)
		}
		again, _ := json.Marshal(got)
		if string(again) != string(data) {
			t.Fatalf("round trip changed event:\n%s\n%s", data, again)
		}
	}
}

func TestApprovalEventsRequireSchemaNineAndIdentity(t *testing.T) {
	requested := ApprovalRequestedPayload{Prompt: "p", Context: []ApprovalContext{}}
	if _, err := json.Marshal(approvalEvent(8, ApprovalRequested, requested)); err == nil {
		t.Fatal("schema 8 must not carry approval events")
	}
	e := approvalEvent(SchemaVersion, ApprovalRequested, requested)
	e.NodeRunID = ""
	if _, err := json.Marshal(e); err == nil {
		t.Fatal("approval events need a node run ID")
	}
	raw := `{"event_schema_version":8,"type":"approval_decided","timestamp":"2026-11-14T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","payload":{"decision":"approved"}}`
	var got Event
	if json.Unmarshal([]byte(raw), &got) == nil {
		t.Fatal("decoding approval under schema 8 must fail")
	}
}

func TestApprovalPayloadLimitsAndDecisions(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n) }
	bad := []Payload{
		ApprovalRequestedPayload{Prompt: long(MaxApprovalPrompt + 1), Context: []ApprovalContext{}},
		ApprovalRequestedPayload{Prompt: "p"}, // nil context is not a list
		ApprovalRequestedPayload{Prompt: "p", Context: make([]ApprovalContext, MaxApprovalContextItems+1)},
		ApprovalRequestedPayload{Prompt: "p", Context: []ApprovalContext{{Name: "", Value: "v"}}},
		ApprovalRequestedPayload{Prompt: "p", Context: []ApprovalContext{{Name: "n", Value: long(MaxApprovalContextValue + 1)}}},
		ApprovalDecidedPayload{Decision: "maybe"},
		ApprovalDecidedPayload{Decision: ApprovalApproved, Comment: long(MaxApprovalComment + 1)},
	}
	for i, p := range bad {
		typ := ApprovalRequested
		if _, ok := p.(ApprovalDecidedPayload); ok {
			typ = ApprovalDecided
		}
		if _, err := json.Marshal(approvalEvent(SchemaVersion, typ, p)); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	// Older schemas keep decoding: a frozen schema 8 record still reads.
	var got Event
	frozen := `{"event_schema_version":8,"type":"run_started","timestamp":"2026-11-14T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":0}}`
	if err := json.Unmarshal([]byte(frozen), &got); err != nil {
		t.Fatal(err)
	}
}
