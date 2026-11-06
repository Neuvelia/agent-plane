package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"agentplane/internal/events"
)

func safeEventFixture() events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion, Type: events.NodeReady,
		Timestamp: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), Sequence: 1,
		RunID: "rid", GraphID: "graph", GraphVersion: "hash", NodeID: "node", NodeRunID: "nrid",
		Payload: events.NodeReadyPayload{},
	}
}

func TestParseSafeEventScansTypedValuesWithoutTreatingFixedKeysAsSecrets(t *testing.T) {
	event := safeEventFixture()
	projection, err := ParseSafeEvent(event, []string{"run"})
	if err != nil || !bytes.Contains(projection.Bytes(), []byte(`"run_id":"rid"`)) {
		t.Fatalf("fixed event key rejected: projection=%q err=%v", projection.Bytes(), err)
	}
	if _, err := ParseSafeEvent(event, []string{"graph"}); err == nil {
		t.Fatal("private typed event value admitted")
	}
	event.IdentityRedacted = true
	if _, err := ParseSafeEvent(event, nil); err == nil {
		t.Fatal("pre-redacted identity admitted")
	}
}

func TestParseSafeEventRejectsRawBytesBeforeBase64(t *testing.T) {
	event := safeEventFixture()
	event.Type = events.LogChunk
	event.Payload = events.LogChunkPayload{Stream: events.Stdout, Data: []byte("private")}
	if _, err := ParseSafeEvent(event, []string{"private"}); err == nil {
		t.Fatal("base64 hid a raw private value")
	}
}

func TestParseSafeEventEnforcesIndexedProjectionLimit(t *testing.T) {
	event := safeEventFixture()
	event.Type = events.NodeEvidence
	event.Payload = events.NodeEvidencePayload{AdapterID: "adapter", AdapterVersion: strings.Repeat("x", maxIndexedProjectionBytes)}
	if _, err := ParseSafeEvent(event, nil); !errors.Is(err, ErrPageTooLarge) {
		t.Fatalf("oversized event = %v", err)
	}
}

func TestParseSafeEventCannotAdmitUntypedDynamicPayloadKeys(t *testing.T) {
	encoded, err := json.Marshal(safeEventFixture())
	if err != nil {
		t.Fatal(err)
	}
	malformed := bytes.Replace(encoded, []byte(`"payload":{}`), []byte(`"payload":{"private_key":"secret"}`), 1)
	var typed events.Event
	if err := json.Unmarshal(malformed, &typed); err == nil {
		t.Fatal("untyped dynamic payload member entered closed event schema")
	}
}

func TestParseSafeEventRejectsInvalidUTF8IdentityBeforeJSONReplacement(t *testing.T) {
	event := safeEventFixture()
	event.GraphID = string([]byte{'g', 0xff})
	if _, err := ParseSafeEvent(event, nil); err == nil {
		t.Fatal("invalid identity changed during JSON encoding")
	}
}
