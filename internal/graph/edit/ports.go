package edit

import (
	"fmt"

	"agentplane/internal/graph"

	"go.yaml.in/yaml/v3"
)

var valueTypes = map[graph.ValueType]bool{
	graph.ValueTypeString: true, graph.ValueTypeNumber: true, graph.ValueTypeBoolean: true,
	graph.ValueTypeJSON: true, graph.ValueTypeArtifactRef: true,
}

// portSection is the node key of a direction: inputs or outputs.
func portSection(direction string) string {
	if direction == DirectionInput {
		return "inputs"
	}
	return "outputs"
}

func portError(code string, op Op, message, suggestion string) *Error {
	e := nodeError(code, op.Node, message, suggestion)
	e.Path = "nodes." + op.Node + "." + portSection(op.Direction) + "." + op.Port
	return e
}

func ports(n *graph.NodeDefinition, direction string) *map[string]graph.PortDefinition {
	if direction == DirectionInput {
		return &n.Inputs
	}
	return &n.Outputs
}

func (s *session) addPort(op Op) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	if !graph.ValidIdentifier(op.Port) {
		return portError(CodePortInvalid, op, fmt.Sprintf("%q is not a valid port name", op.Port), "start with a lowercase letter, then lowercase letters, digits, - or _")
	}
	if !valueTypes[graph.ValueType(op.Type)] {
		return portError(CodePortInvalid, op, fmt.Sprintf("%q is not a port type", op.Type), "use string, number, boolean, json or artifact_ref")
	}
	set := ports(&n, op.Direction)
	if _, exists := (*set)[op.Port]; exists {
		return portError(CodePortExists, op, fmt.Sprintf("%s %q already exists", op.Direction, op.Port), "choose another name")
	}
	required := op.Required != nil && *op.Required
	// typed prediction
	if *set == nil {
		*set = map[string]graph.PortDefinition{}
	}
	(*set)[op.Port] = graph.PortDefinition{Type: graph.ValueType(op.Type), Required: required}
	s.want.Nodes[op.Node] = n
	// tree
	node, err := s.nodeTree(op.Node)
	if err != nil {
		return err
	}
	section := portSection(op.Direction)
	m, ok := childMapping(node, section, true, func(v *yaml.Node) { insertNodeKey(node, section, v) })
	if !ok {
		return portError(CodeParse, op, section+" is not a mapping", "fix the YAML text")
	}
	flow := true // new ports look like the existing ones; `{type: string}` by default
	if len(m.Content) > 0 {
		flow = m.Content[1].Style&yaml.FlowStyle != 0
	}
	value := mapNode(flow, str("type"), str(op.Type))
	if required {
		value.Content = append(value.Content, str("required"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	m.Content = append(m.Content, str(op.Port), value)
	return nil
}

func (s *session) updatePort(op Op) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	set := ports(&n, op.Direction)
	port, exists := (*set)[op.Port]
	if !exists {
		return portError(CodePortNotFound, op, fmt.Sprintf("%s %q does not exist", op.Direction, op.Port), "")
	}
	if op.Type != "" && graph.ValueType(op.Type) != port.Type {
		if !valueTypes[graph.ValueType(op.Type)] {
			return portError(CodePortInvalid, op, fmt.Sprintf("%q is not a port type", op.Type), "use string, number, boolean, json or artifact_ref")
		}
		if e := s.typeChangeConflict(op, n, port); e != nil {
			return e
		}
		port.Type = graph.ValueType(op.Type)
	}
	if op.Required != nil {
		port.Required = *op.Required
	}
	name := op.Port
	if op.NewPort != "" && op.NewPort != op.Port {
		if !graph.ValidIdentifier(op.NewPort) {
			return portError(CodePortInvalid, op, fmt.Sprintf("%q is not a valid port name", op.NewPort), "start with a lowercase letter, then lowercase letters, digits, - or _")
		}
		if _, taken := (*set)[op.NewPort]; taken {
			return portError(CodePortExists, op, fmt.Sprintf("%s %q already exists", op.Direction, op.NewPort), "choose another name")
		}
		name = op.NewPort
	}
	// typed prediction
	delete(*set, op.Port)
	(*set)[name] = port
	s.want.Nodes[op.Node] = n
	// tree
	m, value, err := s.portTree(op)
	if err != nil {
		return err
	}
	setPortFields(value, port, op.Type != "", op.Required != nil)
	if name != op.Port {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == op.Port {
				m.Content[i].Value = name
			}
		}
		base := "nodes." + op.Node + "." + portSection(op.Direction) + "."
		s.renames = append(s.renames, [2]string{base + op.Port, base + name})
		s.rewritePortRefs(op.Node, op.Direction, op.Port, name)
	}
	return nil
}

