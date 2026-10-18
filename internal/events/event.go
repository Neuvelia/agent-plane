package events

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const SchemaVersion = 1

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
	if wire.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported event schema version %d", wire.SchemaVersion)
	}
	if len(wire.Payload) == 0 {
		return errors.New("event payload is missing")
	}
	var payload Payload
	switch wire.Type {
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
	case RunStartedPayload:
		if err := json.Unmarshal(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeReadyPayload:
		if err := json.Unmarshal(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeStartedPayload:
		if err := json.Unmarshal(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case LogChunkPayload:
		if err := json.Unmarshal(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeFinishedPayload:
		if err := json.Unmarshal(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case NodeTerminatedPayload:
		if err := json.Unmarshal(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case RunFinishedPayload:
		if err := json.Unmarshal(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	}
	*e = Event{SchemaVersion: wire.SchemaVersion, Type: wire.Type, Timestamp: wire.Timestamp, Sequence: wire.Sequence, RunID: wire.RunID, GraphID: wire.GraphID, GraphVersion: wire.GraphVersion, NodeID: wire.NodeID, NodeRunID: wire.NodeRunID, AttemptID: wire.AttemptID, Payload: payload}
	return nil
}
