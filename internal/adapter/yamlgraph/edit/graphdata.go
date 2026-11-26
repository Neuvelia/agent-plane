package edit

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"agentplane/internal/domain/graph"

	"go.yaml.in/yaml/v3"
)

// ConnectionOption is one counterpart port in a list_connections answer. It is
// the verdict can_connect would give for that pair: the validator decides.
type ConnectionOption struct {
	Ref      string `json:"ref"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	OK       bool   `json:"ok"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

func graphInputRef(id string) string { return "inputs." + id }

func graphDataError(code, path, message, suggestion string) *Error {
	return &Error{Code: code, Message: message, OpIndex: -1, Path: path, Suggestion: suggestion}
}

const identifierHint = "start with a lowercase letter, then lowercase letters, digits, - or _"

func unknownGraphInput(id string) *Error {
	return graphDataError(CodeGraphInputNotFound, "inputs."+id, fmt.Sprintf("graph input %q does not exist", id), "")
}

// topMapping returns the top-level mapping under key, created in its canonical
// place when absent.
func (s *session) topMapping(key string) (*yaml.Node, *Error) {
	m, ok := childMapping(s.top, key, true, func(v *yaml.Node) { insertTop(s.top, key, v) })
	if !ok {
		return nil, graphDataError(CodeParse, key, key+" is not a mapping", "fix the YAML text")
	}
	return m, nil
}

// topSequence returns the top-level sequence under key, created in its
// canonical place when absent or null.
func (s *session) topSequence(key string) (*yaml.Node, *Error) {
	seq, i := get(s.top, key)
	switch {
	case i < 0:
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		insertTop(s.top, key, seq)
	case seq.Kind == yaml.ScalarNode && seq.Tag == "!!null":
		seq.Kind, seq.Tag, seq.Value, seq.Style = yaml.SequenceNode, "!!seq", "", 0
	case seq.Kind != yaml.SequenceNode:
		return nil, graphDataError(CodeParse, key, key+" is not a list", "fix the YAML text")
	}
	return seq, nil
}

func (s *session) addGraphInput(op Op) *Error {
	path := "inputs." + op.ID
	if !graph.ValidIdentifier(op.ID) {
		return graphDataError(CodePortInvalid, path, fmt.Sprintf("%q is not a valid graph input name", op.ID), identifierHint)
	}
	if !valueTypes[graph.ValueType(op.Type)] {
		return graphDataError(CodePortInvalid, path, fmt.Sprintf("%q is not a graph input type", op.Type), "use string, number, boolean, json or artifact_ref")
	}
	if _, exists := s.want.Inputs[op.ID]; exists {
		return graphDataError(CodeGraphInputExists, path, fmt.Sprintf("graph input %q already exists", op.ID), "choose another name")
	}
	required := op.Required != nil && *op.Required
	// typed prediction
	if s.want.Inputs == nil {
		s.want.Inputs = map[string]graph.InputDefinition{}
	}
	s.want.Inputs[op.ID] = graph.InputDefinition{Type: graph.ValueType(op.Type), Required: required}
	// tree
	m, err := s.topMapping("inputs")
	if err != nil {
		return err
	}
	flow := true // new inputs look like the existing ones; `{type: string}` by default
	if len(m.Content) > 0 {
		flow = m.Content[1].Style&yaml.FlowStyle != 0
	}
	value := mapNode(flow, str("type"), str(op.Type))
	if required {
		value.Content = append(value.Content, str("required"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	m.Content = append(m.Content, str(op.ID), value)
	return nil
}

func (s *session) updateGraphInput(op Op) *Error {
	path := "inputs." + op.ID
	in, ok := s.want.Inputs[op.ID]
	if !ok {
		return unknownGraphInput(op.ID)
	}
	if e := s.retypeInput(op, &in); e != nil {
		return e
	}
	if op.Required != nil {
		in.Required = *op.Required
	}
	name, err := s.newInputName(op)
	if err != nil {
		return err
	}
	// tree first: it can refuse (the input is not a mapping in the file)
	m, _ := get(s.top, "inputs")
	value, _ := get(m, op.ID)
	if value == nil || value.Kind != yaml.MappingNode {
		return graphDataError(CodeParse, path, "the graph input is not a mapping in the file", "fix the YAML text")
	}
	setPortFields(value, graph.PortDefinition(in), op.Type != "", op.Required != nil)
	// typed prediction
	delete(s.want.Inputs, op.ID)
	s.want.Inputs[name] = in
	if name != op.ID {
		s.renameInputRefs(m, op.ID, name)
	}
	return nil
}

// retypeInput applies op's type to in, refusing a type that is not a value
// type or that would break a binding.
func (s *session) retypeInput(op Op, in *graph.InputDefinition) *Error {
	if op.Type == "" || graph.ValueType(op.Type) == in.Type {
		return nil
	}
	if !valueTypes[graph.ValueType(op.Type)] {
		return graphDataError(CodePortInvalid, "inputs."+op.ID, fmt.Sprintf("%q is not a graph input type", op.Type), "use string, number, boolean, json or artifact_ref")
	}
	if e := s.inputTypeConflict(op.ID, *in, graph.ValueType(op.Type)); e != nil {
		return e
	}
	in.Type = graph.ValueType(op.Type)
	return nil
}

// newInputName is the name an update_graph_input leaves the input with.
func (s *session) newInputName(op Op) (string, *Error) {
	if op.NewID == "" || op.NewID == op.ID {
		return op.ID, nil
	}
	if !graph.ValidIdentifier(op.NewID) {
		return "", graphDataError(CodePortInvalid, "inputs."+op.ID, fmt.Sprintf("%q is not a valid graph input name", op.NewID), identifierHint)
	}
	if _, taken := s.want.Inputs[op.NewID]; taken {
		return "", graphDataError(CodeGraphInputExists, "inputs."+op.NewID, fmt.Sprintf("graph input %q already exists", op.NewID), "choose another name")
	}
	return op.NewID, nil
}

// renameInputRefs renames the key of a graph input in the tree and every
// binding that reads from it, in the prediction and the tree.
func (s *session) renameInputRefs(inputs *yaml.Node, id, name string) {
	for i := 0; i+1 < len(inputs.Content); i += 2 {
		if inputs.Content[i].Value == id {
			inputs.Content[i].Value = name
		}
	}
	s.renames = append(s.renames, [2]string{"inputs." + id, "inputs." + name})
	old, repl := graphInputRef(id), graphInputRef(name)
	for i := range s.want.GraphInputBindings {
		if string(s.want.GraphInputBindings[i].From) == old {
			s.want.GraphInputBindings[i].From = graph.GraphInputReference(repl)
		}
	}
	if seq, _ := get(s.top, "graph_input_bindings"); seq != nil {
		for _, item := range seq.Content {
			if from, _ := get(item, "from"); from != nil && from.Value == old {
				from.Value = repl
			}
		}
	}
}

// inputTypeConflict refuses a retype that would make a binding mismatch; the
// validator decides, as for a port retype.
func (s *session) inputTypeConflict(id string, in graph.InputDefinition, to graph.ValueType) *Error {
	after := *s.want
	after.Inputs = make(map[string]graph.InputDefinition, len(s.want.Inputs))
	for k, v := range s.want.Inputs {
		after.Inputs[k] = v
	}
	after.Inputs[id] = graph.InputDefinition{Type: to, Required: in.Required}
	for _, d := range newErrors(s.want, &after) {
		if d.Code == "E_BINDING_TYPE_MISMATCH" {
			return graphDataError(CodeGraphInputTypeConflict, "inputs."+id, d.Message, "remove or change the bindings of this input first")
		}
	}
	return nil
}

func (s *session) removeGraphInput(op Op) *Error {
	if _, ok := s.want.Inputs[op.ID]; !ok {
		return unknownGraphInput(op.ID)
	}
	ref := graphInputRef(op.ID)
	// typed prediction
	delete(s.want.Inputs, op.ID)
	s.want.GraphInputBindings = filter(s.want.GraphInputBindings, func(b graph.GraphInputBindingDefinition) bool { return string(b.From) != ref })
	// tree
	if m, _ := get(s.top, "inputs"); m != nil {
		remove(m, op.ID)
	}
	s.dropSeqItems("graph_input_bindings", func(item *yaml.Node) bool {
		from, _ := get(item, "from")
		return from != nil && from.Value == ref
	})
	return nil
}

func (s *session) addGraphInputBinding(op Op) *Error {
	var q *QueryResult
	if parseGraphInputRef(op.From) {
		q = s.canConnect(op.From, op.To)
	} else {
		q = &QueryResult{Code: CodeInvalidOp, Message: fmt.Sprintf("%q is not a graph input reference (inputs.NAME)", op.From)}
	}
	if !q.OK {
		return &Error{Code: CodeCannotConnect, Message: q.Message, OpIndex: -1, Edge: edgeLabel(op.From, op.To), Suggestion: "the validator refused this binding (" + q.Code + "); check the types and that the input has no other provider"}
	}
	// typed prediction
	s.want.GraphInputBindings = append(s.want.GraphInputBindings, graph.GraphInputBindingDefinition{From: graph.GraphInputReference(op.From), To: graph.NodeInputReference(op.To)})
	// tree
	seq, err := s.topSequence("graph_input_bindings")
	if err != nil {
		return err
	}
	// New items look like the existing ones: flow style if the first item is flow.
	flow := len(seq.Content) > 0 && seq.Content[0].Style&yaml.FlowStyle != 0
	if len(seq.Content) == 0 {
		seq.Style &^= yaml.FlowStyle // an empty `graph_input_bindings: []` becomes a block list
	}
	seq.Content = append(seq.Content, mapNode(flow, str("from"), str(op.From), str("to"), str(op.To)))
	return nil
}

func (s *session) removeGraphInputBinding(op Op) *Error {
	var removed bool
	s.want.GraphInputBindings = filter(s.want.GraphInputBindings, func(b graph.GraphInputBindingDefinition) bool {
		match := string(b.From) == op.From && string(b.To) == op.To
		removed = removed || match
		return !match
	})
	if !removed {
		return &Error{Code: CodeBindingNotFound, Message: fmt.Sprintf("there is no graph input binding %s", edgeLabel(op.From, op.To)), OpIndex: -1, Edge: edgeLabel(op.From, op.To)}
	}
	s.dropSeqItems("graph_input_bindings", func(item *yaml.Node) bool {
		from, _ := get(item, "from")
		to, _ := get(item, "to")
		return from != nil && to != nil && from.Value == op.From && to.Value == op.To
	})
	return nil
}

func (s *session) setGraphOutput(op Op) *Error {
	path := "outputs." + op.ID
	if !graph.ValidIdentifier(op.ID) {
		return graphDataError(CodeGraphOutputInvalid, path, fmt.Sprintf("%q is not a valid graph output name", op.ID), identifierHint)
	}
	if e := s.checkOutputSource(path, op.From); e != nil {
		return e
	}
	// typed prediction
	if s.want.Outputs == nil {
		s.want.Outputs = map[string]graph.OutputDefinition{}
	}
	s.want.Outputs[op.ID] = graph.OutputDefinition{From: graph.NodeOutputReference(op.From)}
	// tree
	m, err := s.topMapping("outputs")
	if err != nil {
		return err
	}
	if value, _ := get(m, op.ID); value != nil {
		if value.Kind != yaml.MappingNode {
			return graphDataError(CodeParse, path, "the graph output is not a mapping in the file", "fix the YAML text")
		}
		if from, _ := get(value, "from"); from != nil {
			from.Kind, from.Tag, from.Value = yaml.ScalarNode, "!!str", op.From
		} else {
			value.Content = append(value.Content, str("from"), str(op.From))
		}
		return nil
	}
	flow := len(m.Content) > 0 && m.Content[1].Style&yaml.FlowStyle != 0
	m.Content = append(m.Content, str(op.ID), mapNode(flow, str("from"), str(op.From)))
	return nil
}

func (s *session) checkOutputSource(path, from string) *Error {
	if !parseRef(from, "outputs") {
		return graphDataError(CodeGraphOutputInvalid, path, fmt.Sprintf("%q is not a node output reference (NODE.outputs.PORT)", from), "name an output of a node")
	}
	node, port, _ := splitRef(from)
	n, ok := s.want.Nodes[node]
	if !ok {
		return graphDataError(CodeGraphOutputInvalid, path, fmt.Sprintf("node %q does not exist", node), "name an output of an existing node")
	}
	if _, ok := n.Outputs[port]; !ok {
		return graphDataError(CodeGraphOutputInvalid, path, fmt.Sprintf("node %q has no output %q", node, port), "name an output the node declares")
	}
	return nil
}

func (s *session) removeGraphOutput(op Op) *Error {
	if _, ok := s.want.Outputs[op.ID]; !ok {
		return graphDataError(CodeGraphOutputInvalid, "outputs."+op.ID, fmt.Sprintf("graph output %q does not exist", op.ID), "")
	}
	delete(s.want.Outputs, op.ID)
	if m, _ := get(s.top, "outputs"); m != nil {
		remove(m, op.ID)
	}
	return nil
}

// splitRef returns the node id and port of NODE.SECTION.PORT.
func splitRef(ref string) (node, port string, ok bool) {
	parts := strings.Split(ref, ".")
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[0], parts[2], true
}

// listConnections answers, for one port, which counterpart ports it can be
// connected to. Each verdict is the validator's, judged against one baseline
// validation of the unedited definition.
func (s *session) listConnections(op Op) *QueryResult {
	base := connectionBaseline(s.want)
	if op.From != "" {
		return s.listTargets(base, op.From)
	}
	return s.listSources(base, op.To)
}

func refused(code, message string) *QueryResult { return &QueryResult{Code: code, Message: message} }

func (s *session) listTargets(base errorBaseline, from string) *QueryResult {
	switch {
	case parseGraphInputRef(from):
		if _, ok := s.want.Inputs[from[len("inputs."):]]; !ok {
			return refused(CodeGraphInputNotFound, fmt.Sprintf("graph input %q does not exist", from[len("inputs."):]))
		}
	case parseRef(from, "outputs"):
		node, port, _ := splitRef(from)
		n, ok := s.want.Nodes[node]
		if !ok {
			return refused(CodeNodeNotFound, fmt.Sprintf("node %q does not exist", node))
		}
		if _, ok := n.Outputs[port]; !ok {
			return refused(CodePortNotFound, fmt.Sprintf("output %q does not exist on node %q", port, node))
		}
	default:
		return refused(CodeInvalidOp, fmt.Sprintf("%q is not a node output reference (NODE.outputs.PORT) or a graph input reference (inputs.NAME)", from))
	}
	var cands []candidate
	for _, id := range sortedKeys(s.want.Nodes) {
		n := s.want.Nodes[id]
		for _, port := range sortedKeys(n.Inputs) {
			ref := id + ".inputs." + port
			cands = append(cands, candidate{ref: ref, port: n.Inputs[port], from: from, to: ref})
		}
	}
	return &QueryResult{OK: true, Options: s.judge(base, cands)}
}

func (s *session) listSources(base errorBaseline, to string) *QueryResult {
	if !parseRef(to, "inputs") {
		return refused(CodeInvalidOp, fmt.Sprintf("%q is not a node input reference (NODE.inputs.PORT)", to))
	}
	node, port, _ := splitRef(to)
	n, ok := s.want.Nodes[node]
	if !ok {
		return refused(CodeNodeNotFound, fmt.Sprintf("node %q does not exist", node))
	}
	if _, ok := n.Inputs[port]; !ok {
		return refused(CodePortNotFound, fmt.Sprintf("input %q does not exist on node %q", port, node))
	}
	var cands []candidate
	for _, id := range sortedKeys(s.want.Inputs) {
		in := s.want.Inputs[id]
		ref := graphInputRef(id)
		cands = append(cands, candidate{ref: ref, port: graph.PortDefinition(in), from: ref, to: to})
	}
	for _, id := range sortedKeys(s.want.Nodes) {
		outs := s.want.Nodes[id].Outputs
		for _, p := range sortedKeys(outs) {
			ref := id + ".outputs." + p
			cands = append(cands, candidate{ref: ref, port: outs[p], from: ref, to: to})
		}
	}
	return &QueryResult{OK: true, Options: s.judge(base, cands)}
}

// candidate is one counterpart port and the connection that would join it.
type candidate struct {
	ref      string
	port     graph.PortDefinition
	from, to string
}

// judge validates every candidate connection. The validations are independent
// reads of the definition, so they run on a bounded pool; each result goes to
// its own slot, which keeps the answer in the candidates' order.
func (s *session) judge(base errorBaseline, cands []candidate) []ConnectionOption {
	options := make([]ConnectionOption, len(cands))
	workers := min(runtime.GOMAXPROCS(0), len(cands))
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(cands) {
					return
				}
				c := cands[i]
				v := connectionVerdict(base, s.connectionAfter(c.from, c.to))
				options[i] = ConnectionOption{Ref: c.ref, Type: string(c.port.Type), Required: c.port.Required, OK: v.OK, Code: v.Code, Message: v.Message}
			}
		}()
	}
	wg.Wait()
	return options
}
