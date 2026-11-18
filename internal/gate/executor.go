// Package gate evaluates deterministic, read-only acceptance checks.
package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/proctree"
	"agentplane/internal/runtime"
)

// Executor evaluates Gate nodes. The zero value is valid.
type Executor struct {
	// EvidenceBound limits the whole command-evidence step; zero selects
	// commandEvidenceBound. It is independent of the node limit.
	EvidenceBound time.Duration
	// Termination tunes how an owned process tree is stopped on cancellation.
	Termination proctree.Policy
	// diffPath replaces the fixed diff binary in tests only; it never comes
	// from graph configuration.
	diffPath string
}

type RedError struct{}

func (RedError) GateVerdict() string    { return "red" }
func (RedError) Error() string          { return "gate check returned red" }
func (RedError) DiagnosticCode() string { return "E_GATE_RED" }

// ExecuteNode evaluates the Gate without observing its Check Result.
func (e Executor) ExecuteNode(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
	o, _, err := e.ExecuteNodeObserved(ctx, id, n, in, runtime.ExecutionObservation{})
	return o, err
}

// ExecuteNodeObserved evaluates the Gate and reports its Check Result to o.Check.
func (e Executor) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	out, err := e.execute(ctx, n, in, o)
	if err != nil {
		return nil, nil, fmt.Errorf("gate node %q: %w", id, err)
	}
	return out, nil, nil
}
func (e Executor) execute(ctx context.Context, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := parse(n)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	scopeInputs := checkedInputs(c)
	p := events.GateCheckPayload{Version: 1, Scope: events.CheckScope{CheckedInputs: scopeInputs}, CheckID: c.CheckID, Kind: events.CheckKind(c.Kind), Verdict: "green", Reason: "check matched", Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(c.Kind), Source: events.EvidenceSource(c.Kind)}}}
	p, err = e.evaluate(ctx, c, in, o, p)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	p.Version = 1
	p.Scope = events.CheckScope{CheckedInputs: scopeInputs}
	p.DurationNS = time.Since(started).Nanoseconds()
	for i := range p.Evidence {
		p.Evidence[i].Kind = events.EvidenceKind(c.Kind)
	}
	if err = p.Validate(); err != nil {
		return nil, err
	}
	if o.Check != nil {
		o.Check(p)
	}
	if p.Verdict == "red" {
		return nil, RedError{}
	}
	out := map[string]graph.Value{}
	if _, ok := n.Outputs["verdict"]; ok {
		out["verdict"] = graph.NewStringValue("green")
	}
	return out, nil
}
func checkedInputs(c config) []string {
	if c.Kind != KindCommand {
		return []string{c.Input}
	}
	inputs := []string{c.Left}
	if c.Right != c.Left {
		inputs = append(inputs, c.Right)
	}
	sort.Strings(inputs)
	return inputs
}

// evaluate runs the check for c.Kind; p is the default result for kinds that
// fill it in place.
func (e Executor) evaluate(ctx context.Context, c config, in map[string]graph.Value, o runtime.ExecutionObservation, p events.GateCheckPayload) (events.GateCheckPayload, error) {
	var err error
	switch c.Kind {
	case KindCommand:
		p, err = e.command(ctx, c, in, o)
	case KindJSONPredicate:
		err = predicate(c, in, &p)
	case KindReportedFileScope:
		err = scope(c, in, &p)
	case KindChangedFilesScope:
		p, err = changedFilesCheck(ctx, c, in, o)
	}
	return p, err
}

