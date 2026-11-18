package trace

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/domain/events"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func gateFact() events.GateCheckPayload {
	return events.GateCheckPayload{Version: 1, Scope: events.CheckScope{CheckedInputs: []string{"data"}}, CheckID: "check", Kind: "json_predicate", Verdict: "red", Reason: "mismatch", Evidence: []events.GateEvidence{{Kind: "json_predicate", Source: "json_predicate"}}}
}
func TestGateSchemaCompatibilityAndValidation(t *testing.T) {
	for version := 1; version <= 5; version++ {
		e := events.Event{SchemaVersion: version, Type: events.GateCheck, Payload: gateFact()}
		b, err := json.Marshal(e)
		if version < 5 {
			if err == nil {
				t.Fatal("old schema accepted gate")
			}
			continue
		}
		var got events.Event
		if err != nil || json.Unmarshal(b, &got) != nil || got.Payload.(events.GateCheckPayload).CheckID != "check" {
			t.Fatal("gate roundtrip failed")
		}
	}
	for version := 1; version <= 5; version++ {
		e := events.Event{SchemaVersion: version, Type: events.RunStarted, Payload: events.RunStartedPayload{}}
		b, err := json.Marshal(e)
		var got events.Event
		if err != nil || json.Unmarshal(b, &got) != nil {
			t.Fatal("legacy read failed")
		}
	}
	for _, mutate := range []func(*events.GateCheckPayload){func(p *events.GateCheckPayload) { p.CheckID = "" }, func(p *events.GateCheckPayload) { p.Verdict = "blue" }, func(p *events.GateCheckPayload) { p.Reason = "" }, func(p *events.GateCheckPayload) { p.Evidence = nil }, func(p *events.GateCheckPayload) { p.Evidence[0].Path = strings.Repeat("x", events.MaxGatePayload) }, func(p *events.GateCheckPayload) { p.Evidence[0].Source = "observed" }, func(p *events.GateCheckPayload) { p.Evidence[0].Stdout = []byte("bad") }} {
		p := gateFact()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("invalid gate accepted")
		}
	}
}
func TestGateRetentionAndDeepCopies(t *testing.T) {
	live := make(chan events.Event)
	c := NewCollectorWithLiveEvents(context.Background(), system.Clock{}, "run", "graph", "version", live)
	for i := 0; i < MaxRetainedProgressItems+1; i++ {
		if _, e := c.Append(events.Event{Type: events.Status, Payload: events.StatusPayload{Code: "working"}}); e != nil {
			t.Fatal(e)
		}
	}
	p := gateFact()
	if _, e := c.Append(events.Event{Type: events.GateCheck, NodeID: "gate", NodeRunID: "node", Payload: p}); e != nil {
		t.Fatal(e)
	}
	p.Evidence[0].Path = "mutated"
	if _, e := c.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{}}); e != nil {
		t.Fatal(e)
	}
	snapshot := c.Snapshot()
	found := false
	for _, e := range snapshot {
		if e.Type == events.GateCheck {
			found = true
			if e.RunID != "run" || e.NodeID != "gate" || e.Payload.(events.GateCheckPayload).Evidence[0].Path != "" {
				t.Fatal("mutable fact")
			}
			e.Payload.(events.GateCheckPayload).Evidence[0].Path = "snapshot mutation"
		}
	}
	if !found || c.Stats().RetainedOmittedEvents == 0 || !c.Stats().LiveStopped {
		t.Fatal("gate retention/progress limits failed")
	}
	for _, e := range c.Snapshot() {
		if e.Type == events.GateCheck && e.Payload.(events.GateCheckPayload).Evidence[0].Path != "" {
			t.Fatal("snapshot aliased")
		}
	}
}

func TestCommandGateFactDetachesBytesAndExitCode(t *testing.T) {
	exit := 1
	p := events.GateCheckPayload{Version: 1, Scope: events.CheckScope{CheckedInputs: []string{"data"}}, CheckID: "diff", Kind: "command", Verdict: "red", Reason: "files differ", Evidence: []events.GateEvidence{{Kind: "command", Source: "command", ExitCode: &exit, Stdout: []byte{0, 255, 'x'}, Stderr: []byte("stderr"), StdoutOmitted: 99}}}
	c := NewCollector(system.Clock{}, "run", "graph", "version")
	if _, e := c.Append(events.Event{Type: events.GateCheck, Payload: p}); e != nil {
		t.Fatal(e)
	}
	p.Evidence[0].Stdout[0] = 'z'
	p.Evidence[0].Stderr[0] = 'z'
	exit = 0
	got := c.Snapshot()[0].Payload.(events.GateCheckPayload)
	if got.Evidence[0].Stdout[0] != 0 || got.Evidence[0].Stderr[0] != 's' || *got.Evidence[0].ExitCode != 1 {
		t.Fatal("retained process evidence aliased")
	}
	b, e := json.Marshal(c.Snapshot()[0])
	var round events.Event
	if e != nil || json.Unmarshal(b, &round) != nil || round.Payload.(events.GateCheckPayload).Evidence[0].Stdout[1] != 255 {
		t.Fatal("byte evidence lost in JSON")
	}
	got.Evidence[0].Stdout[0] = 'q'
	*got.Evidence[0].ExitCode = 0
	if c.Snapshot()[0].Payload.(events.GateCheckPayload).Evidence[0].Stdout[0] != 0 || *c.Snapshot()[0].Payload.(events.GateCheckPayload).Evidence[0].ExitCode != 1 {
		t.Fatal("snapshot evidence aliased")
	}
}

func TestNonCommandGateRejectsProcessOmissionCountersWithoutOverflow(t *testing.T) {
	for _, kind := range []string{"json_predicate", "reported_file_scope"} {
		for _, counts := range [][2]uint64{{^uint64(0), 1}, {1, ^uint64(0)}, {^uint64(0), 0}, {0, ^uint64(0)}} {
			p := gateFact()
			p.Kind = events.CheckKind(kind)
			p.Evidence[0].Source = events.EvidenceSource(kind)
			p.Evidence[0].Kind = events.EvidenceKind(kind)
			if kind == "reported_file_scope" {
				p.Evidence[0].Source = "reported"
			}
			p.Evidence[0].StdoutOmitted = counts[0]
			p.Evidence[0].StderrOmitted = counts[1]
			if p.Validate() == nil {
				t.Errorf("%s accepted forbidden process counters %v", kind, counts)
			}
			// Bypass Event.MarshalJSON validation to exercise the untrusted decoder.
			raw, err := json.Marshal(map[string]any{"event_schema_version": events.SchemaVersion, "type": events.GateCheck, "payload": p})
			if err != nil {
				t.Fatal(err)
			}
			var event events.Event
			if json.Unmarshal(raw, &event) == nil {
				t.Errorf("%s decoder accepted forbidden process counters %v", kind, counts)
			}
		}
	}
}
