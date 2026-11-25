package graph

import (
	"fmt"
	"sort"
	"strings"
)

// BindingPlan is a validated, immutable view of declared graph bindings.
type BindingPlan struct {
	graphID      string
	graphInputs  map[string]InputDefinition
	graphOutputs map[string]resolvedGraphOutput
	nodes        map[string]bindingNode
	dependencies map[string][]string
}

type resolvedGraphOutput struct {
	sourceNodeID string
	sourcePortID string
	valueType    ValueType
}

type bindingNode struct {
	inputs   map[string]PortDefinition
	bindings map[string]resolvedBinding
}

type resolvedBinding struct {
	graphInputID string
	sourceNodeID string
	sourcePortID string
	edgeIndex    int
	bindingIndex int
}

// CompileBindings validates a definition once and compiles its explicit
// bindings. Warning-only definitions still produce a plan.
func CompileBindings(definition *Definition) (*BindingPlan, ValidationResult) {
	result := Validate(definition)
	if !result.Valid() {
		return nil, result
	}

	plan := &BindingPlan{
		graphID:      definition.ID,
		graphInputs:  make(map[string]InputDefinition, len(definition.Inputs)),
		graphOutputs: make(map[string]resolvedGraphOutput, len(definition.Outputs)),
		nodes:        make(map[string]bindingNode, len(definition.Nodes)),
		dependencies: make(map[string][]string, len(definition.Nodes)),
	}
	for inputID, input := range definition.Inputs {
		plan.graphInputs[inputID] = input
	}
	for outputID, output := range definition.Outputs {
		source, _ := parseNodeOutputReference(string(output.From))
		plan.graphOutputs[outputID] = resolvedGraphOutput{
			sourceNodeID: source.nodeID,
			sourcePortID: source.portID,
			valueType:    definition.Nodes[source.nodeID].Outputs[source.portID].Type,
		}
	}
	for nodeID, node := range definition.Nodes {
		inputs := make(map[string]PortDefinition, len(node.Inputs))
		for inputID, input := range node.Inputs {
			inputs[inputID] = input
		}
		plan.nodes[nodeID] = bindingNode{inputs: inputs, bindings: make(map[string]resolvedBinding)}
	}
	// Validate already established that every reference parses, resolves, and
	// has at most one provider per target input.
	for bindingIndex, binding := range definition.GraphInputBindings {
		inputID, _ := parseGraphInputReference(string(binding.From))
		target, _ := parseNodeInputReference(string(binding.To))
		node := plan.nodes[target.nodeID]
		node.bindings[target.portID] = resolvedBinding{graphInputID: inputID, bindingIndex: bindingIndex, edgeIndex: -1}
		plan.nodes[target.nodeID] = node
	}
	for edgeIndex, edge := range definition.Edges {
		source, _ := parseNodeOutputReference(string(edge.From))
		target, _ := parseNodeInputReference(string(edge.To))
		node := plan.nodes[target.nodeID]
		node.bindings[target.portID] = resolvedBinding{sourceNodeID: source.nodeID, sourcePortID: source.portID, edgeIndex: edgeIndex, bindingIndex: -1}
		plan.nodes[target.nodeID] = node
		plan.dependencies[target.nodeID] = append(plan.dependencies[target.nodeID], source.nodeID)
	}
	for nodeID, dependencies := range plan.dependencies {
		sort.Strings(dependencies)
		plan.dependencies[nodeID] = compactStrings(dependencies)
	}
	return plan, result
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	write := 1
	for _, value := range values[1:] {
		if value != values[write-1] {
			values[write] = value
			write++
		}
	}
	return values[:write]
}

// Dependencies returns sorted, distinct direct producer node IDs.
func (p *BindingPlan) Dependencies(nodeID string) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("binding plan is nil")
	}
	if _, ok := p.nodes[nodeID]; !ok {
		return nil, fmt.Errorf("unknown node %q", nodeID)
	}
	return append([]string(nil), p.dependencies[nodeID]...), nil
}

