package fieldstest

import (
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
)

// PortSpec is a validator of declared outputs and the ports it allows.
type PortSpec struct {
	Ports []fields.Port
	// Base is a valid node with no outputs, or with exactly the required ones.
	Base graph.NodeDefinition
	// Satisfy sets config so a port's Requires hold.
	Satisfy func(n *graph.NodeDefinition, r fields.Rule)
	// TypeChecked is true when the validator refuses a port of another type.
	TypeChecked bool
	Validate    func(graph.NodeDefinition) error
}

// RunPorts checks that each allowed port is accepted (with its conditions
// met), that a port whose conditions are unmet is refused, that a port of
// another type is refused when the validator checks types, and that any
// other output name is refused.
func RunPorts(t *testing.T, s PortSpec) {
	t.Helper()
	for _, p := range s.Ports {
		n := Clone(s.Base)
		for _, r := range p.Requires {
			s.Satisfy(&n, r)
		}
		n.Outputs[p.Name] = graph.PortDefinition{Type: graph.ValueType(p.Type), Required: p.Required}
		if err := s.Validate(n); err != nil {
			t.Errorf("output %s: allowed port is refused: %v", p.Name, err)
		}
		if len(p.Requires) > 0 {
			unmet := Clone(s.Base)
			unmet.Outputs[p.Name] = graph.PortDefinition{Type: graph.ValueType(p.Type)}
			if s.Validate(unmet) == nil {
				t.Errorf("output %s: accepted although %+v does not hold", p.Name, p.Requires)
			}
		}
		if s.TypeChecked {
			if wrong, ok := otherType([]string{p.Type}); ok {
				typed := Clone(n)
				typed.Outputs[p.Name] = graph.PortDefinition{Type: wrong, Required: p.Required}
				if s.Validate(typed) == nil {
					t.Errorf("output %s: accepted with type %s instead of %s", p.Name, wrong, p.Type)
				}
			}
		}
	}
	extra := Clone(s.Base)
	extra.Outputs["zz_unknown"] = graph.PortDefinition{Type: graph.ValueTypeString}
	if s.Validate(extra) == nil {
		t.Errorf("an output without a port descriptor is accepted")
	}
}
