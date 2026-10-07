package graph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Severity classifies whether a diagnostic blocks graph execution.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// DiagnosticLocation identifies the definition element associated with a
// diagnostic. F0.1 parsing does not retain YAML source positions, so Path is
// the stable location available to callers in schema v0.
type DiagnosticLocation struct {
	NodeID                 string `json:"node_id,omitempty"`
	EdgeIndex              *int   `json:"edge_index,omitempty"`
	GraphInputBindingIndex *int   `json:"graph_input_binding_index,omitempty"`
	Path                   string `json:"path,omitempty"`
}

// Diagnostic is one machine-readable validation finding.
type Diagnostic struct {
	Severity   Severity           `json:"severity"`
	Code       string             `json:"code"`
	Message    string             `json:"message"`
	GraphID    string             `json:"graph_id,omitempty"`
	Location   DiagnosticLocation `json:"location,omitempty"`
	Suggestion string             `json:"suggestion,omitempty"`
}

// ValidationResult is the complete result of validating one graph definition.
// Warnings are retained even when the graph has blocking errors.
type ValidationResult struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Valid reports whether the result contains no blocking diagnostics.
func (r ValidationResult) Valid() bool {
	return !r.HasErrors()
}

// HasErrors reports whether the result contains a blocking diagnostic.
func (r ValidationResult) HasErrors() bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Validate performs deterministic, side-effect-free semantic validation of a
// parsed graph definition. Structural YAML decoding remains the responsibility
// of Parse.
func Validate(definition *Definition) ValidationResult {
	validator := definitionValidator{definition: definition, usedOutputs: make(map[string]bool)}
	if definition == nil {
		validator.error("E_DEFINITION_NIL", "graph definition is nil", DiagnosticLocation{}, "provide a parsed graph definition")
		return validator.result()
	}

	validator.validateDefinition()
	validator.validateEdges()
	validator.validateGraphInputBindings()
	validator.validateGraphOutputs()
	validator.validateRequiredInputs()
	validator.validateCycles()
	validator.validateUnusedOutputs()
	return validator.result()
}

type definitionValidator struct {
	definition   *Definition
	diagnostics  []Diagnostic
	providers    map[string]int
	dependencies []dependency
	usedOutputs  map[string]bool
}

type dependency struct {
	from      string
	to        string
	edgeIndex int
}

type nodeOutputReference struct {
	nodeID string
	portID string
}

type nodeInputReference struct {
	nodeID string
	portID string
}

func (v *definitionValidator) validateDefinition() {
	if v.definition.ID == "" {
		v.error("E_GRAPH_ID_REQUIRED", "graph id is required", DiagnosticLocation{Path: "id"}, "set a stable graph id")
	} else if !validIdentifier(v.definition.ID) {
		v.error("E_GRAPH_ID_INVALID", fmt.Sprintf("graph id %q is invalid", v.definition.ID), DiagnosticLocation{Path: "id"}, "use lowercase letters, digits, hyphens, and underscores")
	}

	v.requireCollection(v.definition.Inputs != nil, "inputs")
	v.requireCollection(v.definition.Nodes != nil, "nodes")
	v.requireCollection(v.definition.Edges != nil, "edges")
	v.requireCollection(v.definition.Outputs != nil, "outputs")

	for _, inputID := range sortedInputIDs(v.definition.Inputs) {
		input := v.definition.Inputs[inputID]
		path := mapPath("inputs", inputID)
		v.validateIdentifier(inputID, path, "graph input", "")
		v.validateValueType(input.Type, path+".type", "")
	}

	for _, nodeID := range sortedNodeIDs(v.definition.Nodes) {
		node := v.definition.Nodes[nodeID]
		nodePath := mapPath("nodes", nodeID)
		v.validateIdentifier(nodeID, nodePath, "node id", nodeID)
		if node.Type == "" {
			v.error("E_NODE_TYPE_REQUIRED", fmt.Sprintf("node %q is missing required type", nodeID), DiagnosticLocation{NodeID: nodeID, Path: nodePath + ".type"}, "set the node type")
		}
		for _, inputID := range sortedPortIDs(node.Inputs) {
			port := node.Inputs[inputID]
			path := mapPath(nodePath+".inputs", inputID)
			v.validateIdentifier(inputID, path, "node input", nodeID)
			v.validateValueType(port.Type, path+".type", nodeID)
		}
		for _, outputID := range sortedPortIDs(node.Outputs) {
			port := node.Outputs[outputID]
			path := mapPath(nodePath+".outputs", outputID)
			v.validateIdentifier(outputID, path, "node output", nodeID)
			v.validateValueType(port.Type, path+".type", nodeID)
		}
	}

	for _, outputID := range sortedOutputIDs(v.definition.Outputs) {
		v.validateIdentifier(outputID, mapPath("outputs", outputID), "graph output", "")
	}
}

