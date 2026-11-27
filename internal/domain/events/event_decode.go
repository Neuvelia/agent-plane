package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func decodePayload(data json.RawMessage, target any) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("event payload must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// UnmarshalJSON strictly decodes an event envelope and its typed payload.
//
//nolint:gocognit,gocyclo,funlen // moved verbatim from event.go (legacy complexity); splitting by event family is tracked separately
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
	if wire.SchemaVersion < 1 || wire.SchemaVersion > SchemaVersion {
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
	case ValueRecorded:
		payload = ValueRecordedPayload{}
	case ReturnEnvelope:
		payload = ReturnEnvelopePayload{}
	case DeclaredScopeConflicts:
		payload = DeclaredScopeConflictsPayload{}
	case ApprovalRequested:
		payload = ApprovalRequestedPayload{}
	case ApprovalDecided:
		payload = ApprovalDecidedPayload{}
	case WorkspacePrepared:
		payload = WorkspacePreparedPayload{}
	case WorkspaceIntegrated:
		payload = WorkspaceIntegratedPayload{}
	case WorkspaceConflict:
		payload = WorkspaceConflictPayload{}
	case WorkspaceReleased:
		payload = WorkspaceReleasedPayload{}
	case SourceWorkspaceChanged:
		payload = SourceWorkspaceChangedPayload{}
	case PolicyEvaluated:
		payload = PolicyEvaluatedPayload{}
	case PolicyViolation:
		payload = PolicyViolationPayload{}
	case CancelRequested:
		payload = CancelRequestedPayload{}
	case ProcessTerminated:
		payload = ProcessTerminatedPayload{}
	case ExecutorAbandoned:
		payload = ExecutorAbandonedPayload{}
	case AttemptDiff:
		payload = AttemptDiffPayload{}
	case AttemptStarted:
		payload = AttemptStartedPayload{}
	case AttemptInput:
		payload = AttemptInputPayload{}
	case AttemptFinished:
		payload = AttemptFinishedPayload{}
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
	case ValueRecordedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case ReturnEnvelopePayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case DeclaredScopeConflictsPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case ApprovalRequestedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case ApprovalDecidedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case WorkspacePreparedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case WorkspaceIntegratedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case WorkspaceConflictPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case WorkspaceReleasedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case SourceWorkspaceChangedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case PolicyEvaluatedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case CancelRequestedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case PolicyViolationPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case ProcessTerminatedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case ExecutorAbandonedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case AttemptDiffPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case AttemptStartedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case AttemptInputPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
	case AttemptFinishedPayload:
		if err := decodePayload(wire.Payload, &p); err != nil {
			return err
		}
		payload = p
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
