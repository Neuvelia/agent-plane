package events

// ClonePayload returns a copy that shares no mutable slices or pointers with
// the original, so retained events cannot be changed by later writers.
func ClonePayload(payload Payload) Payload {
	if cloned, ok := cloneEvidencePayload(payload); ok {
		return cloned
	}
	if cloned, ok := cloneLifecyclePayload(payload); ok {
		return cloned
	}
	return payload
}

func cloneEvidencePayload(payload Payload) (Payload, bool) {
	switch p := payload.(type) {
	case ValueRecordedPayload:
		return cloneValueRecorded(p), true
	case LegacyGateCheckPayload:
		return CloneLegacyGate(p), true
	case ReturnEnvelopePayload:
		return CloneEnvelope(p), true
	case GateCheckPayload:
		return CloneGate(p), true
	case NodeEvidencePayload:
		p.ChangedFiles = append([]ChangedFile{}, p.ChangedFiles...)
		return p, true
	case DeclaredScopeConflictsPayload:
		p.Details = append([]DeclaredScopeConflict{}, p.Details...)
		return p, true
	case AttemptInputPayload:
		p.IntendedScope = append([]string{}, p.IntendedScope...)
		return p, true
	}
	return nil, false
}

func cloneLifecyclePayload(payload Payload) (Payload, bool) {
	switch p := payload.(type) {
	case CommandPayload:
		p.ExitCode = cloneInt(p.ExitCode)
		return p, true
	case LogChunkPayload:
		p.Data = append([]byte(nil), p.Data...)
		return p, true
	case NodeFinishedPayload:
		p.DiagnosticCodes = append([]string(nil), p.DiagnosticCodes...)
		p.DurationMS = cloneInt64(p.DurationMS)
		return p, true
	case NodeTerminatedPayload:
		p.DiagnosticCodes = append([]string(nil), p.DiagnosticCodes...)
		return p, true
	}
	return nil, false
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}

func cloneInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}
