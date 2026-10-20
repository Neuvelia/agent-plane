package events

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChangedFilesCheckResultRoundTripAndSchema(t *testing.T) {
	p := CheckResult{Version: 1, CheckID: "scope", Kind: CheckChangedFilesScope, Verdict: CheckRed, Reason: "changed_files_outside_scope", Scope: CheckScope{CheckedInputs: []string{"candidate"}}, ChangedFiles: &ChangedFilesScopeDetails{Mode: "deny", AllowedPaths: []string{"src/"}, BaselineHead: "0123456789abcdef", ObservedCount: 2}, Evidence: []GateEvidence{{Kind: EvidenceKind(CheckChangedFilesScope), Source: "git", Path: "outside.go"}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	e := Event{SchemaVersion: SchemaVersion, Type: GateCheck, RunID: "run", NodeRunID: "node", Payload: p}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	got := CheckResults([]Event{decoded}, "run", "node")
	if len(got) != 1 || !reflect.DeepEqual(got[0], p) {
		t.Fatalf("got=%+v", got)
	}
	got[0].ChangedFiles.AllowedPaths[0] = "mutated"
	if e.Payload.(CheckResult).ChangedFiles.AllowedPaths[0] != "src/" {
		t.Fatal("result lookup aliases retained payload")
	}
	e.SchemaVersion = 7
	if err := e.Validate(); err == nil {
		t.Fatal("schema 7 accepted F1.8 check")
	}
}
