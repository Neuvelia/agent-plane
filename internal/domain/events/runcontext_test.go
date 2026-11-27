package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func runContextEvent(schema int, payload Payload, typ Type) Event {
	e := Event{SchemaVersion: schema, Type: typ, Timestamp: time.Unix(1, 0).UTC(), Sequence: 1, RunID: "run", GraphID: "g", GraphVersion: "sha256:" + strings.Repeat("a", 64), Payload: payload}
	if typ != RunStarted {
		e.NodeID, e.NodeRunID = "a", "nr"
	}
	return e
}

func checkResult(kind CheckKind) GateCheckPayload {
	exit := 1
	return GateCheckPayload{Version: 1, Scope: CheckScope{CheckedInputs: []string{"candidate"}}, CheckID: "tests", Kind: kind, Verdict: CheckRed, Reason: "command exited 1",
		Evidence: []GateEvidence{{Kind: EvidenceKind(kind), Source: EvidenceSource(kind), ExitCode: &exit}}}
}

// roadmap: F3.6-AC1
func TestRunContextFactsRequireSchema15(t *testing.T) {
	gate := runContextEvent(SchemaRunContext, checkResult(CheckTestCommand), GateCheck)
	gate.AttemptID = "att"
	facts := map[string]Event{
		"run_started provenance": runContextEvent(SchemaRunContext, RunStartedPayload{NodeCount: 1, RepoName: "api", RepoBranch: "main", RepoCommit: "abc", RuntimeVersion: "v1"}, RunStarted),
		"test_command check":     gate,
		"executor_abandoned":     runContextEvent(SchemaRunContext, ExecutorAbandonedPayload{Reason: ReasonRunTimeout, BoundMS: 7000}, ExecutorAbandoned),
	}
	for name, e := range facts {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("%s under schema %d: %v", name, SchemaRunContext, err)
		}
		var back Event
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("%s round trip: %v", name, err)
		}
		older := strings.Replace(string(data), fmt.Sprintf(`"event_schema_version":%d`, SchemaRunContext), fmt.Sprintf(`"event_schema_version":%d`, SchemaRunContext-1), 1)
		if json.Unmarshal([]byte(older), &back) == nil {
			t.Fatalf("%s must not decode under schema %d", name, SchemaRunContext-1)
		}
		e.SchemaVersion = SchemaRunContext - 1
		if _, err := json.Marshal(e); err == nil {
			t.Fatalf("%s must not be written under schema %d", name, SchemaRunContext-1)
		}
	}
}

func TestRunStartedWithoutProvenanceStaysValidOnOlderSchemas(t *testing.T) {
	e := runContextEvent(1, RunStartedPayload{NodeCount: 2}, RunStarted)
	if _, err := json.Marshal(e); err != nil {
		t.Fatal(err)
	}
}

func TestRunStartedRepoNameIsABaseName(t *testing.T) {
	for _, name := range []string{"/home/u/api", `C:\src\api`, strings.Repeat("a", MaxRunContextText+1), "a\nb"} {
		e := runContextEvent(SchemaRunContext, RunStartedPayload{NodeCount: 1, RepoName: name}, RunStarted)
		if _, err := json.Marshal(e); err == nil {
			t.Fatalf("repo_name %q accepted", name)
		}
	}
}

func TestExecutorAbandonedValidatesReasonAndCorrelation(t *testing.T) {
	bad := runContextEvent(SchemaRunContext, ExecutorAbandonedPayload{Reason: ReasonInterrupt}, ExecutorAbandoned)
	if _, err := json.Marshal(bad); err == nil {
		t.Fatal("an interrupt is not an abandon reason")
	}
	unscoped := runContextEvent(SchemaRunContext, ExecutorAbandonedPayload{Reason: ReasonNodeTimeout}, ExecutorAbandoned)
	unscoped.NodeID, unscoped.NodeRunID = "", ""
	if _, err := json.Marshal(unscoped); err == nil {
		t.Fatal("executor_abandoned needs node correlation")
	}
}
