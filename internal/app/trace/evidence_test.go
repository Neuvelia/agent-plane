package trace

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/domain/events"
	"encoding/json"
	"testing"
)

func TestEvidenceVersionAndDetachedSnapshot(t *testing.T) {
	p := events.NodeEvidencePayload{AdapterID: "codex", AdapterVersion: "1.0", ChangedFiles: []events.ChangedFile{{Path: "a", Provenance: "observed"}}}
	c := NewCollector(system.Clock{}, "run", "g", "v")
	if _, err := c.Append(events.Event{Type: events.NodeEvidence, Payload: p}); err != nil {
		t.Fatal(err)
	}
	p.ChangedFiles[0].Path = "mutated"
	s := c.Snapshot()
	if s[0].Payload.(events.NodeEvidencePayload).ChangedFiles[0].Path != "a" {
		t.Fatal("alias")
	}
	data, err := json.Marshal(s[0])
	if err != nil {
		t.Fatal(err)
	}
	var e events.Event
	if err = json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(`{"event_schema_version":1,"type":"node_evidence","payload":{"adapter_id":"codex","adapter_version":"1","changed_files":[]}}`), &e); err == nil {
		t.Fatal("v1 evidence accepted")
	}
	if err = json.Unmarshal([]byte(`{"event_schema_version":1,"type":"node_ready","payload":{}}`), &e); err != nil {
		t.Fatal(err)
	}
}
func TestEvidenceCannotBeSerializedUnderVersionOne(t *testing.T) {
	if _, e := json.Marshal(events.Event{SchemaVersion: 1, Type: events.NodeEvidence, Payload: events.NodeEvidencePayload{AdapterID: "codex", ChangedFiles: []events.ChangedFile{}}}); e == nil {
		t.Fatal("new evidence serialized as v1")
	}
}
