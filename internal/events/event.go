package events

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const SchemaVersion = 5

type RunID string
type NodeRunID string

// AttemptID is reserved for a later bounded execution-loop contract.
type AttemptID string

func NewRunID() (RunID, error) {
	id, err := newUUID()
	return RunID(id), err
}

func NewNodeRunID() (NodeRunID, error) {
	id, err := newUUID()
	return NodeRunID(id), err
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

type Type string

const (
	NodeEvidence   Type = "node_evidence"
	RunStarted     Type = "run_started"
	NodeReady      Type = "node_ready"
	NodeStarted    Type = "node_started"
	LogChunk       Type = "log_chunk"
	NodeFinished   Type = "node_finished"
	NodeTerminated Type = "node_terminated"
	RunFinished    Type = "run_finished"
)

type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

// Payload is implemented only by the event payloads defined in this package.
type Payload interface{ eventPayload() }

type RunStartedPayload struct {
	NodeCount int `json:"node_count"`
}
type ChangedFile struct {
	Path       string `json:"path"`
	Provenance string `json:"provenance"`
}
type NodeEvidencePayload struct {
	AdapterID      string        `json:"adapter_id"`
	AdapterVersion string        `json:"adapter_version"`
	ChangedFiles   []ChangedFile `json:"changed_files"`
}

func (NodeEvidencePayload) eventPayload() {}

type NodeReadyPayload struct{}
type NodeStartedPayload struct{}
type LogChunkPayload struct {
	Stream Stream `json:"stream"`
	Data   []byte `json:"data"` // JSON represents bytes as base64.
}
type NodeFinishedPayload struct {
	Status          string   `json:"status"`
	FailureKind     string   `json:"failure_kind,omitempty"`
	DiagnosticCodes []string `json:"diagnostic_codes,omitempty"`
	DurationMS      *int64   `json:"duration_ms,omitempty"`
}

// NodeTerminated records a terminal transition that never entered running.
type NodeTerminatedPayload struct {
	Status          string   `json:"status"`
	FailureKind     string   `json:"failure_kind,omitempty"`
	DiagnosticCodes []string `json:"diagnostic_codes,omitempty"`
}
type NodeCounts struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Cancelled int `json:"cancelled"`
}
type RunFinishedPayload struct {
	Status      string     `json:"status"`
	DurationMS  int64      `json:"duration_ms"`
	NodeCounts  NodeCounts `json:"node_counts"`
	FailureKind string     `json:"failure_kind,omitempty"`
}

func (RunStartedPayload) eventPayload()     {}
func (NodeReadyPayload) eventPayload()      {}
func (NodeStartedPayload) eventPayload()    {}
func (LogChunkPayload) eventPayload()       {}
func (NodeFinishedPayload) eventPayload()   {}
func (NodeTerminatedPayload) eventPayload() {}
func (RunFinishedPayload) eventPayload()    {}

// Event is a versioned, append-oriented fact. (run_id, sequence) is its ID.
type Event struct {
	SchemaVersion int       `json:"event_schema_version"`
	Type          Type      `json:"type"`
	Timestamp     time.Time `json:"timestamp"`
	Sequence      uint64    `json:"sequence"`
	RunID         RunID     `json:"run_id"`
	GraphID       string    `json:"graph_id"`
	GraphVersion  string    `json:"graph_version"`
	NodeID        string    `json:"node_id,omitempty"`
	NodeRunID     NodeRunID `json:"node_run_id,omitempty"`
	AttemptID     AttemptID `json:"attempt_id,omitempty"`
	Payload       Payload   `json:"payload"`
}

