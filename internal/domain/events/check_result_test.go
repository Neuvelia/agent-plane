package events

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCheckResultReferenceRoundTrips(t *testing.T) {
	line := 7
	refs := []*EvidenceReference{nil}
	for _, kind := range []ReferenceKind{"file", "test", "diff", "artifact"} {
		refs = append(refs, &EvidenceReference{Kind: kind, State: "known", Identifier: "resource"}, &EvidenceReference{Kind: kind, State: "unavailable", Reason: "not supplied"}, &EvidenceReference{Kind: kind, State: "invalid", Reason: "bad reference"})
	}
	refs = append(refs, &EvidenceReference{Kind: "file", State: "known", Identifier: "file.go", Line: &line})
	for _, verdict := range []CheckVerdict{CheckGreen, CheckRed} {
		for _, ref := range refs {
			p := gateFact()
			p.Verdict = verdict
			p.Evidence[0].Reference = ref
			b, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			// Unmarshal through a map to shuffle field order and preserve required zero.
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(b, &fields); err != nil {
				t.Fatal(err)
			}
			shuffled, _ := json.Marshal(fields)
			var got CheckResult
			if err := json.Unmarshal(shuffled, &got); err != nil || !reflect.DeepEqual(p, got) || !bytes.Contains(b, []byte(`"duration_ns":0`)) {
				t.Fatalf("roundtrip: %s %+v %v", b, got, err)
			}
		}
	}
	exit := 0
	p := gateFact()
	p.Kind = "command"
	p.Verdict = CheckGreen
	p.Evidence = []GateEvidence{{Kind: "command", Source: "command", ExitCode: &exit, Stdout: []byte{0, 255}}}
	b, _ := json.Marshal(p)
	var got CheckResult
	if err := json.Unmarshal(b, &got); err != nil || *got.Evidence[0].ExitCode != 0 || got.Evidence[0].Stdout[1] != 255 {
		t.Fatalf("zero/bytes: %s %v", b, err)
	}
}

func TestCheckResultRejectsMalformedBeforeTrace(t *testing.T) {
	good, _ := json.Marshal(gateFact())
	cases := []string{}
	for _, key := range []string{"check_result_version", "check_id", "kind", "verdict", "reason", "evidence", "scope", "duration_ns"} {
		var m map[string]json.RawMessage
		json.Unmarshal(good, &m)
		delete(m, key)
		b, _ := json.Marshal(m)
		cases = append(cases, string(b))
		m[key] = json.RawMessage(`null`)
		b, _ = json.Marshal(m)
		cases = append(cases, string(b))
	}
	for _, pair := range [][2]string{{`"check_result_version":1`, `"check_result_version":2`}, {`"duration_ns":0`, `"duration_ns":-1`}, {`"duration_ns":0`, `"duration_ns":0.5`}, {`"duration_ns":0`, `"duration_ns":"0"`}, {`"verdict":"red"`, `"verdict":"blue"`}, {`"reason":"mismatch"`, `"reason":""`}, {`"check_id":"check"`, `"check_id":""`}, {`"checked_inputs":["data"]`, `"checked_inputs":[]`}, {`"checked_inputs":["data"]`, `"checked_inputs":["z","a"]`}, {`"checked_inputs":["data"]`, `"checked_inputs":["a","a"]`}, {`"source":"json_predicate"`, `"source":"json_predicate","unknown":1`}, {`"kind":"json_predicate","source"`, `"kind":null,"source"`}} {
		cases = append(cases, strings.Replace(string(good), pair[0], pair[1], 1))
	}
	for _, ref := range []string{`null`, `{}`, `{"kind":"file","state":"known","identifier":""}`, `{"kind":"file","state":"known","identifier":"x","line":0}`, `{"kind":"test","state":"known","identifier":"x","line":1}`, `{"kind":"file","state":"known","identifier":"x","line":null}`, `{"kind":"file","state":"known","identifier":"x","reason":"contradiction"}`, `{"kind":"file","state":"invalid","identifier":"x","reason":"bad"}`, `{"kind":"file","state":"unavailable"}`, `{"kind":"file","state":"known","identifier":"x","test":"y"}`, `{"kind":"other","state":"known","identifier":"x"}`} {
		cases = append(cases, strings.Replace(string(good), `"source":"json_predicate"`, `"source":"json_predicate","reference":`+ref, 1))
	}
	for i, raw := range cases {
		var p CheckResult
		if err := json.Unmarshal([]byte(raw), &p); err == nil {
			t.Errorf("case %d accepted %s", i, raw)
		}
		var event Event
		envelope := `{"event_schema_version":5,"type":"gate_check","payload":` + raw + `}`
		if err := json.Unmarshal([]byte(envelope), &event); err == nil {
			t.Errorf("case %d entered trace", i)
		}
	}
	for _, mutate := range []func(*CheckResult){
		func(p *CheckResult) { p.DurationNS = -1 }, func(p *CheckResult) { p.Version = 0 }, func(p *CheckResult) { p.Scope.CheckedInputs[0] = "\xff" }, func(p *CheckResult) {
			p.Evidence[0].Reference = &EvidenceReference{Kind: "file", State: "known", Identifier: "\xff"}
		}, func(p *CheckResult) { p.Evidence = make([]GateEvidence, 33) },
		func(p *CheckResult) { p.Evidence[0].Path = strings.Repeat("<", MaxGatePayload/6) },
	} {
		p := gateFact()
		mutate(&p)
		c := NewCollector("r", "g", "v")
		if _, err := c.Append(Event{Type: GateCheck, Payload: p}); err == nil || len(c.Snapshot()) != 0 {
			t.Fatal("invalid result retained")
		}
	}
}

