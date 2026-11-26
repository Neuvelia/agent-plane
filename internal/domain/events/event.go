package events

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the event schema new events are stamped with. Readers
// accept every earlier schema with its historical shape.
const SchemaVersion = SchemaPolicy

type RunID string
type NodeRunID string

// AttemptID identifies one scheduler-owned produce/check cycle.
type AttemptID string

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

// RunStartedPayload opens a run. The optional fields are additive (F2.9, F3.6):
// a log written before they existed decodes with them empty, and they are
// omitted when the repository or Git is unavailable.
type RunStartedPayload struct {
	NodeCount int `json:"node_count"`
	// RepoName is the base name of the Git work tree the run started in, never
	// a path. RepoBranch is empty on a detached HEAD.
	RepoName   string `json:"repo_name,omitempty"`
	RepoBranch string `json:"repo_branch,omitempty"`
	RepoCommit string `json:"repo_commit,omitempty"`
	// RuntimeVersion is the engine version that produced the run.
	RuntimeVersion string `json:"runtime_version,omitempty"`
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
	Reason          string   `json:"reason,omitempty"`
}

// NodeTerminated records a terminal transition that never entered running.
type NodeTerminatedPayload struct {
	Status          string   `json:"status"`
	FailureKind     string   `json:"failure_kind,omitempty"`
	DiagnosticCodes []string `json:"diagnostic_codes,omitempty"`
	Reason          string   `json:"reason,omitempty"`
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
	Reason      string     `json:"reason,omitempty"`
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

func (e Event) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	type wire Event
	return json.Marshal(wire(e))
}