func (v *definitionValidator) requireCollection(present bool, name string) {
	if !present {
		v.error("E_GRAPH_FIELD_REQUIRED", fmt.Sprintf("graph is missing required %q collection", name), DiagnosticLocation{Path: name}, "declare the collection, using an empty mapping or list when appropriate")
	}
}

func (v *definitionValidator) validateIdentifier(identifier, path, kind, nodeID string) {
	if validIdentifier(identifier) {
		return
	}
	v.error("E_IDENTIFIER_INVALID", fmt.Sprintf("%s %q is invalid", kind, identifier), DiagnosticLocation{NodeID: nodeID, Path: path}, "use lowercase letters, digits, hyphens, and underscores")
}

func (v *definitionValidator) validateValueType(valueType ValueType, path, nodeID string) {
	if supportedValueType(valueType) {
		return
	}
	v.error("E_VALUE_TYPE_INVALID", fmt.Sprintf("value type %q is not supported", valueType), DiagnosticLocation{NodeID: nodeID, Path: path}, "use string, number, boolean, json, or artifact_ref")
}

func (v *definitionValidator) validateEdges() {
	for edgeIndex, edge := range v.definition.Edges {
		fromPath := fmt.Sprintf("edges[%d].from", edgeIndex)
		toPath := fmt.Sprintf("edges[%d].to", edgeIndex)
		from, fromValid := v.parseNodeOutputReference(string(edge.From), fromPath, edgeIndex, "E_EDGE_SOURCE_REFERENCE_INVALID")
		to, toValid := v.parseNodeInputReference(string(edge.To), toPath, edgeIndex, "E_EDGE_TARGET_REFERENCE_INVALID")

		fromPort, sourceValid := v.resolveNodeOutput(from, fromValid, edgeIndex, fromPath, "E_EDGE_SOURCE")
		toPort, targetValid := v.resolveNodeInput(to, toValid, edgeIndex, toPath, "E_EDGE_TARGET")
		if sourceValid && targetValid {
			v.dependencies = append(v.dependencies, dependency{from: from.nodeID, to: to.nodeID, edgeIndex: edgeIndex})
			if supportedValueType(fromPort.Type) && supportedValueType(toPort.Type) && fromPort.Type != toPort.Type {
				v.error("E_BINDING_TYPE_MISMATCH", fmt.Sprintf("edge binds %q (%s) to %q (%s)", edge.From, fromPort.Type, edge.To, toPort.Type), edgeLocation(edgeIndex, fmt.Sprintf("edges[%d]", edgeIndex)), "bind ports with the same declared type")
			} else if supportedValueType(fromPort.Type) && supportedValueType(toPort.Type) {
				v.addProvider(to.nodeID, to.portID, edgeLocation(edgeIndex, toPath))
				v.usedOutputs[outputKey(from.nodeID, from.portID)] = true
			}
		}
	}
}

func (v *definitionValidator) validateGraphInputBindings() {
	for bindingIndex, binding := range v.definition.GraphInputBindings {
		fromPath := fmt.Sprintf("graph_input_bindings[%d].from", bindingIndex)
		toPath := fmt.Sprintf("graph_input_bindings[%d].to", bindingIndex)
		inputID, inputValid := v.parseGraphInputReference(string(binding.From), fromPath, bindingIndex)
		to, toValid := v.parseNodeInputReferenceForBinding(string(binding.To), toPath, bindingIndex)

		input, sourceValid := v.definition.Inputs[inputID]
		if inputValid {
			if _, ok := v.definition.Inputs[inputID]; !ok {
				v.error("E_GRAPH_INPUT_NOT_FOUND", fmt.Sprintf("graph input %q does not exist", inputID), bindingLocation(bindingIndex, fromPath), "declare the graph input or update the binding")
				sourceValid = false
			}
		}

		toPort, targetValid := v.resolveNodeInputForBinding(to, toValid, bindingIndex, toPath)
		if sourceValid && targetValid && supportedValueType(input.Type) && supportedValueType(toPort.Type) {
			if input.Type != toPort.Type {
				v.error("E_BINDING_TYPE_MISMATCH", fmt.Sprintf("graph input binding binds %q (%s) to %q (%s)", binding.From, input.Type, binding.To, toPort.Type), bindingLocation(bindingIndex, fmt.Sprintf("graph_input_bindings[%d]", bindingIndex)), "bind ports with the same declared type")
			} else {
				v.addProvider(to.nodeID, to.portID, bindingLocation(bindingIndex, toPath))
			}
		}
	}
}