func TestFrozenLegacyGateAndVersionShapes(t *testing.T) {
	frozen := `{"event_schema_version":4,"type":"gate_check","payload":{"check_id":"old","kind":"json_predicate","verdict":"red","reason":"mismatch","evidence":[{"source":"json_predicate"}]}}`
	var e Event
	if err := json.Unmarshal([]byte(frozen), &e); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Payload.(LegacyGateCheckPayload); !ok {
		t.Fatal("legacy upgraded")
	}
	b, err := json.Marshal(e)
	if err != nil || bytes.Contains(b, []byte("duration_ns")) || bytes.Contains(b, []byte("checked_inputs")) {
		t.Fatal("fabricated legacy metadata")
	}
	if len(CheckResults([]Event{e}, "r", "n")) != 0 {
		t.Fatal("legacy lookup upgraded")
	}
	full, _ := json.Marshal(Event{SchemaVersion: 5, Type: GateCheck, Payload: gateFact()})
	for _, raw := range []string{strings.Replace(frozen, `"event_schema_version":4`, `"event_schema_version":5`, 1), strings.Replace(string(full), `"event_schema_version":5`, `"event_schema_version":4`, 1), strings.Replace(string(full), `"event_schema_version":5`, `"event_schema_version":11`, 1)} {
		if json.Unmarshal([]byte(raw), &e) == nil {
			t.Fatal("incompatible shape accepted")
		}
	}
	for version := 1; version <= 4; version++ {
		raw := strings.Replace(`{"event_schema_version":V,"type":"run_started","payload":{"node_count":0}}`, "V", string(rune('0'+version)), 1)
		if json.Unmarshal([]byte(raw), &e) != nil {
			t.Fatal("legacy lifecycle rejected")
		}
	}
}

func TestCheckResultsFilterOrderAndDetach(t *testing.T) {
	line := 2
	p := gateFact()
	p.Evidence[0].Reference = &EvidenceReference{Kind: "file", State: "known", Identifier: "x", Line: &line}
	trace := []Event{{SchemaVersion: 5, RunID: "r", NodeRunID: "n", Type: GateCheck, Sequence: 9, Payload: p}, {SchemaVersion: 5, RunID: "other", NodeRunID: "n", Type: GateCheck, Sequence: 2, Payload: p}, {SchemaVersion: 5, RunID: "r", NodeRunID: "other", Type: GateCheck, Sequence: 3, Payload: p}, {SchemaVersion: 5, RunID: "r", NodeRunID: "n", Type: GateCheck, Sequence: 1, Payload: func() CheckResult { q := CloneGate(p); q.CheckID = "first"; return q }()}}
	got := CheckResults(trace, "r", "n")
	if len(got) != 2 || got[0].CheckID != "first" || got[1].CheckID != "check" {
		t.Fatal("lookup filter/order")
	}
	got[1].Scope.CheckedInputs[0] = "changed"
	got[1].Evidence[0].Reference.Identifier = "changed"
	*got[1].Evidence[0].Reference.Line = 3
	if p.Scope.CheckedInputs[0] != "data" || p.Evidence[0].Reference.Identifier != "x" || line != 2 {
		t.Fatal("lookup aliased")
	}
	if len(CheckResults(trace, "", "n")) != 0 || len(CheckResults(trace, "r", "missing")) != 0 {
		t.Fatal("absent identity lookup")
	}
}

