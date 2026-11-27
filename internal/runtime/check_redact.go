package runtime

import (
	"encoding/json"
	"sort"

	"agentplane/internal/domain/events"
)

// redactedCheck returns the copy of an observed Gate check that enters the
// trace: every text field and byte evidence redacted, changed-files evidence
// kept within the payload budget, and the result validated. valid is false
// when the redacted check no longer satisfies the event contract.
func redactedCheck(p events.GateCheckPayload, redact func(string) string, template *logRedactor) (events.GateCheckPayload, bool) {
	snapshot := events.CloneGate(p)
	snapshot.CheckID = redact(snapshot.CheckID)
	snapshot.Reason = redact(snapshot.Reason)
	if snapshot.ChangedFiles != nil {
		redactAllowedPaths(snapshot.ChangedFiles, redact)
	}
	for i := range snapshot.Scope.CheckedInputs {
		snapshot.Scope.CheckedInputs[i] = redact(snapshot.Scope.CheckedInputs[i])
	}
	sort.Strings(snapshot.Scope.CheckedInputs)
	for i := range snapshot.Evidence {
		redactGateEvidence(&snapshot.Evidence[i], redact, template)
	}
	if snapshot.Kind == events.CheckChangedFilesScope {
		sortPathEvidence(snapshot.Evidence)
		snapshot.Evidence = boundPathEvidence(snapshot)
	}
	return snapshot, snapshot.Validate() == nil
}

// redactAllowedPaths redacts the declared scope, sorted; when redaction
// changed a path it marks the scope redacted and drops duplicates it created.
func redactAllowedPaths(scope *events.ChangedFilesScopeDetails, redact func(string) string) {
	for i, path := range scope.AllowedPaths {
		scope.AllowedPaths[i] = redact(path)
		scope.ScopeRedacted = scope.ScopeRedacted || scope.AllowedPaths[i] != path
	}
	sort.Strings(scope.AllowedPaths)
	if !scope.ScopeRedacted {
		return
	}
	unique := scope.AllowedPaths[:0]
	for _, path := range scope.AllowedPaths {
		if len(unique) == 0 || unique[len(unique)-1] != path {
			unique = append(unique, path)
		}
	}
	scope.AllowedPaths = unique
}

// redactGateEvidence redacts one evidence item. Byte evidence is redacted again
// at the trust boundary for custom executors.
func redactGateEvidence(e *events.GateEvidence, redact func(string) string, template *logRedactor) {
	e.Path = redact(e.Path)
	if e.Reference != nil {
		e.Reference.Identifier = redact(e.Reference.Identifier)
		e.Reference.Reason = redact(e.Reference.Reason)
	}
	e.Stdout = redactBytes(template, events.Stdout, e.Stdout)
	e.Stderr = redactBytes(template, events.Stderr, e.Stderr)
}

func redactBytes(template *logRedactor, stream events.Stream, data []byte) []byte {
	r := template.clone()
	out := append([]byte(nil), r.Write(stream, data)...)
	return append(out, r.Flush(stream)...)
}

// sortPathEvidence orders path evidence by path, with path-less items last.
func sortPathEvidence(evidence []events.GateEvidence) {
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Path == "" {
			return false
		}
		if evidence[j].Path == "" {
			return true
		}
		return evidence[i].Path < evidence[j].Path
	})
}

// boundPathEvidence keeps a changed-files check within MaxGatePayload.
// Redaction can expand path text after the Gate has applied its payload
// budget, so excess path entries are replaced by a counted omission marker.
func boundPathEvidence(snapshot events.GateCheckPayload) []events.GateEvidence {
	for {
		encoded, err := json.Marshal(snapshot)
		if err != nil || len(encoded) <= events.MaxGatePayload {
			return snapshot.Evidence
		}
		lastPath := lastPathEvidence(snapshot.Evidence)
		if lastPath < 0 {
			return snapshot.Evidence
		}
		snapshot.Evidence = append(snapshot.Evidence[:lastPath], snapshot.Evidence[lastPath+1:]...)
		snapshot.Evidence = countOmittedPath(snapshot.Evidence)
	}
}

func lastPathEvidence(evidence []events.GateEvidence) int {
	for i := len(evidence) - 1; i >= 0; i-- {
		if evidence[i].Path != "" {
			return i
		}
	}
	return -1
}

func countOmittedPath(evidence []events.GateEvidence) []events.GateEvidence {
	last := len(evidence) - 1
	if last >= 0 && evidence[last].Path == "" && evidence[last].Omitted > 0 {
		evidence[last].Omitted++
		return evidence
	}
	return append(evidence, events.GateEvidence{Kind: events.EvidenceKind(events.CheckChangedFilesScope), Source: "git", Omitted: 1, Reference: &events.EvidenceReference{Kind: "diff", State: "unavailable", Reason: "paths_exceed_evidence_limit"}})
}

// redactedEvidence returns the copy of a node's Git evidence that enters the
// trace, with the adapter identity and every changed path redacted.
func redactedEvidence(evidence events.NodeEvidencePayload, template *logRedactor) *events.NodeEvidencePayload {
	snapshot := evidence
	snapshot.ChangedFiles = append([]events.ChangedFile{}, evidence.ChangedFiles...)
	redact := func(s string) string { return string(redactBytes(template, events.Stdout, []byte(s))) }
	snapshot.AdapterID = redact(snapshot.AdapterID)
	snapshot.AdapterVersion = redact(snapshot.AdapterVersion)
	for i := range snapshot.ChangedFiles {
		snapshot.ChangedFiles[i].Path = redact(snapshot.ChangedFiles[i].Path)
	}
	return &snapshot
}