func (v *definitionValidator) validateGraphOutputs() {
	for _, outputID := range sortedOutputIDs(v.definition.Outputs) {
		output := v.definition.Outputs[outputID]
		path := mapPath("outputs", outputID) + ".from"
		from, valid := v.parseNodeOutputReference(string(output.From), path, -1, "E_GRAPH_OUTPUT_REFERENCE_INVALID")
		if !valid {
			continue
		}
		node, ok := v.definition.Nodes[from.nodeID]
		if !ok {
			v.error("E_GRAPH_OUTPUT_SOURCE_NODE_NOT_FOUND", fmt.Sprintf("graph output %q references missing node %q", outputID, from.nodeID), DiagnosticLocation{Path: path}, "declare the node or update the graph output")
			continue
		}
		if _, ok := node.Outputs[from.portID]; !ok {
			v.error("E_GRAPH_OUTPUT_SOURCE_OUTPUT_NOT_FOUND", fmt.Sprintf("graph output %q references missing output %q on node %q", outputID, from.portID, from.nodeID), DiagnosticLocation{NodeID: from.nodeID, Path: path}, "declare the node output or update the graph output")
		} else {
			v.usedOutputs[outputKey(from.nodeID, from.portID)] = true
		}
	}
}

func (v *definitionValidator) validateRequiredInputs() {
	for _, nodeID := range sortedNodeIDs(v.definition.Nodes) {
		node := v.definition.Nodes[nodeID]
		for _, inputID := range sortedPortIDs(node.Inputs) {
			if !node.Inputs[inputID].Required || v.providers[inputKey(nodeID, inputID)] > 0 {
				continue
			}
			path := mapPath(mapPath("nodes", nodeID)+".inputs", inputID)
			v.error("E_REQUIRED_INPUT_UNBOUND", fmt.Sprintf("required input %q on node %q has no valid binding", inputID, nodeID), DiagnosticLocation{NodeID: nodeID, Path: path}, "add one compatible edge or graph input binding")
		}
	}
}

func (v *definitionValidator) validateCycles() {
	adjacency := make(map[string][]dependency, len(v.definition.Nodes))
	for _, nodeID := range sortedNodeIDs(v.definition.Nodes) {
		adjacency[nodeID] = nil
	}
	for _, dependency := range v.dependencies {
		adjacency[dependency.from] = append(adjacency[dependency.from], dependency)
	}
	for nodeID := range adjacency {
		sort.Slice(adjacency[nodeID], func(i, j int) bool {
			return adjacency[nodeID][i].edgeIndex < adjacency[nodeID][j].edgeIndex
		})
	}

	state := make(map[string]uint8, len(adjacency))
	stack := make([]string, 0, len(adjacency))
	positions := make(map[string]int, len(adjacency))
	var visit func(string)
	visit = func(nodeID string) {
		state[nodeID] = 1
		positions[nodeID] = len(stack)
		stack = append(stack, nodeID)
		for _, dependency := range adjacency[nodeID] {
			switch state[dependency.to] {
			case 0:
				visit(dependency.to)
			case 1:
				cycle := append([]string(nil), stack[positions[dependency.to]:]...)
				cycle = append(cycle, dependency.to)
				path := fmt.Sprintf("edges[%d]", dependency.edgeIndex)
				v.error("E_GRAPH_CYCLE", fmt.Sprintf("graph contains cycle: %s", strings.Join(cycle, " -> ")), edgeLocation(dependency.edgeIndex, path), "remove a dependency from the cycle")
			}
		}
		stack = stack[:len(stack)-1]
		delete(positions, nodeID)
		state[nodeID] = 2
	}

	for _, nodeID := range sortedNodeIDs(v.definition.Nodes) {
		if state[nodeID] == 0 {
			visit(nodeID)
		}
	}
}

func (v *definitionValidator) validateUnusedOutputs() {
	for _, nodeID := range sortedNodeIDs(v.definition.Nodes) {
		for _, outputID := range sortedPortIDs(v.definition.Nodes[nodeID].Outputs) {
			if v.usedOutputs[outputKey(nodeID, outputID)] {
				continue
			}
			path := mapPath(mapPath("nodes", nodeID)+".outputs", outputID)
			v.warning("W_DECLARED_OUTPUT_UNUSED", fmt.Sprintf("output %q on node %q is not consumed", outputID, nodeID), DiagnosticLocation{NodeID: nodeID, Path: path}, "remove the output or bind it to an edge or graph output")
		}
	}
}

