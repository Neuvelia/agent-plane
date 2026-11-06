package safeview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/history"
)

func eventFixture(kind events.Type, payload events.Payload) events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion, Type: kind,
		Timestamp: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), Sequence: 7,
		RunID: "rid", GraphID: "graph", GraphVersion: "hash", NodeID: "node", NodeRunID: "nrid",
		Payload: payload,
	}
}

func TestProjectEventRedactsRawEvidenceBeforeBase64WithoutMutatingInput(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	payload := events.GateCheckPayload{
		Version: 1, Scope: events.CheckScope{CheckedInputs: []string{"candidate"}},
		CheckID: "check", Kind: events.CheckCommand, Verdict: events.CheckGreen,
		Reason: "private reason", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckCommand), Source: events.EvidenceSource(events.CheckCommand), ExitCode: &zero, Stdout: []byte("before private after")}},
	}
	event := eventFixture(events.GateCheck, payload)
	projected, omission, err := projector.ProjectEvent(event)
	if err != nil || omission != "" || projected.Sequence != 7 {
		t.Fatalf("projection=%+v omission=%q err=%v", projected, omission, err)
	}
	var safe events.Event
	if err := json.Unmarshal(projected.JSON.Bytes(), &safe); err != nil {
		t.Fatal(err)
	}
	check := safe.Payload.(events.GateCheckPayload)
	if check.Reason != "*** reason" || string(check.Evidence[0].Stdout) != "before *** after" {
		t.Fatalf("unsafe Gate content: %+v", check)
	}
	if payload.Reason != "private reason" || string(payload.Evidence[0].Stdout) != "before private after" {
		t.Fatal("input payload mutated")
	}
	if bytes.Contains(projected.JSON.Bytes(), []byte("private")) || bytes.Contains(projected.JSON.Bytes(), []byte("cHJpdmF0ZQ")) {
		t.Fatal("raw or base64 private value in safe JSON")
	}
	sum := sha256.Sum256(projected.JSON.Bytes())
	if projected.Hash != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %q is not canonical JSON hash", projected.Hash)
	}
	fromHistory, err := history.ParseSafeEvent(safe, []string{"private"})
	if err != nil || !bytes.Equal(fromHistory.Bytes(), projected.JSON.Bytes()) {
		t.Fatalf("history/live bytes differ: %v", err)
	}
}

func TestProjectEventKeepsRedactedContentButOmitsPrivateIdentities(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []events.Event{
		eventFixture(events.FileChanged, events.FileChangedPayload{Path: "src/private.go", Action: "update", Provenance: "reported"}),
		eventFixture(events.TextDelta, events.TextDeltaPayload{SourceID: "private-source", Text: "ordinary"}),
		eventFixture(events.NodeFinished, events.NodeFinishedPayload{Status: "succeeded", DiagnosticCodes: []string{"private-code"}}),
	} {
		if _, omission, err := projector.ProjectEvent(event); err != nil || omission != events.OmissionSensitiveIdentity {
			t.Fatalf("identity event %s omission=%q err=%v", event.Type, omission, err)
		}
	}
	content := eventFixture(events.TextDelta, events.TextDeltaPayload{SourceID: "source", Text: "before private after"})
	projected, omission, err := projector.ProjectEvent(content)
	if err != nil || omission != "" || !bytes.Contains(projected.JSON.Bytes(), []byte("before *** after")) {
		t.Fatalf("content projection=%q omission=%q err=%v", projected.JSON.Bytes(), omission, err)
	}
	content.IdentityRedacted = true
	if _, omission, err := projector.ProjectEvent(content); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("pre-redacted identity omission=%q err=%v", omission, err)
	}
}

