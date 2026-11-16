package runtime

import (
	"reflect"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

func registryCheck(verdict events.CheckVerdict) events.CheckResult {
	return events.CheckResult{Version: 1, CheckID: "check", Kind: "json_predicate", Verdict: verdict, Reason: "predicate", Scope: events.CheckScope{CheckedInputs: []string{"candidate"}}, Evidence: []events.GateEvidence{{Kind: "json_predicate", Source: "json_predicate", Reference: &events.EvidenceReference{Kind: "file", State: "known", Identifier: "test"}}}}
}
func newRegistry(t *testing.T) unitRegistry {
	t.Helper()
	r := unitRegistry{}
	if e := r.register("u", "r", "n", "v", []string{"src/"}, false); e != nil {
		t.Fatal(e)
	}
	if e := r.begin("u", "a"); e != nil {
		t.Fatal(e)
	}
	return r
}
func TestUnitRegistryFailsClosedAndAcceptsOnce(t *testing.T) {
	for _, mutate := range []func(*events.ReturnEnvelopePayload){
		func(p *events.ReturnEnvelopePayload) { p.UnitID = "" }, func(p *events.ReturnEnvelopePayload) { p.UnitID = "unknown"; p.SourceNodeID = "unknown" },
		func(p *events.ReturnEnvelopePayload) { p.AttemptID = "unknown" }, func(p *events.ReturnEnvelopePayload) { p.SourceRunID = "other" }, func(p *events.ReturnEnvelopePayload) { p.SourceNodeRunID = "other" }, func(p *events.ReturnEnvelopePayload) { p.SourceGraphVersion = "other" }, func(p *events.ReturnEnvelopePayload) { p.SourceNodeID = "other" }, func(p *events.ReturnEnvelopePayload) { p.CheckID = "other" }, func(p *events.ReturnEnvelopePayload) { p.Reason = "other" }, func(p *events.ReturnEnvelopePayload) { p.Verdict = "red" }, func(p *events.ReturnEnvelopePayload) { p.IntendedScope = []string{"other/"} }, func(p *events.ReturnEnvelopePayload) { p.ScopeRedacted = true }, func(p *events.ReturnEnvelopePayload) { p.Evidence[0].Reference.Identifier = "other" },
	} {
		r := newRegistry(t)
		check := registryCheck("green")
		p, e := r.envelope("u", "a", check)
		if e != nil {
			t.Fatal(e)
		}
		mutate(&p)
		if r.publish(p, check, map[string]graph.Value{"value": graph.NewStringValue("wrong")}) == nil {
			t.Fatal("invalid envelope accepted", p)
		}
		if _, _, e := r.acceptedValues("u"); e == nil {
			t.Fatal("invalid publication leaked")
		}
	}
	r := newRegistry(t)
	check := registryCheck("green")
	p, e := r.envelope("u", "a", check)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := graph.NewJSONValue([]byte(`{"value":1}`))
	candidate := map[string]graph.Value{"data": data}
	if e := r.publish(p, check, candidate); e != nil {
		t.Fatal(e)
	}
	candidate["data"] = graph.NewStringValue("mutated")
	p.IntendedScope[0] = "mutated"
	p.Evidence[0].Reference.Identifier = "mutated"
	values, ports, e := r.acceptedValues("u")
	if e != nil {
		t.Fatal(e)
	}
	original := ports[0]
	values["data"] = graph.NewStringValue("mutated")
	ports[0].Provenance.Port = "mutated"
	again, ports2, _ := r.acceptedValues("u")
	b, _ := again["data"].AsJSON()
	if string(b) != `{"value":1}` || ports2[0].Provenance.Port != "data" || original.Provenance.AttemptID != "a" {
		t.Fatal(again, ports2)
	}
	for _, verdict := range []events.CheckVerdict{"green", "red"} {
		check := registryCheck(verdict)
		p, _ := r.envelope("u", "a", check)
		if r.publish(p, check, nil) == nil {
			t.Fatal("second publication accepted")
		}
	}
	if r.begin("u", "b") == nil || r.register("u", "r", "n", "v", []string{}, false) == nil {
		t.Fatal("accepted unit reopened")
	}
	if r.begin("unknown", "a") == nil || r.register("", "r", "n", "v", []string{}, false) == nil {
		t.Fatal("unknown/missing registration accepted")
	}
}
func TestUnitRegistryRedRetryPreservesExactSanitizedScope(t *testing.T) {
	r := unitRegistry{}
	scope := []string{"[redacted] /../"}
	if e := r.register("u", "r", "n", "v", scope, true); e != nil {
		t.Fatal(e)
	}
	scope[0] = "mutated"
	_ = r.begin("u", "a")
	check := registryCheck("red")
	p, _ := r.envelope("u", "a", check)
	if !reflect.DeepEqual(p.IntendedScope, []string{"[redacted] /../"}) || !p.ScopeRedacted {
		t.Fatal(p)
	}
	if e := r.publish(p, check, map[string]graph.Value{"candidate": graph.NewStringValue("red")}); e != nil {
		t.Fatal(e)
	}
	if _, _, e := r.acceptedValues("u"); e == nil {
		t.Fatal("red leaked")
	}
	if r.publish(p, check, nil) == nil {
		t.Fatal("duplicate red accepted")
	}
	if e := r.begin("u", "b"); e != nil {
		t.Fatal(e)
	}
	if r.publish(p, check, nil) == nil {
		t.Fatal("old attempt accepted")
	}
	p, _ = r.envelope("u", "b", check)
	if e := r.publish(p, check, nil); e != nil {
		t.Fatal(e)
	}
	if r.begin("u", "a") == nil {
		t.Fatal("attempt identity reused")
	}
}
