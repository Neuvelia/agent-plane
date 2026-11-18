package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func cancelEvent(version int, typ Type, node bool, payload Payload) Event {
	e := Event{SchemaVersion: version, Type: typ, Timestamp: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Sequence: 4, RunID: "run", GraphID: "g", GraphVersion: "sha256-v0:x", Payload: payload}
	if node {
		e.NodeID, e.NodeRunID = "worker", "nr"
	}
	return e
}

func roundTrip(t *testing.T, e Event) Event {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal %s: %v", e.Type, err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	return got
}

func TestCancelEventsRoundTripAndBounds(t *testing.T) {
	for _, e := range []Event{
		cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonInterrupt, Source: CancelSourceSignal}),
		cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonRunTimeout, Source: CancelSourceDeadline}),
		cancelEvent(SchemaVersion, CancelRequested, true, CancelRequestedPayload{Scope: CancelScopeNode, Reason: ReasonNodeTimeout, Source: CancelSourceDeadline}),
		cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful, GraceMS: 5000, ElapsedMS: 12}),
		cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessForced, GraceMS: 200, ElapsedMS: 1300, Leaked: 2}),
		cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessAlreadyExited}),
	} {
		got := roundTrip(t, e)
		if got.Payload != e.Payload || got.Type != e.Type {
			t.Fatalf("round trip = %#v, want %#v", got, e)
		}
	}

	bad := map[string]Event{
		"unknown scope":           cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: "all", Reason: ReasonInterrupt, Source: CancelSourceSignal}),
		"missing scope":           cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Reason: ReasonInterrupt, Source: CancelSourceSignal}),
		"missing reason":          cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeRun, Source: CancelSourceSignal}),
		"missing source":          cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonInterrupt}),
		"unknown source":          cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonInterrupt, Source: "cli"}),
		"node scope uncorrelated": cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeNode, Reason: ReasonNodeTimeout, Source: CancelSourceDeadline}),
		"run scope with node":     cancelEvent(SchemaVersion, CancelRequested, true, CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonInterrupt, Source: CancelSourceSignal}),
		"process uncorrelated":    cancelEvent(SchemaVersion, ProcessTerminated, false, ProcessTerminatedPayload{Method: ProcessGraceful}),
		"process no node run": func() Event {
			e := cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful})
			e.NodeRunID = ""
			return e
		}(),
		"unknown method":        cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: "killed"}),
		"negative leaked":       cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessForced, Leaked: -1}),
		"leaked beyond bound":   cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessForced, Leaked: MaxProcessLeaked + 1}),
		"negative grace":        cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful, GraceMS: -1}),
		"negative elapsed":      cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful, ElapsedMS: -1}),
		"grace beyond bound":    cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful, GraceMS: MaxProcessMS + 1}),
		"elapsed beyond bound":  cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful, ElapsedMS: MaxProcessMS + 1}),
		"leaked on graceful":    cancelEvent(SchemaVersion, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful, Leaked: 1}),
		"payload type mismatch": cancelEvent(SchemaVersion, ProcessTerminated, true, CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonInterrupt, Source: CancelSourceSignal}),
	}
	for name, e := range bad {
		t.Run(name, func(t *testing.T) {
			if err := e.Validate(); err == nil {
				t.Fatalf("%s accepted: %#v", name, e)
			}
			if _, err := json.Marshal(e); err == nil {
				t.Fatalf("%s serialized", name)
			}
		})
	}

	var decoded Event
	unknownField := fmt.Sprintf(`{"event_schema_version":%d,"type":"process_terminated","node_id":"n","node_run_id":"nr","payload":{"method":"graceful","grace_ms":1,"elapsed_ms":1,"leaked":0,"pid":7}}`, SchemaVersion)
	if err := json.Unmarshal([]byte(unknownField), &decoded); err == nil {
		t.Fatal("process_terminated accepted an unknown payload field")
	}
}

