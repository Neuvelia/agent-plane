package safeview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"unicode/utf8"

	"agentplane/internal/events"
	"agentplane/internal/history"
	"agentplane/internal/runtime"
)

// ProjectedEvent is the one canonical event byte representation used by live
// replay and durable ingestion. Hash covers JSON.Bytes(), not a raw event.
type ProjectedEvent struct {
	Sequence uint64
	JSON     history.SafeJSON
	Hash     string
}

type Projector struct {
	forbidden []string
	redactor  *runtime.CaptureRedactor
}

func NewProjector(values []string) (*Projector, error) {
	redactor, err := runtime.NewCaptureRedactor(values)
	if err != nil {
		return nil, err
	}
	return &Projector{forbidden: append([]string(nil), values...), redactor: redactor}, nil
}

// ProjectEvent never encodes the input event. A fact whose identity cannot be
// preserved is accounted for by its original sequence outside the event log.
func (p *Projector) ProjectEvent(event events.Event) (ProjectedEvent, string, error) {
	if p == nil || p.redactor == nil {
		return ProjectedEvent{}, "", errors.New("safe event projector is unavailable")
	}
	if event.IdentityRedacted || event.SchemaVersion < 1 || event.SchemaVersion > events.SchemaVersion || event.Sequence == 0 || event.RunID == "" || event.GraphID == "" || event.GraphVersion == "" || event.Timestamp.IsZero() ||
		p.identity(string(event.Type), string(event.RunID), event.GraphID, event.GraphVersion, event.NodeID, string(event.NodeRunID), string(event.AttemptID)) {
		return ProjectedEvent{}, events.OmissionSensitiveIdentity, nil
	}
	safe := event
	var valid bool
	switch item := event.Payload.(type) {
	case events.RunStartedPayload, events.NodeReadyPayload, events.NodeStartedPayload, events.ProgressOmittedPayload, events.AttemptStartedPayload:
		valid = true
	case events.NodeEvidencePayload:
		valid = !p.identity(item.AdapterID, item.AdapterVersion)
		item.ChangedFiles = append([]events.ChangedFile(nil), item.ChangedFiles...)
		for _, change := range item.ChangedFiles {
			valid = valid && !p.identity(change.Path, change.Provenance)
		}
		safe.Payload = item
	case events.LogChunkPayload:
		valid = !p.identity(string(item.Stream))
		item.Data = p.redactor.Redact(item.Data)
		safe.Payload = item
	case events.NodeFinishedPayload:
		valid = !p.identity(item.Status, item.FailureKind)
		item.DiagnosticCodes = append([]string(nil), item.DiagnosticCodes...)
		for _, code := range item.DiagnosticCodes {
			valid = valid && !p.identity(code)
		}
		safe.Payload = item
	case events.NodeTerminatedPayload:
		valid = !p.identity(item.Status, item.FailureKind)
		item.DiagnosticCodes = append([]string(nil), item.DiagnosticCodes...)
		for _, code := range item.DiagnosticCodes {
			valid = valid && !p.identity(code)
		}
		safe.Payload = item
	case events.RunFinishedPayload:
		valid = !p.identity(item.Status, item.FailureKind)
	case events.TextDeltaPayload:
		valid = !p.identity(item.SourceID)
		item.Text = p.content(item.Text)
		safe.Payload = item
	case events.ToolCallPayload:
		valid = !p.identity(item.CallID, item.Name, item.Status)
	case events.CommandPayload:
		valid = !p.identity(item.CommandID, item.Status)
	case events.FileChangedPayload:
		valid = !p.identity(item.Path, item.Action, item.Provenance)
	case events.StatusPayload:
		valid = !p.identity(item.Code)
	case events.AttemptInputPayload:
		valid = !p.identity(item.TaskFingerprint, string(item.PreviousAttemptID), item.PreviousCheckID)
		item.IntendedScope = append([]string(nil), item.IntendedScope...)
		for _, scope := range item.IntendedScope {
			valid = valid && !p.identity(scope)
		}
		safe.Payload = item
	case events.AttemptFinishedPayload:
		valid = !p.identity(item.Outcome, item.CheckID)
	case events.GateCheckPayload:
		item = events.CloneGate(item)
		valid = !p.identity(item.CheckID, string(item.Kind), string(item.Verdict))
		item.Reason = p.content(item.Reason)
		for _, checked := range item.Scope.CheckedInputs {
			valid = valid && !p.identity(checked)
		}
		if item.ChangedFiles != nil {
			valid = valid && !item.ChangedFiles.ScopeRedacted && !p.identity(item.ChangedFiles.Mode, item.ChangedFiles.BaselineHead)
			for _, path := range item.ChangedFiles.AllowedPaths {
				valid = valid && !p.identity(path)
			}
		}
		valid = p.gateEvidence(item.Evidence) && valid
		safe.Payload = item
	case events.LegacyGateCheckPayload:
		item = events.CloneLegacyGate(item)
		valid = !p.identity(item.CheckID, item.Kind, item.Verdict)
		item.Reason = p.content(item.Reason)
		for i := range item.Evidence {
			evidence := &item.Evidence[i]
			valid = valid && !p.identity(evidence.Source, evidence.Path)
			evidence.Stdout = p.redactor.Redact(evidence.Stdout)
			evidence.Stderr = p.redactor.Redact(evidence.Stderr)
		}
		safe.Payload = item
	case events.ReturnEnvelopePayload:
		originalScopePresent := item.IntendedScope != nil
		item = events.CloneEnvelope(item)
		valid = originalScopePresent && !item.ScopeRedacted && !p.identity(item.UnitID, string(item.Verdict), string(item.AttemptID), item.CheckID, string(item.SourceRunID), item.SourceNodeID, string(item.SourceNodeRunID), item.SourceGraphVersion)
		item.Reason = p.content(item.Reason)
		for _, scope := range item.IntendedScope {
			valid = valid && !p.identity(scope)
		}
		valid = p.gateEvidence(item.Evidence) && valid
		safe.Payload = item
	case events.DeclaredScopeConflictsPayload:
		valid = item.Details != nil
		item.Details = append([]events.DeclaredScopeConflict{}, item.Details...)
		for _, detail := range item.Details {
			valid = valid && !p.identity(detail.LeftNodeID, detail.RightNodeID, detail.Reason)
		}
		safe.Payload = item
	}
	if !valid || safe.Validate() != nil {
		return ProjectedEvent{}, events.OmissionSensitiveIdentity, nil
	}
	projection, err := history.ParseSafeEvent(safe, p.forbidden)
	if errors.Is(err, history.ErrPageTooLarge) {
		return ProjectedEvent{}, events.OmissionNotRetained, nil
	}
	if err != nil {
		return ProjectedEvent{}, events.OmissionSensitiveIdentity, nil
	}
	encoded := projection.Bytes()
	sum := sha256.Sum256(encoded)
	return ProjectedEvent{Sequence: event.Sequence, JSON: projection, Hash: "sha256:" + hex.EncodeToString(sum[:])}, "", nil
}

func (p *Projector) identity(values ...string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) || value != "" && !bytes.Equal(p.redactor.Redact([]byte(value)), []byte(value)) {
			return true
		}
	}
	return false
}

func (p *Projector) content(value string) string {
	return string(p.redactor.Redact([]byte(value)))
}

func (p *Projector) gateEvidence(items []events.GateEvidence) bool {
	valid := true
	for i := range items {
		item := &items[i]
		valid = valid && !p.identity(string(item.Kind), string(item.Source), item.Path)
		if item.Reference != nil {
			valid = valid && !p.identity(string(item.Reference.Kind), string(item.Reference.State), item.Reference.Identifier)
			item.Reference.Reason = p.content(item.Reference.Reason)
		}
		item.Stdout = p.redactor.Redact(item.Stdout)
		item.Stderr = p.redactor.Redact(item.Stderr)
	}
	return valid
}
