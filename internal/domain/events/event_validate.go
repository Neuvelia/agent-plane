package events

import (
	"errors"
	"fmt"
)

// Validate checks the event envelope before it becomes a serialized fact.
//
//nolint:gocognit,gocyclo,funlen // moved verbatim from event.go (legacy complexity); splitting by event family is tracked separately
func (e Event) Validate() error {
	if e.SchemaVersion < 0 || e.SchemaVersion > SchemaVersion {
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
	case ValueRecordedPayload:
		payloadType = ValueRecorded
		if err := validateValueRecorded(e, p); err != nil {
			return err
		}
	case ReturnEnvelopePayload:
		payloadType = ReturnEnvelope
		if e.SchemaVersion != 0 && e.SchemaVersion < 7 {
			return errors.New("return envelope requires schema 7")
		}
		if len(p.Evidence) > 0 && p.Evidence[0].Kind == EvidenceKind(CheckChangedFilesScope) && e.SchemaVersion != 0 && e.SchemaVersion < 8 {
			return errors.New("changed-files return envelope requires schema 8")
		}
		if err := p.Validate(); err != nil {
			return err
		}
		if p.SourceRunID != e.RunID || p.SourceNodeID != e.NodeID || p.SourceNodeRunID != e.NodeRunID || p.AttemptID != e.AttemptID || p.SourceGraphVersion != e.GraphVersion {
			return errors.New("return envelope source does not match event")
		}
	case ApprovalRequestedPayload:
		payloadType = ApprovalRequested
		if err := validateApproval(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case ApprovalDecidedPayload:
		payloadType = ApprovalDecided
		if err := validateApproval(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case DeclaredScopeConflictsPayload:
		payloadType = DeclaredScopeConflicts
		if e.SchemaVersion != 0 && e.SchemaVersion < 7 {
			return errors.New("declared conflicts require schema 7")
		}
		if err := p.Validate(); err != nil {
			return err
		}

	case WorkspacePreparedPayload:
		payloadType = WorkspacePrepared
		if err := validateWorkspace(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case WorkspaceIntegratedPayload:
		payloadType = WorkspaceIntegrated
		if err := validateWorkspace(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case WorkspaceConflictPayload:
		payloadType = WorkspaceConflict
		if err := validateWorkspace(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case WorkspaceReleasedPayload:
		payloadType = WorkspaceReleased
		if err := validateWorkspace(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case SourceWorkspaceChangedPayload:
		payloadType = SourceWorkspaceChanged
		if err := validateWorkspace(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case PolicyEvaluatedPayload:
		payloadType = PolicyEvaluated
		if err := validatePolicy(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case PolicyViolationPayload:
		payloadType = PolicyViolation
		if err := validatePolicy(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case CancelRequestedPayload:
		payloadType = CancelRequested
		if err := validateCancelRequested(e, p); err != nil {
			return err
		}
	case ProcessTerminatedPayload:
		payloadType = ProcessTerminated
		if err := validateProcessTerminated(e, p); err != nil {
			return err
		}
	case AttemptDiffPayload:
		payloadType = AttemptDiff
		if err := validateAttemptDiffEnvelope(e); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
	case AttemptStartedPayload:
		payloadType = AttemptStarted
		if err := validateAttempt(e); err != nil {
			return err
		}
	case AttemptInputPayload:
		payloadType = AttemptInput
		if err := validateAttempt(e); err != nil {
			return err
		}
	case AttemptFinishedPayload:
		payloadType = AttemptFinished
		if err := validateAttempt(e); err != nil {
			return err
		}
	case LegacyGateCheckPayload:
		if e.SchemaVersion != 4 {
			return errors.New("legacy gate_check requires schema 4")
		}
		payloadType = GateCheck
		if err := p.Validate(); err != nil {
			return err
		}
	case GateCheckPayload:
		if e.SchemaVersion != 0 && (e.SchemaVersion < 5 || e.SchemaVersion > SchemaVersion) {
			return errors.New("full check result requires schema 5")
		}
		if p.Kind == CheckChangedFilesScope && e.SchemaVersion != 0 && e.SchemaVersion < 8 {
			return errors.New("changed-files scope requires schema 8")
		}
		if isPolicyCheckKind(p.Kind) && e.SchemaVersion != 0 && e.SchemaVersion < SchemaPolicy {
			return fmt.Errorf("%s check requires schema %d", p.Kind, SchemaPolicy)
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
		if err := validateTerminalReason(e, p.Status, p.FailureKind, p.Reason); err != nil {
			return err
		}
	case NodeTerminatedPayload:
		payloadType = NodeTerminated
		if err := validateTerminalReason(e, p.Status, p.FailureKind, p.Reason); err != nil {
			return err
		}
	case RunFinishedPayload:
		payloadType = RunFinished
		if err := validateTerminalReason(e, p.Status, p.FailureKind, p.Reason); err != nil {
			return err
		}
	default:
		return fmt.Errorf("event %q has no supported payload", e.Type)
	}
	if e.Type != payloadType {
		return fmt.Errorf("event %q has payload for %q", e.Type, payloadType)
	}
	return validateProgress(e.Type, e.Payload)
}