func TestCancelEventsRequireNewSchema(t *testing.T) {
	old := schemaCancelTimeout - 1
	for name, e := range map[string]Event{
		"cancel_requested":   cancelEvent(old, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonInterrupt, Source: CancelSourceSignal}),
		"process_terminated": cancelEvent(old, ProcessTerminated, true, ProcessTerminatedPayload{Method: ProcessGraceful}),
		"node_finished":      cancelEvent(old, NodeFinished, true, NodeFinishedPayload{Status: "cancelled", Reason: ReasonInterrupt}),
		"node_terminated":    cancelEvent(old, NodeTerminated, true, NodeTerminatedPayload{Status: "cancelled", Reason: ReasonInterrupt}),
		"run_finished":       cancelEvent(old, RunFinished, false, RunFinishedPayload{Status: "cancelled", Reason: ReasonInterrupt}),
		"attempt timeout": func() Event {
			e := cancelEvent(old, AttemptFinished, true, AttemptFinishedPayload{Outcome: "timeout"})
			e.AttemptID = "a"
			return e
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := e.Validate(); err == nil || !strings.Contains(err.Error(), fmt.Sprint(schemaCancelTimeout)) {
				t.Fatalf("schema %d accepted %s (err %v)", old, name, err)
			}
			e.SchemaVersion = schemaCancelTimeout
			if err := e.Validate(); err != nil {
				t.Fatalf("schema %d rejected %s: %v", schemaCancelTimeout, name, err)
			}
		})
	}
	if SchemaVersion < schemaCancelTimeout {
		t.Fatalf("SchemaVersion %d is below the cancel/timeout schema %d", SchemaVersion, schemaCancelTimeout)
	}

	var decoded Event
	line := fmt.Sprintf(`{"event_schema_version":%d,"type":"cancel_requested","payload":{"scope":"run","reason":"interrupt","source":"signal"}}`, old)
	if err := json.Unmarshal([]byte(line), &decoded); err == nil {
		t.Fatal("old-schema cancel_requested decoded")
	}
}

func TestReasonOnlyOnCancelledOrTimeoutTerminals(t *testing.T) {
	type terminal func(status, kind, reason string) Event
	terminals := map[string]terminal{
		"node_finished": func(status, kind, reason string) Event {
			return cancelEvent(SchemaVersion, NodeFinished, true, NodeFinishedPayload{Status: status, FailureKind: kind, Reason: reason})
		},
		"node_terminated": func(status, kind, reason string) Event {
			return cancelEvent(SchemaVersion, NodeTerminated, true, NodeTerminatedPayload{Status: status, FailureKind: kind, Reason: reason})
		},
		"run_finished": func(status, kind, reason string) Event {
			return cancelEvent(SchemaVersion, RunFinished, false, RunFinishedPayload{Status: status, FailureKind: kind, Reason: reason})
		},
	}
	for name, build := range terminals {
		t.Run(name, func(t *testing.T) {
			for _, ok := range []Event{
				build("cancelled", "", ReasonInterrupt),
				build("cancelled", "", ReasonSiblingFailed),
				build("failed", "timeout", ReasonNodeTimeout),
				build("cancelled", "", ""),
				build("succeeded", "", ""),
				build("failed", "executor", ""),
			} {
				if err := ok.Validate(); err != nil {
					t.Fatalf("%#v: %v", ok.Payload, err)
				}
				roundTrip(t, ok)
			}
			for _, bad := range []Event{
				build("succeeded", "", ReasonInterrupt),
				build("failed", "executor", ReasonUserCancel),
				build("failed", "", ReasonRunTimeout),
				build("skipped", "", ReasonSiblingFailed),
			} {
				if err := bad.Validate(); err == nil {
					t.Fatalf("reason accepted on %#v", bad.Payload)
				}
			}
		})
	}
}

