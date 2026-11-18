package api

import (
	"fmt"

	"agentplane/internal/adapter/yamlgraph/edit"
	"agentplane/internal/domain/graph"
)

// The document DTO is the editor's view of a graph definition. It has explicit
// JSON tags and is separate from the runtime types, so a change in the engine's
// structs can never silently change the wire format (ADR-0021).

// diagnosticDTO is a diagnostic as the editor receives it: the engine's
// diagnostic unchanged, plus the 1-based source line when it can be resolved.
// The line is an API field only; graph.Diagnostic and the CLI JSON do not carry it.
type diagnosticDTO struct {
	graph.Diagnostic
	Line int `json:"line,omitempty"`
}

func withLines(text []byte, diags []graph.Diagnostic) []diagnosticDTO {
	lines := edit.DiagnosticLines(text, diags)
	out := make([]diagnosticDTO, len(diags))
	for i, d := range diags {
		out[i] = diagnosticDTO{Diagnostic: d, Line: lines[i]}
	}
	return out
}

type portDTO struct {
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

type edgeDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type loopBindingDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type loopDTO struct {
	MaxAttempts int              `json:"max_attempts,omitempty"`
	Gate        loopGateDTO      `json:"gate"`
	Bindings    []loopBindingDTO `json:"bindings"`
}

type loopGateDTO struct {
	Inputs map[string]portDTO `json:"inputs,omitempty"`
	Config map[string]any     `json:"config,omitempty"`
}

type nodeDTO struct {
	Type    string             `json:"type"`
	Loop    *loopDTO           `json:"loop,omitempty"`
	Inputs  map[string]portDTO `json:"inputs,omitempty"`
	Config  map[string]any     `json:"config,omitempty"`
	Outputs map[string]portDTO `json:"outputs,omitempty"`

	TimeoutSeconds *int `json:"timeout_seconds,omitempty"`
}

type limitsDTO struct {
	RunTimeoutSeconds *int `json:"run_timeout_seconds,omitempty"`
}

type graphInputDTO struct {
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

type outputDTO struct {
	From string `json:"from"`
}

type definitionDTO struct {
	SchemaVersion      int                      `json:"schema_version"`
	ID                 string                   `json:"id"`
	Name               string                   `json:"name,omitempty"`
	Metadata           map[string]any           `json:"metadata,omitempty"`
	Units              []string                 `json:"units,omitempty"`
	Inputs             map[string]graphInputDTO `json:"inputs"`
	Nodes              map[string]nodeDTO       `json:"nodes"`
	Edges              []edgeDTO                `json:"edges"`
	GraphInputBindings []edgeDTO                `json:"graph_input_bindings,omitempty"`
	Outputs            map[string]outputDTO     `json:"outputs"`
	Limits             *limitsDTO               `json:"limits,omitempty"`
}

func toDefinitionDTO(d *graph.Definition) *definitionDTO {
	out := &definitionDTO{
		SchemaVersion: int(d.SchemaVersion),
		ID:            d.ID,
		Name:          d.Name,
		Metadata:      safeMap(d.Metadata),
		Units:         append([]string(nil), d.Units...),
		Inputs:        map[string]graphInputDTO{},
		Nodes:         map[string]nodeDTO{},
		Edges:         []edgeDTO{},
		Outputs:       map[string]outputDTO{},
	}
	if d.Limits != nil && d.Limits.RunTimeoutSeconds != nil {
		out.Limits = &limitsDTO{RunTimeoutSeconds: d.Limits.RunTimeoutSeconds}
	}
	for k, v := range d.Inputs {
		out.Inputs[k] = graphInputDTO{Type: string(v.Type), Required: v.Required}
	}
	for id, n := range d.Nodes {
		node := nodeDTO{Type: n.Type, Inputs: ports(n.Inputs), Config: safeMap(n.Config), Outputs: ports(n.Outputs), TimeoutSeconds: n.TimeoutSeconds}
		if n.Loop != nil {
			l := &loopDTO{MaxAttempts: n.Loop.MaxAttempts, Gate: loopGateDTO{Inputs: ports(n.Loop.Gate.Inputs), Config: safeMap(n.Loop.Gate.Config)}, Bindings: []loopBindingDTO{}}
			for _, b := range n.Loop.Bindings {
				l.Bindings = append(l.Bindings, loopBindingDTO{From: b.From, To: b.To})
			}
			node.Loop = l
		}
		out.Nodes[id] = node
	}
	for _, e := range d.Edges {
		out.Edges = append(out.Edges, edgeDTO{From: string(e.From), To: string(e.To)})
	}
	for _, b := range d.GraphInputBindings {
		out.GraphInputBindings = append(out.GraphInputBindings, edgeDTO{From: string(b.From), To: string(b.To)})
	}
	for k, v := range d.Outputs {
		out.Outputs[k] = outputDTO{From: string(v.From)}
	}
	return out
}

func ports(in map[string]graph.PortDefinition) map[string]portDTO {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]portDTO, len(in))
	for k, v := range in {
		out[k] = portDTO{Type: string(v.Type), Required: v.Required}
	}
	return out
}

// safeMap converts a top-level mapping; an empty one is omitted (omitempty).
func safeMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	return convertMap(in)
}

// convertMap keeps empty nested mappings as {} (they are data, not absence).
func convertMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = jsonSafe(v)
	}
	return out
}

// jsonSafe makes decoded YAML encodable as JSON: nested mappings come back as
// map[string]any, graph.Metadata or graph.NodeConfig (named types) or, for
// non-string keys, map[any]any.
func jsonSafe(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return convertMap(x)
	case graph.Metadata:
		return convertMap(x)
	case graph.NodeConfig:
		return convertMap(x)
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = jsonSafe(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = jsonSafe(val)
		}
		return out
	default:
		return v
	}
}
