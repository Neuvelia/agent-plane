package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIDsAreUniqueAndUUIDShaped(t *testing.T) {
	a, err := NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	node, err := NewNodeRunID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || string(a) == string(node) || len(a) != 36 || a[14] != '4' {
		t.Fatalf("IDs: %q %q %q", a, b, node)
	}
}

func TestEventJSONRoundTripsTypedPayload(t *testing.T) {
	e := Event{SchemaVersion: SchemaVersion, Type: LogChunk, Timestamp: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), Sequence: 3, RunID: "run", GraphID: "graph", GraphVersion: "sha256-v0:test", NodeID: "node", NodeRunID: "node-run", Payload: LogChunkPayload{Stream: Stdout, Data: []byte{0, 255, 'x'}}}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"type":"log_chunk"`) || !strings.Contains(string(data), `"event_schema_version":1`) || strings.Contains(string(data), "attempt_id") {
		t.Fatalf("JSON = %s", data)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	payload, ok := got.Payload.(LogChunkPayload)
	if !ok || got.Sequence != 3 || payload.Stream != Stdout || string(payload.Data) != string([]byte{0, 255, 'x'}) {
		t.Fatalf("round trip = %#v", got)
	}
}

func TestEventJSONRejectsUnknownType(t *testing.T) {
	var event Event
	if err := json.Unmarshal([]byte(`{"type":"unknown","payload":{}}`), &event); err == nil {
		t.Fatal("unknown event type accepted")
	}
}

func TestEveryEventPayloadRoundTrips(t *testing.T) {
	cases := []struct {
		kind    Type
		payload Payload
	}{
		{RunStarted, RunStartedPayload{NodeCount: 2}},
		{NodeReady, NodeReadyPayload{}},
		{NodeStarted, NodeStartedPayload{}},
		{LogChunk, LogChunkPayload{Stream: Stderr, Data: []byte("error")}},
		{NodeFinished, NodeFinishedPayload{Status: "failed", FailureKind: "executor"}},
		{NodeTerminated, NodeTerminatedPayload{Status: "skipped"}},
		{RunFinished, RunFinishedPayload{Status: "failed", NodeCounts: NodeCounts{Failed: 1, Skipped: 1}}},
	}
	for _, test := range cases {
		t.Run(string(test.kind), func(t *testing.T) {
			original := Event{SchemaVersion: SchemaVersion, Type: test.kind, Payload: test.payload}
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Event
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Type != test.kind || decoded.Payload == nil {
				t.Fatalf("decoded = %#v", decoded)
			}
		})
	}
}

func TestEventRejectsMismatchedPayload(t *testing.T) {
	event := Event{SchemaVersion: SchemaVersion, Type: LogChunk, Payload: NodeFinishedPayload{Status: "failed"}}
	if _, err := json.Marshal(event); err == nil {
		t.Fatal("mismatched type and payload serialized")
	}
	var decoded Event
	if err := json.Unmarshal([]byte(`{"event_schema_version":1,"type":"log_chunk","payload":{"status":"failed"}}`), &decoded); err == nil {
		t.Fatal("mismatched payload JSON decoded")
	}
}