func TestGreenMayOmitEvidenceAndRedCannot(t *testing.T) {
	p := gateFact()
	p.Verdict = CheckGreen
	p.Evidence = []GateEvidence{}
	b, _ := json.Marshal(p)
	var got CheckResult
	c := NewCollector("r", "g", "v")
	if _, err := c.Append(Event{Type: GateCheck, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(c.Snapshot()[0]); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil || got.Evidence == nil || len(got.Evidence) != 0 {
		t.Fatalf("green empty evidence: %s %v", b, err)
	}
	p.Verdict = CheckRed
	if p.Validate() == nil {
		t.Fatal("red empty evidence accepted")
	}
}

func TestFullResultRejectsCaseAliasesAndForbiddenReferenceFields(t *testing.T) {
	good, err := json.Marshal(gateFact())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{}
	var resultFields map[string]json.RawMessage
	if err := json.Unmarshal(good, &resultFields); err != nil {
		t.Fatal(err)
	}
	// Aliases are unknown fields even when a correctly cased field is present.
	for _, key := range []string{"check_result_version", "check_id", "kind", "verdict", "reason", "evidence", "scope", "duration_ns"} {
		cases["result/"+key] = strings.TrimSuffix(string(good), "}") + `,"` + strings.ToUpper(key) + `":null}`
		cases["result valid alias/"+key] = strings.TrimSuffix(string(good), "}") + `,"` + strings.ToUpper(key) + `":` + string(resultFields[key]) + `}`
	}
	cases["scope/alias"] = strings.Replace(string(good), `"checked_inputs":["data"]`, `"checked_inputs":["data"],"CHECKED_INPUTS":null`, 1)
	for _, key := range []string{"kind", "source", "reference", "exit_code", "path", "stdout", "stderr", "stdout_omitted", "stderr_omitted", "omitted"} {
		cases["evidence/"+key] = strings.Replace(string(good), `"source":"json_predicate"`, `"source":"json_predicate","`+strings.ToUpper(key)+`":null`, 1)
	}
	cases["scope/valid alias"] = strings.Replace(string(good), `"checked_inputs":["data"]`, `"checked_inputs":["data"],"CHECKED_INPUTS":["data"]`, 1)
	for key, value := range map[string]string{"KIND": `"json_predicate"`, "SOURCE": `"json_predicate"`, "REFERENCE": `{"kind":"file","state":"known","identifier":"file.go"}`, "PATH": `""`, "OMITTED": `0`} {
		cases["evidence valid alias/"+key] = strings.Replace(string(good), `"source":"json_predicate"`, `"source":"json_predicate","`+key+`":`+value, 1)
	}
	known := `{"kind":"file","state":"known","identifier":"file.go"}`
	for key, value := range map[string]string{"KIND": `"file"`, "STATE": `"known"`, "IDENTIFIER": `"file.go"`, "LINE": `1`, "REASON": `""`} {
		ref := strings.TrimSuffix(known, "}") + `,"` + key + `":` + value + `}`
		cases["reference valid alias/"+key] = strings.Replace(string(good), `"source":"json_predicate"`, `"source":"json_predicate","reference":`+ref, 1)
	}
	for _, key := range []string{"kind", "state", "identifier", "line", "reason"} {
		ref := strings.TrimSuffix(known, "}") + `,"` + strings.ToUpper(key) + `":null}`
		cases["reference/"+key] = strings.Replace(string(good), `"source":"json_predicate"`, `"source":"json_predicate","reference":`+ref, 1)
	}
	for name, ref := range map[string]string{
		"known empty reason":           `{"kind":"file","state":"known","identifier":"file.go","reason":""}`,
		"unavailable empty identifier": `{"kind":"file","state":"unavailable","reason":"missing","identifier":""}`,
		"invalid empty identifier":     `{"kind":"file","state":"invalid","reason":"bad","identifier":""}`,
		"unavailable null line":        `{"kind":"file","state":"unavailable","reason":"missing","line":null}`,
		"known null identifier":        `{"kind":"file","state":"known","identifier":null}`,
	} {
		cases[name] = strings.Replace(string(good), `"source":"json_predicate"`, `"source":"json_predicate","reference":`+ref, 1)
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var result CheckResult
			if err := json.Unmarshal([]byte(raw), &result); err == nil {
				t.Errorf("direct decode accepted forbidden field: %s", raw)
			}
			var event Event
			envelope := `{"event_schema_version":5,"type":"gate_check","payload":` + raw + `}`
			if err := json.Unmarshal([]byte(envelope), &event); err == nil {
				t.Errorf("event decode accepted forbidden field: %s", raw)
			}
		})
	}
	// Freeze the legacy decoder's existing case-insensitive matching behavior.
	legacy := `{"event_schema_version":4,"type":"gate_check","payload":{"check_id":"old","kind":"json_predicate","verdict":"red","reason":"mismatch","evidence":[{"SOURCE":"json_predicate"}]}}`
	var event Event
	if err := json.Unmarshal([]byte(legacy), &event); err != nil {
		t.Fatalf("legacy decoding changed: %v", err)
	}
}