func TestProjectEventFixedKeysAndSizeLimit(t *testing.T) {
	projector, err := NewProjector([]string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, omission, err := projector.ProjectEvent(eventFixture(events.NodeReady, events.NodeReadyPayload{})); err != nil || omission != "" {
		t.Fatalf("fixed key falsely treated as secret: %q %v", omission, err)
	}
	big := eventFixture(events.NodeEvidence, events.NodeEvidencePayload{AdapterID: "adapter", AdapterVersion: strings.Repeat("x", 3<<20)})
	if _, omission, err := projector.ProjectEvent(big); err != nil || omission != events.OmissionNotRetained {
		t.Fatalf("oversized event omission=%q err=%v", omission, err)
	}
}

func TestProjectEventCoversEveryCurrentPayloadFamily(t *testing.T) {
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	check := events.GateCheckPayload{Version: 1, Scope: events.CheckScope{CheckedInputs: []string{"candidate"}}, CheckID: "check", Kind: events.CheckCommand, Verdict: events.CheckGreen, Reason: "passed", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckCommand), Source: events.EvidenceSource(events.CheckCommand), ExitCode: &zero}}}
	envelope := events.ReturnEnvelopePayload{Version: 1, UnitID: "node", Verdict: events.CheckGreen, Reason: "passed", Evidence: events.CloneGate(check).Evidence, IntendedScope: []string{}, AttemptID: "aid", CheckID: "check", SourceRunID: "rid", SourceNodeID: "node", SourceNodeRunID: "nrid", SourceGraphVersion: "hash"}
	cases := []struct {
		kind    events.Type
		payload events.Payload
	}{
		{events.RunStarted, events.RunStartedPayload{NodeCount: 1}},
		{events.NodeEvidence, events.NodeEvidencePayload{AdapterID: "adapter", AdapterVersion: "v1", ChangedFiles: []events.ChangedFile{{Path: "src/a.go", Provenance: "git"}}}},
		{events.NodeReady, events.NodeReadyPayload{}},
		{events.NodeStarted, events.NodeStartedPayload{}},
		{events.LogChunk, events.LogChunkPayload{Stream: events.Stdout, Data: []byte("output")}},
		{events.NodeFinished, events.NodeFinishedPayload{Status: "succeeded"}},
		{events.NodeTerminated, events.NodeTerminatedPayload{Status: "skipped"}},
		{events.RunFinished, events.RunFinishedPayload{Status: "succeeded"}},
		{events.TextDelta, events.TextDeltaPayload{SourceID: "source", Text: "delta"}},
		{events.ToolCallStarted, events.ToolCallPayload{CallID: "call", Name: "tool", Status: "in_progress"}},
		{events.ToolCallFinished, events.ToolCallPayload{CallID: "call", Name: "tool", Status: "completed"}},
		{events.CommandStarted, events.CommandPayload{CommandID: "command", Status: "in_progress"}},
		{events.CommandFinished, events.CommandPayload{CommandID: "command", Status: "completed"}},
		{events.FileChanged, events.FileChangedPayload{Path: "src/a.go", Action: "update", Provenance: "reported"}},
		{events.Status, events.StatusPayload{Code: "working"}},
		{events.ProgressOmitted, events.ProgressOmittedPayload{RetainedEvents: 1}},
		{events.GateCheck, check},
		{events.AttemptStarted, events.AttemptStartedPayload{Number: 1, MaxAttempts: 3}},
		{events.AttemptInput, events.AttemptInputPayload{TaskFingerprint: strings.Repeat("a", 64), ScopePresent: true, IntendedScope: []string{}}},
		{events.AttemptFinished, events.AttemptFinishedPayload{Outcome: "execution_error"}},
		{events.ReturnEnvelope, envelope},
		{events.DeclaredScopeConflicts, events.DeclaredScopeConflictsPayload{Details: []events.DeclaredScopeConflict{}, Omitted: 0}},
	}
	for _, test := range cases {
		t.Run(string(test.kind), func(t *testing.T) {
			event := eventFixture(test.kind, test.payload)
			if test.kind == events.AttemptStarted || test.kind == events.AttemptInput || test.kind == events.AttemptFinished || test.kind == events.ReturnEnvelope {
				event.AttemptID = "aid"
			}
			if err := event.Validate(); err != nil {
				t.Fatalf("invalid test fixture: %v", err)
			}
			projection, omission, err := projector.ProjectEvent(event)
			if err != nil || omission != "" || len(projection.JSON.Bytes()) == 0 {
				t.Fatalf("projection=%q omission=%q err=%v", projection.JSON.Bytes(), omission, err)
			}
		})
	}
}

func TestProjectEventOmitsReferenceAndEnumIdentitiesButRetainsReasonRedaction(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	base := events.GateCheckPayload{Version: 1, Scope: events.CheckScope{CheckedInputs: []string{"candidate"}}, CheckID: "check", Kind: events.CheckCommand, Verdict: events.CheckGreen, Reason: "passed", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckCommand), Source: events.EvidenceSource(events.CheckCommand), ExitCode: &zero, Reference: &events.EvidenceReference{Kind: "file", State: "known", Identifier: "src/private.go"}}}}
	if _, omission, err := projector.ProjectEvent(eventFixture(events.GateCheck, base)); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("private reference omission=%q err=%v", omission, err)
	}
	base.Evidence[0].Reference = &events.EvidenceReference{Kind: "file", State: "unavailable", Reason: "private reason"}
	projection, omission, err := projector.ProjectEvent(eventFixture(events.GateCheck, base))
	if err != nil || omission != "" || !bytes.Contains(projection.JSON.Bytes(), []byte(`"reason":"*** reason"`)) {
		t.Fatalf("reference reason projection=%q omission=%q err=%v", projection.JSON.Bytes(), omission, err)
	}
	if base.Evidence[0].Reference.Reason != "private reason" {
		t.Fatal("reference reason mutated")
	}
	green, err := NewProjector([]string{"green"})
	if err != nil {
		t.Fatal(err)
	}
	if _, omission, err := green.ProjectEvent(eventFixture(events.GateCheck, base)); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("private required enum omission=%q err=%v", omission, err)
	}
}

