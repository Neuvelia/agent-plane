package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func textPtr(s string) *string { return &s }

func valueEvent(version int, node string, nodeRun NodeRunID, attempt AttemptID, p ValueRecordedPayload) Event {
	return Event{SchemaVersion: version, Type: ValueRecorded, Timestamp: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), Sequence: 2, RunID: "run", GraphID: "g", GraphVersion: "sha256-v0:x", NodeID: node, NodeRunID: nodeRun, AttemptID: attempt, Payload: p}
}

func outputValue(text string) ValueRecordedPayload {
	return ValueRecordedPayload{NodeRunID: "nr", AttemptID: "a", Port: "stdout", Type: "string", SizeBytes: int64(len(text)), SHA256: digest(text), Value: textPtr(text)}
}

func TestValueRecordedRoundTripsOutputsAndGraphInputs(t *testing.T) {
	full := strings.Repeat("x", 40)
	cases := []Event{
		valueEvent(SchemaVersion, "work", "nr", "a", outputValue("hello")),
		valueEvent(SchemaVersion, "work", "nr", "a", outputValue("")),
		valueEvent(SchemaVersion, "", "", "", ValueRecordedPayload{Port: "inputs.job", Type: "json", SizeBytes: 2, SHA256: digest("{}"), Value: textPtr("{}")}),
		valueEvent(SchemaVersion, "work", "nr", "", ValueRecordedPayload{NodeRunID: "nr", Port: "big", Type: "string", SizeBytes: 40, SHA256: digest(full), Truncated: true, Redacted: true, Value: textPtr(full[:10])}),
		valueEvent(SchemaVersion, "work", "nr", "", ValueRecordedPayload{NodeRunID: "nr", Port: "capped", Type: "number", SizeBytes: 3, SHA256: digest("1.5"), Truncated: true}),
		// The graph contract has no identifier length cap, so neither does the port.
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("x"); p.Port = "o" + strings.Repeat("u", 300); return p }()),
		valueEvent(SchemaVersion, "", "", "", ValueRecordedPayload{Port: "inputs.i" + strings.Repeat("n", 300), Type: "string", SizeBytes: 1, SHA256: digest("x"), Value: textPtr("x")}),
	}
	for _, original := range cases {
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("%#v: %v", original.Payload, err)
		}
		var decoded Event
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("%s: %v", data, err)
		}
		got := decoded.Payload.(ValueRecordedPayload)
		want := original.Payload.(ValueRecordedPayload)
		if (got.Value == nil) != (want.Value == nil) || (got.Value != nil && *got.Value != *want.Value) || got.Port != want.Port || got.SizeBytes != want.SizeBytes || got.Truncated != want.Truncated || got.Redacted != want.Redacted || decoded.NodeRunID != original.NodeRunID {
			t.Fatalf("round trip %s = %#v", data, got)
		}
	}
	empty, _ := json.Marshal(cases[1])
	if !strings.Contains(string(empty), `"value":""`) {
		t.Fatalf("empty value must be present, got %s", empty)
	}
	capped, _ := json.Marshal(cases[4])
	if strings.Contains(string(capped), `"value"`) || !strings.Contains(string(capped), `"truncated":true`) {
		t.Fatalf("capped value = %s", capped)
	}
}

func TestValueRecordedRejectsInvalidPayloads(t *testing.T) {
	big := strings.Repeat("y", MaxValueBytes+1)
	bad := []Event{
		valueEvent(9, "work", "nr", "a", outputValue("hello")),                                                                                   // schema below 10
		valueEvent(SchemaVersion, "work", "other", "a", outputValue("hello")),                                                                    // envelope mismatch
		valueEvent(SchemaVersion, "", "", "", outputValue("hello")),                                                                              // output without node
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.Port = "inputs.x"; return p }()), // graph input with node
		valueEvent(SchemaVersion, "", "", "", ValueRecordedPayload{Port: "inputs.", Type: "string", SHA256: digest(""), Value: textPtr("")}),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.Type = "bytes"; return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.SizeBytes = 4; return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.SHA256 = digest("other"); return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.SHA256 = strings.ToUpper(p.SHA256); return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.Value = nil; return p }()), // missing without truncated
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue(big); p.Truncated = true; p.SizeBytes++; return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.Truncated = true; return p }()), // truncated but complete
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.SizeBytes = -1; return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("a\xffb"); return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.Port = ""; return p }()),
		valueEvent(SchemaVersion, "work", "nr", "a", func() ValueRecordedPayload { p := outputValue("hello"); p.Port = "Out"; return p }()),
		valueEvent(SchemaVersion, "", "", "", ValueRecordedPayload{Port: "inputs.1x", Type: "string", SHA256: digest(""), Value: textPtr("")}),
	}
	for i, e := range bad {
		if _, err := json.Marshal(e); err == nil {
			t.Errorf("case %d serialized: %#v", i, e.Payload)
		}
	}
	good, err := json.Marshal(valueEvent(SchemaVersion, "work", "nr", "a", outputValue("hello")))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ from, to string }{
		{`"event_schema_version":10`, `"event_schema_version":9`},
		{`"redacted":false`, `"redacted":false,"extra":1`},
		{`"size_bytes":5`, `"size_bytes":"5"`},
	} {
		var decoded Event
		if err := json.Unmarshal([]byte(strings.Replace(string(good), mutation.from, mutation.to, 1)), &decoded); err == nil {
			t.Errorf("mutation %q decoded", mutation.to)
		}
	}
}

func TestOlderSchemaStreamsStayReadable(t *testing.T) {
	for version := 1; version < SchemaVersion; version++ {
		line := fmt.Sprintf(`{"event_schema_version":%d,"type":"run_started","timestamp":"2026-11-15T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":1}}`, version)
		var decoded Event
		if err := json.Unmarshal([]byte(line), &decoded); err != nil || decoded.SchemaVersion != version {
			t.Fatalf("schema %d: %v", version, err)
		}
	}
}

func TestCollectorCopiesRecordedValue(t *testing.T) {
	c := NewCollector("run", "g", "v")
	p := outputValue("hello")
	recorded, err := c.Append(Event{Type: ValueRecorded, NodeID: "work", NodeRunID: "nr", AttemptID: "a", Payload: p})
	if err != nil {
		t.Fatal(err)
	}
	*p.Value = "HELLO"
	*recorded.Payload.(ValueRecordedPayload).Value = "mutated"
	if got := *c.Snapshot()[0].Payload.(ValueRecordedPayload).Value; got != "hello" {
		t.Fatalf("retained value = %q", got)
	}
}