// Validate checks the event envelope before it becomes a serialized fact.
func (e Event) Validate() error {
	if e.SchemaVersion != 0 && e.SchemaVersion != 1 && e.SchemaVersion != 2 && e.SchemaVersion != 3 && e.SchemaVersion != 4 && e.SchemaVersion != 5 {
		return fmt.Errorf("unsupported event schema version %d", e.SchemaVersion)
	}
	if e.SchemaVersion > 0 && e.SchemaVersion < 3 && (IsNormalized(e.Type) || e.Type == ProgressOmitted) {
		return errors.New("progress requires event schema 3")
	}
	if e.SchemaVersion == 1 && e.Type == NodeEvidence {
		return errors.New("node_evidence requires event schema 2")
	}
	if e.SchemaVersion > 0 && e.SchemaVersion < 4 && e.Type == GateCheck {
		return errors.New("gate_check requires event schema 4")
	}
	var payloadType Type
	switch p := e.Payload.(type) {
	case LegacyGateCheckPayload:
		if e.SchemaVersion != 4 {
			return errors.New("legacy gate_check requires schema 4")
		}
		payloadType = GateCheck
		if err := p.Validate(); err != nil {
			return err
		}
	case GateCheckPayload:
		if e.SchemaVersion != 0 && e.SchemaVersion != 5 {
			return errors.New("full check result requires schema 5")
		}
		payloadType = GateCheck
		if err := p.Validate(); err != nil {
			return err
		}
	case TextDeltaPayload:
		payloadType = TextDelta
	case ToolCallPayload:
		if e.Type == ToolCallStarted || e.Type == ToolCallFinished {
			payloadType = e.Type
		}
	case CommandPayload:
		if e.Type == CommandStarted || e.Type == CommandFinished {
			payloadType = e.Type
		}
	case FileChangedPayload:
		payloadType = FileChanged
	case StatusPayload:
		payloadType = Status
	case ProgressOmittedPayload:
		payloadType = ProgressOmitted
	case NodeEvidencePayload:
		payloadType = NodeEvidence
	case RunStartedPayload:
		payloadType = RunStarted
	case NodeReadyPayload:
		payloadType = NodeReady
	case NodeStartedPayload:
		payloadType = NodeStarted
	case LogChunkPayload:
		payloadType = LogChunk
	case NodeFinishedPayload:
		payloadType = NodeFinished
	case NodeTerminatedPayload:
		payloadType = NodeTerminated
	case RunFinishedPayload:
		payloadType = RunFinished
	default:
		return fmt.Errorf("event %q has no supported payload", e.Type)
	}
	if e.Type != payloadType {
		return fmt.Errorf("event %q has payload for %q", e.Type, payloadType)
	}
	return validateProgress(e.Type, e.Payload)
}

func (e Event) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	type wire Event
	return json.Marshal(wire(e))
}

func decodePayload(data json.RawMessage, target any) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("event payload must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (e *Event) UnmarshalJSON(data []byte) error {
	var wire struct {
		SchemaVersion int             `json:"event_schema_version"`
		Type          Type            `json:"type"`
		Timestamp     time.Time       `json:"timestamp"`
		Sequence      uint64          `json:"sequence"`
		RunID         RunID           `json:"run_id"`
		GraphID       string          `json:"graph_id"`
		GraphVersion  string          `json:"graph_version"`
		NodeID        string          `json:"node_id"`
		NodeRunID     NodeRunID       `json:"node_run_id"`
		AttemptID     AttemptID       `json:"attempt_id"`
		Payload       json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.SchemaVersion != 1 && wire.SchemaVersion != 2 && wire.SchemaVersion != 3 && wire.SchemaVersion != 4 && wire.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported event schema version %d", wire.SchemaVersion)
	}
	if len(wire.Payload) == 0 {
		return errors.New("event payload is missing")
	}
	var payload Payload
	if wire.SchemaVersion == 1 && wire.Type == NodeEvidence {
		return errors.New("node_evidence requires event schema 2")
	}
	switch wire.Type {
	case GateCheck:
		if wire.SchemaVersion == 4 {
			payload = LegacyGateCheckPayload{}
		} else {
			payload = GateCheckPayload{}
		}
	case TextDelta:
		payload = TextDeltaPayload{}
	case ToolCallStarted, ToolCallFinished:
		payload = ToolCallPayload{}
	case CommandStarted, CommandFinished:
		payload = CommandPayload{}
	case FileChanged:
		payload = FileChangedPayload{}
	case Status:
		payload = StatusPayload{}
	case ProgressOmitted:
		payload = ProgressOmittedPayload{}
	case NodeEvidence:
		payload = NodeEvidencePayload{}
	case RunStarted:
		payload = RunStartedPayload{}
	case NodeReady:
		payload = NodeReadyPayload{}
	case NodeStarted:
		payload = NodeStartedPayload{}
	case LogChunk:
		payload = LogChunkPayload{}
	case NodeFinished:
		payload = NodeFinishedPayload{}
	case NodeTerminated:
		payload = NodeTerminatedPayload{}
	case RunFinished:
		payload = RunFinishedPayload{}
	default:
		return fmt.Errorf("unknown event type %q", wire.Type)
	}
	switch p := payload.(type) {
	case LegacyGateCheckPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case GateCheckPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case ProgressOmittedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case StatusPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case FileChangedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case CommandPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case ToolCallPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case TextDeltaPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeEvidencePayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case RunStartedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeReadyPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeStartedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case LogChunkPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeFinishedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeTerminatedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case RunFinishedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	}
	decoded := Event{SchemaVersion: wire.SchemaVersion, Type: wire.Type, Timestamp: wire.Timestamp, Sequence: wire.Sequence, RunID: wire.RunID, GraphID: wire.GraphID, GraphVersion: wire.GraphVersion, NodeID: wire.NodeID, NodeRunID: wire.NodeRunID, AttemptID: wire.AttemptID, Payload: payload}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*e = decoded
	return nil
}