func (v *definitionValidator) parseNodeOutputReference(reference, path string, edgeIndex int, code string) (nodeOutputReference, bool) {
	parsed, ok := parseNodeOutputReference(reference)
	if !ok {
		v.error(code, fmt.Sprintf("%q is not a valid node output reference", reference), edgeLocation(edgeIndex, path), "use NODE.outputs.PORT")
	}
	return parsed, ok
}

func (v *definitionValidator) parseNodeInputReference(reference, path string, edgeIndex int, code string) (nodeInputReference, bool) {
	parsed, ok := parseNodeInputReference(reference)
	if !ok {
		v.error(code, fmt.Sprintf("%q is not a valid node input reference", reference), edgeLocation(edgeIndex, path), "use NODE.inputs.PORT")
	}
	return parsed, ok
}

func (v *definitionValidator) parseNodeInputReferenceForBinding(reference, path string, bindingIndex int) (nodeInputReference, bool) {
	parsed, ok := parseNodeInputReference(reference)
	if !ok {
		v.error("E_GRAPH_INPUT_TARGET_REFERENCE_INVALID", fmt.Sprintf("%q is not a valid node input reference", reference), bindingLocation(bindingIndex, path), "use NODE.inputs.PORT")
	}
	return parsed, ok
}

func (v *definitionValidator) parseGraphInputReference(reference, path string, bindingIndex int) (string, bool) {
	parts := strings.Split(reference, ".")
	if len(parts) == 2 && parts[0] == "inputs" && validIdentifier(parts[1]) {
		return parts[1], true
	}
	v.error("E_GRAPH_INPUT_REFERENCE_INVALID", fmt.Sprintf("%q is not a valid graph input reference", reference), bindingLocation(bindingIndex, path), "use inputs.INPUT")
	return "", false
}

func (v *definitionValidator) resolveNodeOutput(reference nodeOutputReference, referenceValid bool, edgeIndex int, path, codePrefix string) (PortDefinition, bool) {
	if !referenceValid {
		return PortDefinition{}, false
	}
	node, ok := v.definition.Nodes[reference.nodeID]
	if !ok {
		v.error(codePrefix+"_NODE_NOT_FOUND", fmt.Sprintf("reference %q uses missing node %q", reference.nodeID+".outputs."+reference.portID, reference.nodeID), edgeLocation(edgeIndex, path), "declare the node or update the reference")
		return PortDefinition{}, false
	}
	port, ok := node.Outputs[reference.portID]
	if !ok {
		v.error(codePrefix+"_OUTPUT_NOT_FOUND", fmt.Sprintf("output %q does not exist on node %q", reference.portID, reference.nodeID), DiagnosticLocation{NodeID: reference.nodeID, EdgeIndex: intPointer(edgeIndex), Path: path}, "declare the output or update the reference")
		return PortDefinition{}, false
	}
	return port, true
}

func (v *definitionValidator) resolveNodeInput(reference nodeInputReference, referenceValid bool, edgeIndex int, path, codePrefix string) (PortDefinition, bool) {
	if !referenceValid {
		return PortDefinition{}, false
	}
	node, ok := v.definition.Nodes[reference.nodeID]
	if !ok {
		v.error(codePrefix+"_NODE_NOT_FOUND", fmt.Sprintf("reference %q uses missing node %q", reference.nodeID+".inputs."+reference.portID, reference.nodeID), edgeLocation(edgeIndex, path), "declare the node or update the reference")
		return PortDefinition{}, false
	}
	port, ok := node.Inputs[reference.portID]
	if !ok {
		v.error(codePrefix+"_INPUT_NOT_FOUND", fmt.Sprintf("input %q does not exist on node %q", reference.portID, reference.nodeID), DiagnosticLocation{NodeID: reference.nodeID, EdgeIndex: intPointer(edgeIndex), Path: path}, "declare the input or update the reference")
		return PortDefinition{}, false
	}
	return port, true
}

func (v *definitionValidator) resolveNodeInputForBinding(reference nodeInputReference, referenceValid bool, bindingIndex int, path string) (PortDefinition, bool) {
	if !referenceValid {
		return PortDefinition{}, false
	}
	node, ok := v.definition.Nodes[reference.nodeID]
	if !ok {
		v.error("E_GRAPH_INPUT_TARGET_NODE_NOT_FOUND", fmt.Sprintf("node %q does not exist", reference.nodeID), bindingLocation(bindingIndex, path), "declare the node or update the binding")
		return PortDefinition{}, false
	}
	port, ok := node.Inputs[reference.portID]
	if !ok {
		v.error("E_GRAPH_INPUT_TARGET_INPUT_NOT_FOUND", fmt.Sprintf("input %q does not exist on node %q", reference.portID, reference.nodeID), DiagnosticLocation{NodeID: reference.nodeID, GraphInputBindingIndex: intPointer(bindingIndex), Path: path}, "declare the input or update the binding")
		return PortDefinition{}, false
	}
	return port, true
}

