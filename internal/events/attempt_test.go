package events

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAttemptEventsRoundTrip(t *testing.T) {
	for _, item := range []struct {
		kind Type
		p    Payload
	}{{AttemptStarted, AttemptStartedPayload{1, 3}}, {AttemptInput, AttemptInputPayload{TaskFingerprint: strings.Repeat("a", 64), IntendedScope: []string{}, ScopePresent: true}}, {AttemptFinished, AttemptFinishedPayload{Outcome: "accepted", CheckID: "test"}}} {
		e := Event{SchemaVersion: 6, Type: item.kind, NodeRunID: "node", AttemptID: "attempt", Payload: item.p}
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Event
		if err = json.Unmarshal(b, &decoded); err != nil {
			t.Fatal(err)
		}
		e.SchemaVersion = 5
		if e.Validate() == nil {
			t.Fatal("old schema accepted attempt")
		}
	}
}
func TestCheckResultsSchema5AndAttemptFilter(t *testing.T) {
	p := gateFact()
	trace := []Event{{SchemaVersion: 5, Type: GateCheck, RunID: "r", NodeRunID: "n", Sequence: 1, Payload: p}, {SchemaVersion: 6, Type: GateCheck, RunID: "r", NodeRunID: "n", AttemptID: "a", Sequence: 2, Payload: p}}
	all := CheckResults(trace, "r", "n")
	filtered := AttemptCheckResults(trace, "r", "n", "a")
	if len(all) != 2 || len(filtered) != 1 {
		t.Fatal(all, filtered)
	}
	filtered[0].Evidence[0].Path = "changed"
	if trace[1].Payload.(CheckResult).Evidence[0].Path == "changed" {
		t.Fatal("lookup aliased")
	}
}
