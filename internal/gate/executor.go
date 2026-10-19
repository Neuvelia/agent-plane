// Package gate evaluates deterministic, read-only acceptance checks.
package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

type Executor struct{}
type RedError struct{}

func (RedError) GateVerdict() string    { return "red" }
func (RedError) Error() string          { return "gate check returned red" }
func (RedError) DiagnosticCode() string { return "E_GATE_RED" }
func (Executor) ExecuteNode(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
	o, _, e := (Executor{}).ExecuteNodeObserved(ctx, id, n, in, runtime.ExecutionObservation{})
	return o, e
}
func (Executor) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	out, err := execute(ctx, n, in, o)
	if err != nil {
		return nil, nil, fmt.Errorf("gate node %q: %w", id, err)
	}
	return out, nil, nil
}
func execute(ctx context.Context, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := parse(n)
	if err != nil {
		return nil, err
	}
	p := events.GateCheckPayload{CheckID: c.CheckID, Kind: c.Kind, Verdict: "green", Reason: "check matched", Evidence: []events.GateEvidence{{Source: c.Kind}}}
	switch c.Kind {
	case "command":
		p, err = command(ctx, c, in, o)
	case "json_predicate":
		err = predicate(c, in, &p)
	case "reported_file_scope":
		err = scope(c, in, &p)
	}
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
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
	p.Evidence = []events.GateEvidence{{Source: "reported"}}
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
	budget := events.MaxGatePayload - len(base) - 128
	used := 0
	for i, k := range keys {
		item := events.GateEvidence{Source: "reported", Path: k}
		encoded, _ := json.Marshal(item)
		// Reserve envelope and final omission marker space; count encoded escaping.
		if i >= 30 || len(k) > 512 || used+len(encoded)+1 > budget {
			p.Evidence = append(p.Evidence, events.GateEvidence{Source: "reported", Omitted: uint64(len(keys) - i)})
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