func (v *definitionValidator) addProvider(nodeID, inputID string, location DiagnosticLocation) {
	if v.providers == nil {
		v.providers = make(map[string]int)
	}
	key := inputKey(nodeID, inputID)
	v.providers[key]++
	if v.providers[key] > 1 {
		path := mapPath(mapPath("nodes", nodeID)+".inputs", inputID)
		location.NodeID = nodeID
		location.Path = path
		v.error("E_INPUT_MULTIPLE_PROVIDERS", fmt.Sprintf("input %q on node %q has multiple providers", inputID, nodeID), location, "bind exactly one source to this input")
	}
}

func (v *definitionValidator) error(code, message string, location DiagnosticLocation, suggestion string) {
	v.diagnostics = append(v.diagnostics, Diagnostic{Severity: SeverityError, Code: code, Message: message, GraphID: v.graphID(), Location: location, Suggestion: suggestion})
}

func (v *definitionValidator) warning(code, message string, location DiagnosticLocation, suggestion string) {
	v.diagnostics = append(v.diagnostics, Diagnostic{Severity: SeverityWarning, Code: code, Message: message, GraphID: v.graphID(), Location: location, Suggestion: suggestion})
}

func (v *definitionValidator) graphID() string {
	if v.definition == nil {
		return ""
	}
	return v.definition.ID
}

func (v *definitionValidator) result() ValidationResult {
	sort.SliceStable(v.diagnostics, func(i, j int) bool {
		left, right := v.diagnostics[i], v.diagnostics[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Location.Path != right.Location.Path {
			return left.Location.Path < right.Location.Path
		}
		return left.Code < right.Code
	})
	return ValidationResult{Diagnostics: v.diagnostics}
}

func parseNodeOutputReference(reference string) (nodeOutputReference, bool) {
	parts := strings.Split(reference, ".")
	if len(parts) != 3 || parts[1] != "outputs" || !validIdentifier(parts[0]) || !validIdentifier(parts[2]) {
		return nodeOutputReference{}, false
	}
	return nodeOutputReference{nodeID: parts[0], portID: parts[2]}, true
}

func parseNodeInputReference(reference string) (nodeInputReference, bool) {
	parts := strings.Split(reference, ".")
	if len(parts) != 3 || parts[1] != "inputs" || !validIdentifier(parts[0]) || !validIdentifier(parts[2]) {
		return nodeInputReference{}, false
	}
	return nodeInputReference{nodeID: parts[0], portID: parts[2]}, true
}

func validIdentifier(identifier string) bool {
	if identifier == "" || identifier[0] < 'a' || identifier[0] > 'z' {
		return false
	}
	for _, character := range identifier[1:] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func supportedValueType(valueType ValueType) bool {
	switch valueType {
	case ValueTypeString, ValueTypeNumber, ValueTypeBoolean, ValueTypeJSON, ValueTypeArtifactRef:
		return true
	default:
		return false
	}
}

func sortedInputIDs(inputs map[string]InputDefinition) []string {
	return sortedKeys(inputs)
}

func sortedNodeIDs(nodes map[string]NodeDefinition) []string {
	return sortedKeys(nodes)
}

func sortedPortIDs(ports map[string]PortDefinition) []string {
	return sortedKeys(ports)
}

func sortedOutputIDs(outputs map[string]OutputDefinition) []string {
	return sortedKeys(outputs)
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func inputKey(nodeID, inputID string) string {
	return nodeID + "\x00" + inputID
}

func outputKey(nodeID, outputID string) string {
	return nodeID + "\x00" + outputID
}

func mapPath(prefix, key string) string {
	if validIdentifier(key) {
		return prefix + "." + key
	}
	return prefix + "[" + strconv.Quote(key) + "]"
}

func edgeLocation(edgeIndex int, path string) DiagnosticLocation {
	if edgeIndex < 0 {
		return DiagnosticLocation{Path: path}
	}
	return DiagnosticLocation{EdgeIndex: intPointer(edgeIndex), Path: path}
}

func bindingLocation(bindingIndex int, path string) DiagnosticLocation {
	return DiagnosticLocation{GraphInputBindingIndex: intPointer(bindingIndex), Path: path}
}

func intPointer(value int) *int {
	return &value
}
