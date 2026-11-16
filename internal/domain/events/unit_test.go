package events

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func envelopeFact() ReturnEnvelopePayload {
	c := gateFact()
	return ReturnEnvelopePayload{Version: 1, UnitID: "u", Verdict: c.Verdict, Reason: c.Reason, Evidence: c.Evidence, IntendedScope: []string{"src/", "./"}, AttemptID: "a", CheckID: c.CheckID, SourceRunID: "r", SourceNodeID: "u", SourceNodeRunID: "n", SourceGraphVersion: "v"}
}
func TestReturnEnvelopeStrictRoundTripAndDetachedCollector(t *testing.T) {
	p := envelopeFact()
	p.Evidence[0].Reference = &EvidenceReference{Kind: "file", State: "known", Identifier: "file", Line: new(int)}
	*p.Evidence[0].Reference.Line = 2
	c := NewCollector("r", "g", "v")
	ev, e := c.Append(Event{Type: ReturnEnvelope, NodeID: "u", NodeRunID: "n", AttemptID: "a", Payload: p})
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(ev)
	if e != nil {
		t.Fatal(e)
	}
	var got Event
	if e = json.Unmarshal(b, &got); e != nil || !reflect.DeepEqual(ev, got) {
		t.Fatal(e, string(b), ev, got)
	}
	p.Evidence[0].Path = "changed"
	*p.Evidence[0].Reference.Line = 9
	p.IntendedScope[0] = "changed"
	snap := c.Snapshot()[0].Payload.(ReturnEnvelopePayload)
	if snap.Evidence[0].Path == "changed" || *snap.Evidence[0].Reference.Line != 2 || snap.IntendedScope[0] != "src/" {
		t.Fatal("retention aliases input")
	}
	snap.Evidence[0].Reference.Identifier = "changed"
	if c.Snapshot()[0].Payload.(ReturnEnvelopePayload).Evidence[0].Reference.Identifier != "file" {
		t.Fatal("snapshot aliases collector")
	}
}
func TestReturnEnvelopeRejectsMalformedFieldsAndSources(t *testing.T) {
	p := envelopeFact()
	b, _ := json.Marshal(p)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	for key := range fields {
		for _, replacement := range []json.RawMessage{nil, json.RawMessage("null")} {
			copy := map[string]json.RawMessage{}
			for k, v := range fields {
				copy[k] = v
			}
			if replacement == nil {
				delete(copy, key)
			} else {
				copy[key] = replacement
			}
			encoded, _ := json.Marshal(copy)
			var got ReturnEnvelopePayload
			if json.Unmarshal(encoded, &got) == nil {
				t.Errorf("accepted missing/null %s", key)
			}
		}
	}
	for _, raw := range []string{strings.Replace(string(b), `"unit_id"`, `"Unit_ID"`, 1), strings.Replace(string(b), `"unit_id":`, `"unit_id":"u","unit_id":`, 1), strings.Replace(string(b), `"return_envelope_version":1`, `"return_envelope_version":2`, 1), strings.Replace(string(b), `"scope_redacted":false`, `"scope_redacted":"false"`, 1), strings.Replace(string(b), `"source_node_id":"u"`, `"source_node_id":"other"`, 1), strings.Replace(string(b), `"verdict":"red"`, `"verdict":"purple"`, 1), strings.Replace(string(b), `"reason":`, `"unexpected":1,"reason":`, 1)} {
		var got ReturnEnvelopePayload
		if json.Unmarshal([]byte(raw), &got) == nil {
			t.Fatal("accepted", raw)
		}
	}
	for _, mutate := range []func(*Event){func(e *Event) { e.RunID = "other" }, func(e *Event) { e.NodeRunID = "other" }, func(e *Event) { e.NodeID = "other" }, func(e *Event) { e.AttemptID = "other" }, func(e *Event) { e.GraphVersion = "other" }, func(e *Event) { e.SchemaVersion = 6 }} {
		e := Event{Type: ReturnEnvelope, RunID: "r", NodeID: "u", NodeRunID: "n", GraphVersion: "v", AttemptID: "a", Payload: p}
		mutate(&e)
		if e.Validate() == nil {
			t.Fatal("mismatched source accepted")
		}
	}
	huge := bytes.Replace(b, []byte("{"), append([]byte("{"), bytes.Repeat([]byte(" "), MaxReturnEnvelope)...), 1)
	var got ReturnEnvelopePayload
	if json.Unmarshal(huge, &got) == nil {
		t.Fatal("oversized accepted")
	}
}
func TestUnitScopeCompleteNormalizedAndBounded(t *testing.T) {
	normalized, e := NormalizeUnitScope([]string{"./src//a/", ".", "a//b"})
	if e != nil || !reflect.DeepEqual(normalized, []string{"src/a/", "./", "a/b"}) {
		t.Fatal(normalized, e)
	}
	for _, scope := range [][]string{{"/a"}, {"C:/a"}, {"a\\b"}, {"a/../b"}, {"../b"}, {"\x00"}, {""}, {"\xff"}, {strings.Repeat("a", 513)}, make([]string, 33)} {
		if _, e := NormalizeUnitScope(scope); e == nil {
			t.Fatal("accepted", scope)
		}
	}
	scope := make([]string, 32)
	for i := range scope {
		scope[i] = strings.Repeat("<", 80)
	}
	if _, e := NormalizeUnitScope(scope); e == nil {
		t.Fatal("encoded scope limit ignored")
	}
	if e := ValidateUnitScope([]string{"[redacted] opaque/../text"}, true); e != nil {
		t.Fatal(e)
	}
	if _, e := NormalizeUnitScope([]string{}); e != nil {
		t.Fatal("empty scope rejected", e)
	}
}
func TestSchemaSevenCheckLookupAndLegacySix(t *testing.T) {
	for _, v := range []int{5, 6, 7} {
		e := Event{SchemaVersion: v, Type: GateCheck, RunID: "r", NodeRunID: "n", AttemptID: "a", Payload: gateFact()}
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var round Event
		if err := json.Unmarshal(b, &round); err != nil {
			t.Fatal(err)
		}
		if len(AttemptCheckResults([]Event{round}, "r", "n", "a")) != 1 {
			t.Fatal("lookup lost version", v)
		}
	}
}

func TestUnitFactsSurviveProgressLimits(t *testing.T) {
	c := NewCollector("r", "g", "v")
	for range 4100 {
		if _, e := c.Append(Event{Type: TextDelta, NodeID: "u", NodeRunID: "n", Payload: TextDeltaPayload{Text: "progress"}}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := c.Append(Event{Type: ReturnEnvelope, NodeID: "u", NodeRunID: "n", AttemptID: "a", Payload: envelopeFact()}); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Append(Event{Type: DeclaredScopeConflicts, Payload: DeclaredScopeConflictsPayload{Details: []DeclaredScopeConflict{{LeftNodeID: "a", RightNodeID: "b", Reason: "unknown_scope"}}}}); e != nil {
		t.Fatal(e)
	}
	envelopes, conflicts := 0, 0
	for _, e := range c.Snapshot() {
		if e.Type == ReturnEnvelope {
			envelopes++
		}
		if e.Type == DeclaredScopeConflicts {
			conflicts++
		}
	}
	if envelopes != 1 || conflicts != 1 || c.Stats().RetainedOmittedEvents == 0 {
		t.Fatal(envelopes, conflicts, c.Stats())
	}
}