// typeChangeConflict refuses a type change that would make a connected edge
// or loop binding mismatch; the validator decides, as for can_connect.
func (s *session) typeChangeConflict(op Op, n graph.NodeDefinition, port graph.PortDefinition) *Error {
	after := *s.want
	after.Nodes = make(map[string]graph.NodeDefinition, len(s.want.Nodes))
	for id, v := range s.want.Nodes {
		after.Nodes[id] = v
	}
	changed := n
	if op.Direction == DirectionInput {
		changed.Inputs = copyPorts(n.Inputs)
		changed.Inputs[op.Port] = graph.PortDefinition{Type: graph.ValueType(op.Type), Required: port.Required}
	} else {
		changed.Outputs = copyPorts(n.Outputs)
		changed.Outputs[op.Port] = graph.PortDefinition{Type: graph.ValueType(op.Type), Required: port.Required}
	}
	after.Nodes[op.Node] = changed
	for _, d := range newErrors(s.want, &after) {
		if d.Code == "E_BINDING_TYPE_MISMATCH" || d.Code == "E_LOOP_INVALID" {
			return portError(CodePortConflict, op, d.Message, "remove or change the connected edge or loop binding first")
		}
	}
	return nil
}

func copyPorts(in map[string]graph.PortDefinition) map[string]graph.PortDefinition {
	out := make(map[string]graph.PortDefinition, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// portTree returns the ports mapping of op's direction and op's port value.
func (s *session) portTree(op Op) (*yaml.Node, *yaml.Node, *Error) {
	node, err := s.nodeTree(op.Node)
	if err != nil {
		return nil, nil, err
	}
	m, _ := get(node, portSection(op.Direction))
	value, _ := get(m, op.Port)
	if value == nil || value.Kind != yaml.MappingNode {
		return nil, nil, portError(CodeParse, op, "the port is not a mapping in the file", "fix the YAML text")
	}
	return m, value, nil
}

// setPortFields writes the type and required fields an operation changed into
// a port mapping, keeping its other content and style; required: false is
// written by omission.
func setPortFields(value *yaml.Node, port graph.PortDefinition, touchType, touchRequired bool) {
	if touchType {
		if t, _ := get(value, "type"); t != nil {
			t.Kind, t.Tag, t.Value = yaml.ScalarNode, "!!str", string(port.Type)
		} else {
			value.Content = append(value.Content, str("type"), str(string(port.Type)))
		}
	}
	if !touchRequired {
		return
	}
	r, _ := get(value, "required")
	switch {
	case port.Required && r != nil:
		r.Kind, r.Tag, r.Value = yaml.ScalarNode, "!!bool", "true"
	case port.Required:
		value.Content = append(value.Content, str("required"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	default:
		remove(value, "required")
	}
}

func (s *session) removePort(op Op) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	set := ports(&n, op.Direction)
	if _, exists := (*set)[op.Port]; !exists {
		return portError(CodePortNotFound, op, fmt.Sprintf("%s %q does not exist", op.Direction, op.Port), "")
	}
	// typed prediction
	delete(*set, op.Port)
	if len(*set) == 0 {
		*set = nil
	}
	s.want.Nodes[op.Node] = n
	// tree
	node, err := s.nodeTree(op.Node)
	if err != nil {
		return err
	}
	section := portSection(op.Direction)
	m, _ := get(node, section)
	remove(m, op.Port)
	if m != nil && len(m.Content) == 0 {
		remove(node, section)
	}
	s.rewritePortRefs(op.Node, op.Direction, op.Port, "")
	return nil
}