func TestReasonSetIsClosed(t *testing.T) {
	for _, r := range []string{ReasonInterrupt, ReasonUserCancel, ReasonRunTimeout, ReasonSiblingFailed, ReasonNodeTimeout} {
		if !ValidReason(r) {
			t.Fatalf("%q must be valid", r)
		}
		e := cancelEvent(SchemaVersion, RunFinished, false, RunFinishedPayload{Status: "cancelled", Reason: r})
		if err := e.Validate(); err != nil {
			t.Fatalf("%q: %v", r, err)
		}
	}
	for _, r := range []string{"timeout", "Interrupt", "node_timeout ", "ctrl-c", strings.Repeat("a", 65)} {
		if ValidReason(r) {
			t.Fatalf("%q must not be valid", r)
		}
		if err := cancelEvent(SchemaVersion, RunFinished, false, RunFinishedPayload{Status: "cancelled", Reason: r}).Validate(); err == nil {
			t.Fatalf("%q accepted on run_finished", r)
		}
		if err := cancelEvent(SchemaVersion, CancelRequested, false, CancelRequestedPayload{Scope: CancelScopeRun, Reason: r, Source: CancelSourceSignal}).Validate(); err == nil {
			t.Fatalf("%q accepted on cancel_requested", r)
		}
	}
}

func TestCancelEventsCloneIsDetached(t *testing.T) {
	ms := int64(7)
	original := NodeFinishedPayload{Status: "failed", FailureKind: "timeout", Reason: ReasonNodeTimeout, DurationMS: &ms, DiagnosticCodes: []string{"x"}}
	cloned := ClonePayload(original).(NodeFinishedPayload)
	*original.DurationMS = 99
	original.DiagnosticCodes[0] = "changed"
	if *cloned.DurationMS != 7 || cloned.DiagnosticCodes[0] != "x" || cloned.Reason != ReasonNodeTimeout {
		t.Fatalf("clone aliased the original: %#v", cloned)
	}
	for _, p := range []Payload{
		CancelRequestedPayload{Scope: CancelScopeRun, Reason: ReasonInterrupt, Source: CancelSourceSignal},
		ProcessTerminatedPayload{Method: ProcessForced, Leaked: 1},
		NodeTerminatedPayload{Status: "cancelled", Reason: ReasonInterrupt},
		RunFinishedPayload{Status: "cancelled", Reason: ReasonInterrupt},
	} {
		got := ClonePayload(p)
		if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", p) {
			t.Fatalf("clone of %T changed: %#v", p, got)
		}
	}
}

// Every earlier schema keeps decoding with its historical shape; none of
// those streams may carry the new facts or fields.
func TestOlderSchemaFixturesDecodeWithoutCancelFacts(t *testing.T) {
	for v := 1; v <= SchemaVersion; v++ {
		line := fmt.Sprintf(`{"event_schema_version":%d,"type":"node_finished","timestamp":"2026-11-18T09:00:00Z","sequence":4,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","payload":{"status":"failed","failure_kind":"executor","duration_ms":1}}`, v)
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("schema %d node_finished: %v", v, err)
		}
		if p := e.Payload.(NodeFinishedPayload); p.Reason != "" || p.FailureKind != "executor" {
			t.Fatalf("schema %d payload = %#v", v, p)
		}
	}

	const stream11 = `{"event_schema_version":11,"type":"run_started","timestamp":"2026-11-18T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":1}}
{"event_schema_version":11,"type":"node_started","timestamp":"2026-11-18T09:00:01Z","sequence":2,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","payload":{}}
{"event_schema_version":11,"type":"node_terminated","timestamp":"2026-11-18T09:00:03Z","sequence":3,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","payload":{"status":"cancelled"}}
{"event_schema_version":11,"type":"run_finished","timestamp":"2026-11-18T09:00:04Z","sequence":5,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"status":"cancelled","duration_ms":4,"node_counts":{"succeeded":0,"failed":0,"skipped":0,"cancelled":1}}}`
	var last Event
	for i, line := range strings.Split(stream11, "\n") {
		if err := json.Unmarshal([]byte(line), &last); err != nil {
			t.Fatalf("schema 11 line %d: %v", i+1, err)
		}
	}
	if p := last.Payload.(RunFinishedPayload); p.Status != "cancelled" || p.Reason != "" {
		t.Fatalf("schema 11 run_finished = %#v", p)
	}
}
