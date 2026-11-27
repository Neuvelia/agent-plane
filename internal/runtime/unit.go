package runtime

import (
	"agentplane/internal/domain/run"
	"errors"
	"fmt"
	"reflect"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

type unitRecord struct {
	run       events.RunID
	node      events.NodeRunID
	version   string
	scope     []string
	redacted  bool
	current   events.AttemptID
	published bool
	seen      map[events.AttemptID]bool
	accepted  events.AttemptID
	values    map[string]graph.Value
}
type unitRegistry map[string]*unitRecord

func (r unitRegistry) register(id string, run events.RunID, node events.NodeRunID, version string, scope []string, redacted bool) error {
	if id == "" || r[id] != nil || run == "" || node == "" || version == "" {
		return errors.New("unit registration requires unique unit and source identities")
	}
	if err := events.ValidateUnitScope(scope, redacted); err != nil {
		return err
	}
	r[id] = &unitRecord{run: run, node: node, version: version, scope: append([]string{}, scope...), redacted: redacted, seen: map[events.AttemptID]bool{}}
	return nil
}
func (r unitRegistry) begin(id string, aid events.AttemptID) error {
	u := r[id]
	if u == nil || aid == "" || u.accepted != "" || (u.current != "" && !u.published) || u.seen[aid] {
		return fmt.Errorf("unit %q cannot start unknown, unfinished or accepted attempt", id)
	}
	u.current = aid
	u.seen[aid] = true
	u.published = false
	return nil
}

// closeUnchecked ends the current attempt of unit id without a Return Envelope.
// Only an attempt that produced no Check Result (a node deadline, F3.4) ends
// this way: there is no verdict to publish, nothing is accepted, and the next
// attempt may begin (ADR-0044).
func (r unitRegistry) closeUnchecked(id string, aid events.AttemptID) error {
	u := r[id]
	if u == nil || aid == "" || u.current != aid || u.published || u.accepted != "" {
		return fmt.Errorf("unit %q can close only its current unpublished attempt", id)
	}
	u.published = true
	return nil
}

func (r unitRegistry) envelope(id string, aid events.AttemptID, check events.CheckResult) (events.ReturnEnvelopePayload, error) {
	u := r[id]
	if u == nil {
		return events.ReturnEnvelopePayload{}, fmt.Errorf("unknown unit %q", id)
	}
	if err := check.Validate(); err != nil {
		return events.ReturnEnvelopePayload{}, err
	}
	c := events.CloneGate(check)
	p := events.ReturnEnvelopePayload{Version: 1, UnitID: id, Verdict: c.Verdict, Reason: c.Reason, Evidence: c.Evidence, IntendedScope: append([]string{}, u.scope...), ScopeRedacted: u.redacted, AttemptID: aid, CheckID: c.CheckID, SourceRunID: u.run, SourceNodeID: id, SourceNodeRunID: u.node, SourceGraphVersion: u.version}
	return p, p.Validate()
}
func (r unitRegistry) publish(p events.ReturnEnvelopePayload, check events.CheckResult, values map[string]graph.Value) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid return envelope: %w", err)
	}
	u := r[p.UnitID]
	if u == nil || u.current == "" || p.AttemptID != u.current || u.published || u.accepted != "" {
		return fmt.Errorf("unit %q publication must identify its current unpublished attempt; accepted values cannot be overwritten", p.UnitID)
	}
	expected, err := r.envelope(p.UnitID, u.current, check)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(p, expected) {
		return fmt.Errorf("unit %q envelope contradicts retained check, intended scope or source execution", p.UnitID)
	}
	u.published = true
	if p.Verdict == events.CheckGreen {
		u.accepted = u.current
		u.values = copyValues(values)
	}
	return nil
}
func (r unitRegistry) acceptedValues(id string) (map[string]graph.Value, []run.PortValue, error) {
	u := r[id]
	if u == nil || u.accepted == "" {
		return nil, nil, fmt.Errorf("unit %q has no accepted attempt", id)
	}
	values := copyValues(u.values)
	ports := sortedPortValues(values)
	for i := range ports {
		ports[i].Provenance = &run.AcceptedProvenance{RunID: u.run, NodeRunID: u.node, AttemptID: u.accepted, Port: ports[i].Name}
	}
	return values, ports, nil
}
func unitScopes(d *graph.Definition, r *logRedactor) (map[string][]string, map[string]bool, error) {
	scopes := map[string][]string{}
	redacted := map[string]bool{}
	for _, id := range d.Units {
		scope, err := events.NormalizeUnitScope(originalScope(d.Nodes[id]))
		if err != nil {
			return nil, nil, unitScopeError(d, id, err)
		}
		for i, s := range scope {
			c := r.clone()
			b := append([]byte{}, c.Write(events.Stdout, []byte(s))...)
			b = append(b, c.Flush(events.Stdout)...)
			scope[i] = string(b)
			redacted[id] = redacted[id] || scope[i] != s
		}
		if err := events.ValidateUnitScope(scope, redacted[id]); err != nil {
			return nil, nil, unitScopeError(d, id, err)
		}
		scopes[id] = scope
	}
	return scopes, redacted, nil
}

func unitScopeError(d *graph.Definition, id string, cause error) error {
	return &run.PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{Severity: graph.SeverityError, Code: "E_UNIT_SCOPE", GraphID: d.ID, Location: graph.DiagnosticLocation{NodeID: id, Path: "nodes." + id + ".config.allowed_paths"}, Message: fmt.Sprintf("unit %q intended scope is invalid: %v", id, cause), Suggestion: "use complete portable relative paths within the enrolled-unit scope bounds, including after configured redaction"}}}}
}