func TestProjectEventOmitsPreviouslyRedactedScopeIdentity(t *testing.T) {
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	check := events.GateCheckPayload{Version: 1, Scope: events.CheckScope{CheckedInputs: []string{"candidate"}}, CheckID: "check", Kind: events.CheckChangedFilesScope, Verdict: events.CheckGreen, Reason: "passed", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckChangedFilesScope), Source: "git"}}, ChangedFiles: &events.ChangedFilesScopeDetails{Mode: "warn", AllowedPaths: []string{"src/**"}, ScopeRedacted: true, BaselineHead: "head", ObservedCount: 0}}
	if _, omission, err := projector.ProjectEvent(eventFixture(events.GateCheck, check)); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("redacted Gate scope omission=%q err=%v", omission, err)
	}
	envelope := events.ReturnEnvelopePayload{Version: 1, UnitID: "node", Verdict: events.CheckGreen, Reason: "passed", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckCommand), Source: "command", ExitCode: &zero}}, IntendedScope: []string{"src/"}, ScopeRedacted: true, AttemptID: "aid", CheckID: "check", SourceRunID: "rid", SourceNodeID: "node", SourceNodeRunID: "nrid", SourceGraphVersion: "hash"}
	event := eventFixture(events.ReturnEnvelope, envelope)
	event.AttemptID = "aid"
	if _, omission, err := projector.ProjectEvent(event); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("redacted Return Envelope scope omission=%q err=%v", omission, err)
	}
}

func TestProjectEventHandlesLegacyGateAndRejectsUnknownPayload(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	legacy := eventFixture(events.GateCheck, events.LegacyGateCheckPayload{CheckID: "check", Kind: "command", Verdict: "green", Reason: "private reason", Evidence: []events.LegacyGateEvidence{{Source: "command", ExitCode: &zero, Stdout: []byte("private output")}}})
	legacy.SchemaVersion = 4
	projection, omission, err := projector.ProjectEvent(legacy)
	if err != nil || omission != "" {
		t.Fatalf("legacy Gate projection=%q omission=%q err=%v", projection.JSON.Bytes(), omission, err)
	}
	var safe events.Event
	if err := json.Unmarshal(projection.JSON.Bytes(), &safe); err != nil {
		t.Fatal(err)
	}
	payload := safe.Payload.(events.LegacyGateCheckPayload)
	if payload.Reason != "*** reason" || string(payload.Evidence[0].Stdout) != "*** output" {
		t.Fatalf("legacy Gate leak: %+v", payload)
	}
	unknown := eventFixture(events.NodeReady, nil)
	if _, omission, err := projector.ProjectEvent(unknown); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("unknown payload omission=%q err=%v", omission, err)
	}
	mismatched := eventFixture(events.NodeReady, events.NodeStartedPayload{})
	if _, omission, err := projector.ProjectEvent(mismatched); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("mismatched payload omission=%q err=%v", omission, err)
	}
}

func TestProjectEventOmitsInvalidUTF8IdentityBeforeJSONReplacement(t *testing.T) {
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	event := eventFixture(events.NodeReady, events.NodeReadyPayload{})
	event.GraphID = string([]byte{'g', 0xff})
	if _, omission, err := projector.ProjectEvent(event); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("invalid graph ID omission=%q err=%v", omission, err)
	}
	event = eventFixture(events.NodeEvidence, events.NodeEvidencePayload{AdapterID: string([]byte{'a', 0xff})})
	if _, omission, err := projector.ProjectEvent(event); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("invalid adapter ID omission=%q err=%v", omission, err)
	}
}

func TestProjectEventDoesNotRepairInvalidNilPayloadCollections(t *testing.T) {
	projector, err := NewProjector(nil)
	if err != nil {
		t.Fatal(err)
	}
	conflicts := eventFixture(events.DeclaredScopeConflicts, events.DeclaredScopeConflictsPayload{Details: nil})
	if _, omission, err := projector.ProjectEvent(conflicts); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("nil conflict details repaired: omission=%q err=%v", omission, err)
	}
	zero := 0
	envelope := events.ReturnEnvelopePayload{Version: 1, UnitID: "node", Verdict: events.CheckGreen, Reason: "passed", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckCommand), Source: "command", ExitCode: &zero}}, IntendedScope: nil, AttemptID: "aid", CheckID: "check", SourceRunID: "rid", SourceNodeID: "node", SourceNodeRunID: "nrid", SourceGraphVersion: "hash"}
	event := eventFixture(events.ReturnEnvelope, envelope)
	event.AttemptID = "aid"
	if _, omission, err := projector.ProjectEvent(event); err != nil || omission != events.OmissionSensitiveIdentity {
		t.Fatalf("nil Return Envelope scope repaired: omission=%q err=%v", omission, err)
	}
}
