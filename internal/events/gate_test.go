package events

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func gateFact() GateCheckPayload {
	return GateCheckPayload{Version: 1, Scope: CheckScope{CheckedInputs: []string{"data"}}, CheckID: "check", Kind: "json_predicate", Verdict: "red", Reason: "mismatch", Evidence: []GateEvidence{{Kind: "json_predicate", Source: "json_predicate"}}}
}
func TestGateSchemaCompatibilityAndValidation(t *testing.T) {
	for version := 1; version <= 5; version++ {
		e := Event{SchemaVersion: version, Type: GateCheck, Payload: gateFact()}
		b, err := json.Marshal(e)
		if version < 5 {
			if err == nil {
				t.Fatal("old schema accepted gate")
			}
			continue
		}
		var got Event
		if err != nil || json.Unmarshal(b, &got) != nil || got.Payload.(GateCheckPayload).CheckID != "check" {
			t.Fatal("gate roundtrip failed")
		}
	}
	for version := 1; version <= 5; version++ {
		e := Event{SchemaVersion: version, Type: RunStarted, Payload: RunStartedPayload{}}
		b, err := json.Marshal(e)
		var got Event
		if err != nil || json.Unmarshal(b, &got) != nil {
			t.Fatal("legacy read failed")
		}
	}
	for _, mutate := range []func(*GateCheckPayload){func(p *GateCheckPayload) { p.CheckID = "" }, func(p *GateCheckPayload) { p.Verdict = "blue" }, func(p *GateCheckPayload) { p.Reason = "" }, func(p *GateCheckPayload) { p.Evidence = nil }, func(p *GateCheckPayload) { p.Evidence[0].Path = strings.Repeat("x", MaxGatePayload) }, func(p *GateCheckPayload) { p.Evidence[0].Source = "observed" }, func(p *GateCheckPayload) { p.Evidence[0].Stdout = []byte("bad") }} {
		p := gateFact()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("invalid gate accepted")
		}
	}
}
func TestGateRetentionAndDeepCopies(t *testing.T) {
	live := make(chan Event)
	c := NewCollectorWithLiveEvents(context.Background(), "run", "graph", "version", live)
	for i := 0; i < MaxRetainedProgressItems+1; i++ {
		if _, e := c.Append(Event{Type: Status, Payload: StatusPayload{Code: "working"}}); e != nil {
			t.Fatal(e)
		}
	}
	p := gateFact()
	if _, e := c.Append(Event{Type: GateCheck, NodeID: "gate", NodeRunID: "node", Payload: p}); e != nil {
		t.Fatal(e)
	}
	p.Evidence[0].Path = "mutated"
	if _, e := c.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{}}); e != nil {
		t.Fatal(e)
	}
	snapshot := c.Snapshot()
	found := false
	for _, e := range snapshot {
		if e.Type == GateCheck {
			found = true
			if e.RunID != "run" || e.NodeID != "gate" || e.Payload.(GateCheckPayload).Evidence[0].Path != "" {
				t.Fatal("mutable fact")
			}
			e.Payload.(GateCheckPayload).Evidence[0].Path = "snapshot mutation"
		}
	}
	if !found || c.Stats().RetainedOmittedEvents == 0 || !c.Stats().LiveStopped {
		t.Fatal("gate retention/progress limits failed")
	}
	for _, e := range c.Snapshot() {
		if e.Type == GateCheck && e.Payload.(GateCheckPayload).Evidence[0].Path != "" {
			t.Fatal("snapshot aliased")
		}
	}
}

func TestCommandGateFactDetachesBytesAndExitCode(t *testing.T) {
	exit := 1
	p := GateCheckPayload{Version: 1, Scope: CheckScope{CheckedInputs: []string{"data"}}, CheckID: "diff", Kind: "command", Verdict: "red", Reason: "files differ", Evidence: []GateEvidence{{Kind: "command", Source: "command", ExitCode: &exit, Stdout: []byte{0, 255, 'x'}, Stderr: []byte("stderr"), StdoutOmitted: 99}}}
	c := NewCollector("run", "graph", "version")
	if _, e := c.Append(Event{Type: GateCheck, Payload: p}); e != nil {
		t.Fatal(e)
	}
	p.Evidence[0].Stdout[0] = 'z'
	p.Evidence[0].Stderr[0] = 'z'
	exit = 0
	got := c.Snapshot()[0].Payload.(GateCheckPayload)
	if got.Evidence[0].Stdout[0] != 0 || got.Evidence[0].Stderr[0] != 's' || *got.Evidence[0].ExitCode != 1 {
		t.Fatal("retained process evidence aliased")
	}
	b, e := json.Marshal(c.Snapshot()[0])
	var round Event
	if e != nil || json.Unmarshal(b, &round) != nil || round.Payload.(GateCheckPayload).Evidence[0].Stdout[1] != 255 {
		t.Fatal("byte evidence lost in JSON")
	}
	got.Evidence[0].Stdout[0] = 'q'
	*got.Evidence[0].ExitCode = 0
	if c.Snapshot()[0].Payload.(GateCheckPayload).Evidence[0].Stdout[0] != 0 || *c.Snapshot()[0].Payload.(GateCheckPayload).Evidence[0].ExitCode != 1 {
		t.Fatal("snapshot evidence aliased")
	}
}

func TestNonCommandGateRejectsProcessOmissionCountersWithoutOverflow(t *testing.T) {
	for _, kind := range []string{"json_predicate", "reported_file_scope"} {
		for _, counts := range [][2]uint64{{^uint64(0), 1}, {1, ^uint64(0)}, {^uint64(0), 0}, {0, ^uint64(0)}} {
			p := gateFact()
			p.Kind = CheckKind(kind)
			p.Evidence[0].Source = EvidenceSource(kind)
			p.Evidence[0].Kind = EvidenceKind(kind)
			if kind == "reported_file_scope" {
				p.Evidence[0].Source = "reported"
			}
			p.Evidence[0].StdoutOmitted = counts[0]
			p.Evidence[0].StderrOmitted = counts[1]
			if p.Validate() == nil {
				t.Errorf("%s accepted forbidden process counters %v", kind, counts)
			}
			// Bypass Event.MarshalJSON validation to exercise the untrusted decoder.
			raw, err := json.Marshal(map[string]any{"event_schema_version": SchemaVersion, "type": GateCheck, "payload": p})
			if err != nil {
				t.Fatal(err)
			}
			var event Event
			if json.Unmarshal(raw, &event) == nil {
				t.Errorf("%s decoder accepted forbidden process counters %v", kind, counts)
			}
		}
	}
}
