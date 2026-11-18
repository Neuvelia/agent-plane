package events

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

// ValueRecorded exists from event schema 10 (ADR-0023). It is emitted only when
// value recording is enabled for a run, once per supplied graph input and once
// per accepted output port.
const ValueRecorded Type = "value_recorded"

// Value recording bounds are part of the contract (ADR-0023): at most
// MaxValueBytes of text per event and MaxRunValueBytes of carried text per run.
const (
	MaxValueBytes    = 16 * 1024
	MaxRunValueBytes = 1 << 20
	// GraphInputPortPrefix marks a graph input in ValueRecordedPayload.Port.
	GraphInputPortPrefix = "inputs."
)

// ValueRecordedPayload is one bounded, already-redacted runtime value. Size and
// SHA256 describe the full redacted text before any cut; Value is nil only when
// nothing could be carried, which always implies Truncated.
type ValueRecordedPayload struct {
	NodeRunID NodeRunID `json:"node_run_id,omitempty"`
	AttemptID AttemptID `json:"attempt_id,omitempty"`
	Port      string    `json:"port"`
	Type      string    `json:"type"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
	Truncated bool      `json:"truncated"`
	Redacted  bool      `json:"redacted"`
	Value     *string   `json:"value,omitempty"`
}

func (ValueRecordedPayload) eventPayload() {}

var recordedValueTypes = map[string]bool{"string": true, "number": true, "boolean": true, "json": true, "artifact_ref": true}

// Validate checks the payload's own bounds and internal consistency.
func (p ValueRecordedPayload) Validate() error {
	if !recordedValueTypes[p.Type] {
		return errors.New("recorded value has an unknown type")
	}
	if !validRecordedPort(p.Port) {
		return errors.New("recorded value port must be a graph identifier or inputs.<identifier>")
	}
	if p.SizeBytes < 0 {
		return errors.New("recorded value size must not be negative")
	}
	if len(p.SHA256) != 64 || strings.ToLower(p.SHA256) != p.SHA256 {
		return errors.New("recorded value sha256 must be 64 lowercase hex characters")
	}
	if _, err := hex.DecodeString(p.SHA256); err != nil {
		return errors.New("recorded value sha256 must be 64 lowercase hex characters")
	}
	if p.Value == nil {
		if !p.Truncated {
			return errors.New("recorded value without text must be marked truncated")
		}
		return nil
	}
	v := *p.Value
	if len(v) > MaxValueBytes {
		return errors.New("recorded value exceeds limit")
	}
	if !utf8.ValidString(v) {
		return errors.New("recorded value must be valid UTF-8")
	}
	if p.Truncated {
		if int64(len(v)) >= p.SizeBytes {
			return errors.New("truncated recorded value must be shorter than its size")
		}
		return nil
	}
	if int64(len(v)) != p.SizeBytes {
		return errors.New("recorded value size does not match its text")
	}
	sum := sha256.Sum256([]byte(v))
	if hex.EncodeToString(sum[:]) != p.SHA256 {
		return errors.New("recorded value sha256 does not match its text")
	}
	return nil
}

// validRecordedPort accepts exactly the port forms a valid graph can produce:
// an output port identifier or GraphInputPortPrefix plus a graph input
// identifier. It mirrors graph.ValidIdentifier (docs/graph-spec.md) without a
// length cap, because the graph contract has none and appendEvent panics on an
// invalid event; events stays free of a graph import.
func validRecordedPort(port string) bool {
	return validPortIdentifier(strings.TrimPrefix(port, GraphInputPortPrefix))
}

func validPortIdentifier(identifier string) bool {
	if identifier == "" || identifier[0] < 'a' || identifier[0] > 'z' {
		return false
	}
	for _, character := range identifier[1:] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func validateValueRecorded(e Event, p ValueRecordedPayload) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < 10 {
		return errors.New("value_recorded requires event schema 10")
	}
	if p.NodeRunID != e.NodeRunID || p.AttemptID != e.AttemptID {
		return errors.New("recorded value identifiers do not match event")
	}
	if strings.HasPrefix(p.Port, GraphInputPortPrefix) {
		if e.NodeID != "" || e.NodeRunID != "" || e.AttemptID != "" || len(p.Port) == len(GraphInputPortPrefix) {
			return errors.New("recorded graph input must not name a node")
		}
	} else if e.NodeID == "" || e.NodeRunID == "" || strings.Contains(p.Port, ".") {
		return errors.New("recorded output requires node and node run IDs")
	}
	return p.Validate()
}

func cloneValueRecorded(p ValueRecordedPayload) ValueRecordedPayload {
	if p.Value != nil {
		v := *p.Value
		p.Value = &v
	}
	return p
}
