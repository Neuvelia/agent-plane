package edit

import (
	"fmt"
	"strings"

	"agentplane/internal/domain/graph"

	"go.yaml.in/yaml/v3"
)

// parseRef splits NODE.outputs.PORT or NODE.inputs.PORT. Identifiers contain no dots.
func parseRef(ref, kind string) bool {
	parts := strings.Split(ref, ".")
	return len(parts) == 3 && parts[1] == kind && graph.ValidIdentifier(parts[0]) && graph.ValidIdentifier(parts[2])
}

func edgeLabel(from, to string) string { return from + " -> " + to }

// newErrors returns the blocking diagnostics present after an edit that were not
// present before. can_connect has no rules of its own: it asks the validator.
func newErrors(before, after *graph.Definition) []graph.Diagnostic {
	seen := map[string]int{}
	for _, d := range graph.Validate(before).Diagnostics {
		if d.Severity == graph.SeverityError {
			seen[d.Code+"|"+d.Message]++
		}
	}
	var added []graph.Diagnostic
	for _, d := range graph.Validate(after).Diagnostics {
		if d.Severity != graph.SeverityError {
			continue
		}
		key := d.Code + "|" + d.Message
		if seen[key] > 0 {
			seen[key]--
			continue
		}
		added = append(added, d)
	}
	return added
}

func (s *session) canConnect(from, to string) *QueryResult {
	if !parseRef(from, "outputs") {
		return &QueryResult{Code: CodeInvalidOp, Message: fmt.Sprintf("%q is not a node output reference (NODE.outputs.PORT)", from)}
	}
	if !parseRef(to, "inputs") {
		return &QueryResult{Code: CodeInvalidOp, Message: fmt.Sprintf("%q is not a node input reference (NODE.inputs.PORT)", to)}
	}
	after := *s.want
	after.Edges = append(append([]graph.EdgeDefinition(nil), s.want.Edges...), graph.EdgeDefinition{From: graph.NodeOutputReference(from), To: graph.NodeInputReference(to)})
	if errs := newErrors(s.want, &after); len(errs) > 0 {
		return &QueryResult{Code: errs[0].Code, Message: errs[0].Message}
	}
	return &QueryResult{OK: true}
}

func (s *session) addEdge(op Op) *Error {
	q := s.canConnect(op.From, op.To)
	if !q.OK {
		return &Error{Code: CodeCannotConnect, Message: q.Message, OpIndex: -1, Edge: edgeLabel(op.From, op.To), Suggestion: "the validator refused this edge (" + q.Code + "); check the port types and that the input has no other provider"}
	}
	// typed prediction
	s.want.Edges = append(s.want.Edges, graph.EdgeDefinition{From: graph.NodeOutputReference(op.From), To: graph.NodeInputReference(op.To)})
	// tree
	edges, i := get(s.top, "edges")
	if i < 0 {
		edges = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		insertTop(s.top, "edges", edges)
	}
	if edges.Kind != yaml.SequenceNode {
		return &Error{Code: CodeParse, Message: "edges is not a list", OpIndex: -1, Suggestion: "fix the YAML text"}
	}
	// New items look like the existing ones: flow style if the first item is flow.
	flow := len(edges.Content) > 0 && edges.Content[0].Style&yaml.FlowStyle != 0
	if len(edges.Content) == 0 {
		edges.Style &^= yaml.FlowStyle // an empty `edges: []` becomes a block list
	}
	edges.Content = append(edges.Content, mapNode(flow, str("from"), str(op.From), str("to"), str(op.To)))
	return nil
}

func (s *session) removeEdge(op Op) *Error {
	var removed bool
	s.want.Edges = filter(s.want.Edges, func(e graph.EdgeDefinition) bool {
		match := string(e.From) == op.From && string(e.To) == op.To
		removed = removed || match
		return !match
	})
	if !removed {
		return &Error{Code: CodeEdgeNotFound, Message: fmt.Sprintf("there is no edge %s", edgeLabel(op.From, op.To)), OpIndex: -1, Edge: edgeLabel(op.From, op.To)}
	}
	s.dropSeqItems("edges", func(item *yaml.Node) bool {
		from, _ := get(item, "from")
		to, _ := get(item, "to")
		return from != nil && to != nil && from.Value == op.From && to.Value == op.To
	})
	return nil
}