// ValidateGraphInputs checks supplied run inputs against the graph's declared
// input contract. It does not read any undeclared ambient values.
func (p *BindingPlan) ValidateGraphInputs(values map[string]Value) ValidationResult {
	if p == nil {
		return nilBindingPlanResult()
	}
	var diagnostics []Diagnostic
	for _, inputID := range sortedInputIDs(p.graphInputs) {
		input := p.graphInputs[inputID]
		value, present := values[inputID]
		path := mapPath("inputs", inputID)
		if !present && input.Required {
			diagnostics = append(diagnostics, p.diagnostic("E_GRAPH_INPUT_VALUE_MISSING", fmt.Sprintf("required graph input %q has no value", inputID), DiagnosticLocation{Path: path}, "supply the graph input value"))
		} else if present && value.Type() != input.Type {
			diagnostics = append(diagnostics, p.diagnostic("E_GRAPH_INPUT_VALUE_TYPE_MISMATCH", fmt.Sprintf("graph input %q expects %s but received %s", inputID, input.Type, value.Type()), DiagnosticLocation{Path: path}, "supply a value with the declared type"))
		}
	}
	diagnostics = append(diagnostics, p.unknownInputDiagnostics(values)...)
	return sortedValidationResult(diagnostics)
}

// unknownInputDiagnostics reports supplied values the graph does not declare.
// Their keys come from outside the graph (--input, the API) and are never
// echoed, even when they match the identifier grammar: the F3.8 input guard
// runs after this check, so a pasted secret would otherwise reach the
// terminal, logs or an API response. The diagnostics are numbered only to tell
// them apart; the numbers do not identify a key.
func (p *BindingPlan) unknownInputDiagnostics(values map[string]Value) []Diagnostic {
	var unknown int
	for inputID := range values {
		if _, declared := p.graphInputs[inputID]; !declared {
			unknown++
		}
	}
	suggestion := "remove the value or declare the graph input; declared inputs: " + p.declaredInputList()
	diagnostics := make([]Diagnostic, 0, unknown)
	for range unknown {
		message := fmt.Sprintf("a supplied graph input is not declared (%d of %d)", len(diagnostics)+1, unknown)
		diagnostics = append(diagnostics, p.diagnostic("E_GRAPH_INPUT_VALUE_UNKNOWN", message, DiagnosticLocation{Path: "inputs"}, suggestion))
	}
	return diagnostics
}

