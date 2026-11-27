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

// validation is a graph validator: graph.Validate, or graph.ValidateDataflow
// when the compared definitions differ only in edges and graph input bindings.
type validation func(*graph.Definition) graph.ValidationResult

// errorBaseline holds the blocking diagnostics of a definition, so many
// candidate edits can each be judged against one validation of the original.
type errorBaseline struct {
	validate validation
	seen     map[string]int
}

func baselineWith(validate validation, before *graph.Definition) errorBaseline {
	seen := map[string]int{}
	for _, d := range validate(before).Diagnostics {
		if d.Severity == graph.SeverityError {
			seen[d.Code+"|"+d.Message]++
		}
	}
	return errorBaseline{validate: validate, seen: seen}
}

// connectionBaseline is the baseline for judging connections. A connection
// changes only edges or graph input bindings, so only graph.ValidateDataflow's
// passes can report a new error for it; every other pass reports the same
// diagnostics before and after, under other codes. Skipping them keeps each
// verdict the validator's while list_connections stays fast (issue #192).
func connectionBaseline(before *graph.Definition) errorBaseline {
	return baselineWith(graph.ValidateDataflow, before)
}

// newErrors returns the blocking diagnostics of after that the baseline does
// not have, by the baseline's own validator.
func (b errorBaseline) newErrors(after *graph.Definition) []graph.Diagnostic {
	left := make(map[string]int, len(b.seen))
	for k, v := range b.seen {
		left[k] = v
	}
	var added []graph.Diagnostic
	for _, d := range b.validate(after).Diagnostics {
		if d.Severity != graph.SeverityError {
			continue
		}
		key := d.Code + "|" + d.Message
		if left[key] > 0 {
			left[key]--
			continue
		}
		added = append(added, d)
	}
	return added
}

// newErrors returns the blocking diagnostics present after an edit that were not
// present before, by the full validator: the edit may change any part of the
// definition. can_connect has no rules of its own: it asks the validator.
func newErrors(before, after *graph.Definition) []graph.Diagnostic {
	return baselineWith(graph.Validate, before).newErrors(after)
}

// parseGraphInputRef reports whether ref is inputs.NAME.
func parseGraphInputRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, "inputs.")
	return ok && graph.ValidIdentifier(name)
}

// connectionAfter is the definition with from connected to to: an edge for a
// node output, a graph input binding for inputs.NAME.
func (s *session) connectionAfter(from, to string) *graph.Definition {
	after := *s.want
	if parseGraphInputRef(from) {
		after.GraphInputBindings = append(append([]graph.GraphInputBindingDefinition(nil), s.want.GraphInputBindings...), graph.GraphInputBindingDefinition{From: graph.GraphInputReference(from), To: graph.NodeInputReference(to)})
		return &after
	}
	after.Edges = append(append([]graph.EdgeDefinition(nil), s.want.Edges...), graph.EdgeDefinition{From: graph.NodeOutputReference(from), To: graph.NodeInputReference(to)})
	return &after
}

func connectionVerdict(base errorBaseline, after *graph.Definition) *QueryResult {
	if errs := base.newErrors(after); len(errs) > 0 {
		return &QueryResult{Code: errs[0].Code, Message: errs[0].Message}
	}
	return &QueryResult{OK: true}
}

func (s *session) canConnect(from, to string) *QueryResult {
	if !parseRef(from, "outputs") && !parseGraphInputRef(from) {
		return &QueryResult{Code: CodeInvalidOp, Message: fmt.Sprintf("%q is not a node output reference (NODE.outputs.PORT) or a graph input reference (inputs.NAME)", from)}
	}
	if !parseRef(to, "inputs") {
		return &QueryResult{Code: CodeInvalidOp, Message: fmt.Sprintf("%q is not a node input reference (NODE.inputs.PORT)", to)}
	}
	return connectionVerdict(connectionBaseline(s.want), s.connectionAfter(from, to))
}

func (s *session) addEdge(op Op) *Error {
	var q *QueryResult
	if parseRef(op.From, "outputs") {
		q = s.canConnect(op.From, op.To)
	} else {
		q = &QueryResult{Code: CodeInvalidOp, Message: fmt.Sprintf("%q is not a node output reference (NODE.outputs.PORT)", op.From)}
	}
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