func changedFilesCheck(ctx context.Context, c config, in map[string]graph.Value, o runtime.ExecutionObservation) (events.GateCheckPayload, error) {
	var p events.GateCheckPayload
	if _, ok := in[c.Input]; !ok {
		return p, fmt.Errorf("changed-files scope requires candidate input %q", c.Input)
	}
	if o.ScopeBaselineError != nil && c.Mode == "warn" {
		p = indeterminateChangedFiles(c.Allowed, c.CheckID, "")
		p.Scope.CheckedInputs = []string{c.Input}
		return p, nil
	}
	if o.ScopeBaseline == nil {
		return p, errors.New("changed-files scope baseline unavailable")
	}
	root, e := filepath.EvalSymlinks(c.Workspace)
	if e != nil || root != o.ScopeBaseline.Root {
		return p, errors.New("changed-files scope workspace differs from baseline")
	}
	p, err := CheckChangedFiles(ctx, *o.ScopeBaseline, c.Allowed, c.Mode, c.CheckID)
	p.Scope.CheckedInputs = []string{c.Input}
	return p, err
}
func decodeInput(c config, in map[string]graph.Value) (any, error) {
	b, ok := in[c.Input].AsJSON()
	if !ok {
		return nil, fmt.Errorf("input %q must resolve to JSON", c.Input)
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	err := d.Decode(&v)
	return v, err
}
func predicate(c config, in map[string]graph.Value, p *events.GateCheckPayload) error {
	v, err := decodeInput(c, in)
	if err != nil {
		return err
	}
	if _, ok := v.(map[string]any); !ok {
		return errors.New("predicate input must be an object")
	}
	for _, key := range c.Field {
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("predicate field traversal requires objects")
		}
		v, ok = m[key]
		if !ok {
			p.Verdict = "red"
			p.Reason = "predicate field missing"
			return nil
		}
	}
	switch v.(type) {
	case nil, string, bool, json.Number:
	default:
		return errors.New("predicate field must be scalar/null")
	}
	var expected any
	d := json.NewDecoder(bytes.NewReader(c.Expected))
	d.UseNumber()
	_ = d.Decode(&expected)
	equal := false
	switch a := v.(type) {
	case json.Number:
		if b, ok := expected.(json.Number); ok {
			equal = numberKey(string(a)) == numberKey(string(b))
		}
	case nil:
		equal = expected == nil
	case string:
		b, ok := expected.(string)
		equal = ok && a == b
	case bool:
		b, ok := expected.(bool)
		equal = ok && a == b
	}
	if (c.Operator == "eq") != equal {
		p.Verdict = "red"
		p.Reason = "predicate mismatch"
	}
	return nil
}
func scope(c config, in map[string]graph.Value, p *events.GateCheckPayload) error {
	v, err := decodeInput(c, in)
	if err != nil {
		return err
	}
	a, ok := v.([]any)
	if !ok {
		return errors.New("reported files input must be an array")
	}
	bad := map[string]bool{}
	for _, v := range a {
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("reported file must be a path/provenance record")
		}
		raw, ok := m["path"].(string)
		if !ok {
			return errors.New("reported file path must be a string")
		}
		if provenance, present := m["provenance"]; present {
			if _, ok := provenance.(string); !ok {
				return errors.New("reported provenance must be a string")
			}
		}
		for k := range m {
			if k != "path" && k != "provenance" {
				return errors.New("unknown reported file field")
			}
		}
		q, err := normalize(raw)
		if err != nil {
			return err
		}
		allowed := false
		for _, a := range c.Allowed {
			if q == a || strings.HasSuffix(a, "/") && strings.HasPrefix(q, a) {
				allowed = true
				break
			}
		}
		if !allowed {
			bad[q] = true
		}
	}
	p.Evidence = []events.GateEvidence{{Kind: "reported_file_scope", Source: "reported"}}
	if len(bad) == 0 {
		return nil
	}
	p.Verdict = "red"
	p.Reason = "reported paths outside allowed scope"
	keys := make([]string, 0, len(bad))
	for k := range bad {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	p.Evidence = nil
	base, _ := json.Marshal(p)
	budget := events.MaxGatePayload - len(base) - 256
	used := 0
	for i, k := range keys {
		item := events.GateEvidence{Kind: "reported_file_scope", Source: "reported", Path: k}
		encoded, _ := json.Marshal(item)
		// Reserve envelope and final omission marker space; count encoded escaping.
		if i >= 30 || len(k) > 512 || used+len(encoded)+1 > budget {
			p.Evidence = append(p.Evidence, events.GateEvidence{Kind: "reported_file_scope", Source: "reported", Omitted: uint64(len(keys) - i)})
			break
		}
		p.Evidence = append(p.Evidence, item)
		used += len(encoded) + 1
	}
	return nil
}

// numberKey compares decimal values without float rounding or expanding exponents.
func numberKey(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign = "-"
		s = s[1:]
	}
	mantissa, exponent, has := strings.Cut(strings.ToLower(s), "e")
	scale := new(big.Int)
	if has {
		scale.SetString(exponent, 10)
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return "0"
	}
	scale.Sub(scale, big.NewInt(int64(len(fraction))))
	trimmed := strings.TrimRight(digits, "0")
	scale.Add(scale, big.NewInt(int64(len(digits)-len(trimmed))))
	return sign + trimmed + "e" + scale.String()
}