func (p *BindingPlan) declaredInputList() string {
	ids := sortedInputIDs(p.graphInputs)
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

// ResolveNodeInputs maps available declared source values to one node's
// inputs. Callers must first establish that direct producer nodes succeeded;
// this method does not schedule nodes or distinguish pending from missing
// outputs. Errors return a nil input map rather than usable partial inputs.
func (p *BindingPlan) ResolveNodeInputs(nodeID string, graphInputs map[string]Value, nodeOutputs map[string]map[string]Value) (map[string]Value, ValidationResult) {
	if p == nil {
		return nil, nilBindingPlanResult()
	}
	node, ok := p.nodes[nodeID]
	if !ok {
		path := mapPath("nodes", nodeID)
		return nil, ValidationResult{Diagnostics: []Diagnostic{p.diagnostic("E_RESOLUTION_NODE_NOT_FOUND", fmt.Sprintf("node %q does not exist", nodeID), DiagnosticLocation{NodeID: nodeID, Path: path}, "resolve inputs for a declared node")}}
	}
	inputs := make(map[string]Value)
	var diagnostics []Diagnostic
	for _, inputID := range sortedPortIDs(node.inputs) {
		binding, bound := node.bindings[inputID]
		if !bound {
			continue
		}
		input := node.inputs[inputID]
		location := DiagnosticLocation{NodeID: nodeID, Path: mapPath(mapPath("nodes", nodeID)+".inputs", inputID)}
		var value Value
		var present bool
		if binding.graphInputID != "" {
			location.GraphInputBindingIndex = intPointer(binding.bindingIndex)
			value, present = graphInputs[binding.graphInputID]
			if !present && (input.Required || p.graphInputs[binding.graphInputID].Required) {
				diagnostics = append(diagnostics, p.diagnostic("E_GRAPH_INPUT_VALUE_MISSING", fmt.Sprintf("graph input %q has no value for node %q input %q", binding.graphInputID, nodeID, inputID), location, "supply the graph input value"))
			}
		} else {
			location.EdgeIndex = intPointer(binding.edgeIndex)
			value, present = nodeOutputs[binding.sourceNodeID][binding.sourcePortID]
			if !present {
				diagnostics = append(diagnostics, p.diagnostic("E_BOUND_OUTPUT_VALUE_MISSING", fmt.Sprintf("node output %q has no value for node %q input %q", binding.sourceNodeID+".outputs."+binding.sourcePortID, nodeID, inputID), location, "provide the declared source output value"))
			}
		}
		if !present {
			continue
		}
		if value.Type() != input.Type {
			diagnostics = append(diagnostics, p.diagnostic("E_BOUND_VALUE_TYPE_MISMATCH", fmt.Sprintf("node %q input %q expects %s but received %s", nodeID, inputID, input.Type, value.Type()), location, "provide a value with the declared type"))
			continue
		}
		inputs[inputID] = value.clone()
	}
	if len(diagnostics) > 0 {
		return nil, sortedValidationResult(diagnostics)
	}
	return inputs, ValidationResult{}
}

// ResolveGraphOutputs materializes exposed graph values after all nodes have
// succeeded. Missing and mistyped values make the entire result unusable.
func (p *BindingPlan) ResolveGraphOutputs(nodeOutputs map[string]map[string]Value) (map[string]Value, ValidationResult) {
	if p == nil {
		return nil, nilBindingPlanResult()
	}
	outputs := make(map[string]Value, len(p.graphOutputs))
	var diagnostics []Diagnostic
	for _, outputID := range sortedKeys(p.graphOutputs) {
		binding := p.graphOutputs[outputID]
		value, present := nodeOutputs[binding.sourceNodeID][binding.sourcePortID]
		location := DiagnosticLocation{NodeID: binding.sourceNodeID, Path: mapPath("outputs", outputID) + ".from"}
		if !present {
			diagnostics = append(diagnostics, p.diagnostic("E_GRAPH_OUTPUT_VALUE_MISSING", fmt.Sprintf("graph output %q has no value from %q", outputID, binding.sourceNodeID+".outputs."+binding.sourcePortID), location, "provide the declared source output value"))
			continue
		}
		if value.Type() != binding.valueType {
			diagnostics = append(diagnostics, p.diagnostic("E_GRAPH_OUTPUT_VALUE_TYPE_MISMATCH", fmt.Sprintf("graph output %q expects %s but received %s", outputID, binding.valueType, value.Type()), location, "provide a value with the declared type"))
			continue
		}
		outputs[outputID] = value.clone()
	}
	if len(diagnostics) > 0 {
		return nil, sortedValidationResult(diagnostics)
	}
	return outputs, ValidationResult{}
}

func (p *BindingPlan) diagnostic(code, message string, location DiagnosticLocation, suggestion string) Diagnostic {
	return Diagnostic{Severity: SeverityError, Code: code, Message: message, GraphID: p.graphID, Location: location, Suggestion: suggestion}
}

func nilBindingPlanResult() ValidationResult {
	return ValidationResult{Diagnostics: []Diagnostic{{Severity: SeverityError, Code: "E_BINDING_PLAN_NIL", Message: "binding plan is nil", Suggestion: "compile a valid graph definition first"}}}
}
