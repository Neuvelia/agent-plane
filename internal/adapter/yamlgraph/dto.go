// Package yamlgraph reads and writes graph definitions as YAML (schema v0). It
// owns the on-disk spelling of the schema; the domain graph package stays free
// of any serialization format.
package yamlgraph

import (
	"strings"

	"agentplane/internal/domain/graph"
)

// The DTOs mirror the domain graph types and carry the YAML field names. They
// are the only place the on-disk spelling of schema v0 is declared (R5); the
// domain types stay free of transport tags. A field added to the schema must
// be added to both a DTO and its mapping, and the round-trip tests fail if
// one is forgotten.

type definitionDTO struct {
	Units              []string               `yaml:"units,omitempty"`
	SchemaVersion      graph.SchemaVersion    `yaml:"schema_version"`
	ID                 string                 `yaml:"id"`
	Name               string                 `yaml:"name,omitempty"`
	Metadata           graph.Metadata         `yaml:"metadata,omitempty"`
	Inputs             map[string]inputDTO    `yaml:"inputs"`
	Nodes              map[string]nodeDTO     `yaml:"nodes"`
	Edges              []edgeDTO              `yaml:"edges"`
	GraphInputBindings []graphInputBindingDTO `yaml:"graph_input_bindings,omitempty"`
	Outputs            map[string]outputDTO   `yaml:"outputs"`
	Limits             *limitsDTO             `yaml:"limits,omitempty"`
}

type limitsDTO struct {
	RunTimeoutSeconds *int `yaml:"run_timeout_seconds,omitempty"`
}

type inputDTO struct {
	Type     graph.ValueType `yaml:"type"`
	Required bool            `yaml:"required,omitempty"`
}

type nodeDTO struct {
	Type    string             `yaml:"type"`
	Loop    *loopDTO           `yaml:"loop,omitempty"`
	Inputs  map[string]portDTO `yaml:"inputs,omitempty"`
	Config  graph.NodeConfig   `yaml:"config,omitempty"`
	Outputs map[string]portDTO `yaml:"outputs,omitempty"`

	TimeoutSeconds *int `yaml:"timeout_seconds,omitempty"`
}

type portDTO struct {
	Type     graph.ValueType `yaml:"type"`
	Required bool            `yaml:"required,omitempty"`
}

type edgeDTO struct {
	From graph.NodeOutputReference `yaml:"from"`
	To   graph.NodeInputReference  `yaml:"to"`
}

type graphInputBindingDTO struct {
	From graph.GraphInputReference `yaml:"from"`
	To   graph.NodeInputReference  `yaml:"to"`
}

type outputDTO struct {
	From graph.NodeOutputReference `yaml:"from"`
}

type loopDTO struct {
	MaxAttempts int              `yaml:"max_attempts,omitempty"`
	Gate        loopGateDTO      `yaml:"gate"`
	Bindings    []loopBindingDTO `yaml:"bindings"`
}

type loopGateDTO struct {
	Inputs map[string]portDTO `yaml:"inputs,omitempty"`
	Config graph.NodeConfig   `yaml:"config,omitempty"`
}

type loopBindingDTO struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// mapValues converts a map and preserves nil-ness: YAML distinguishes an
// absent key (nil) from an empty mapping, and validation reports them
// differently.
func mapValues[K comparable, A, B any](in map[K]A, convert func(A) B) map[K]B {
	if in == nil {
		return nil
	}
	out := make(map[K]B, len(in))
	for key, value := range in {
		out[key] = convert(value)
	}
	return out
}

// mapSlice converts a slice and preserves nil-ness (see mapValues).
func mapSlice[A, B any](in []A, convert func(A) B) []B {
	if in == nil {
		return nil
	}
	out := make([]B, len(in))
	for i, value := range in {
		out[i] = convert(value)
	}
	return out
}

func (d definitionDTO) toDomain() *graph.Definition {
	def := &graph.Definition{
		Units:         d.Units,
		SchemaVersion: d.SchemaVersion,
		ID:            d.ID,
		Name:          d.Name,
		Metadata:      d.Metadata,
		Inputs:        mapValues(d.Inputs, func(v inputDTO) graph.InputDefinition { return graph.InputDefinition(v) }),
		Nodes:         mapValues(d.Nodes, nodeDTO.toDomain),
		Edges:         mapSlice(d.Edges, func(v edgeDTO) graph.EdgeDefinition { return graph.EdgeDefinition(v) }),
		GraphInputBindings: mapSlice(d.GraphInputBindings, func(v graphInputBindingDTO) graph.GraphInputBindingDefinition {
			return graph.GraphInputBindingDefinition(v)
		}),
		Outputs: mapValues(d.Outputs, func(v outputDTO) graph.OutputDefinition { return graph.OutputDefinition(v) }),
	}
	if d.Limits != nil {
		def.Limits = &graph.LimitsDefinition{RunTimeoutSeconds: d.Limits.RunTimeoutSeconds}
	}
	return def
}

func (d nodeDTO) toDomain() graph.NodeDefinition {
	node := graph.NodeDefinition{
		Type:    d.Type,
		Inputs:  mapValues(d.Inputs, portToDomain),
		Config:  d.Config,
		Outputs: mapValues(d.Outputs, portToDomain),

		TimeoutSeconds: d.TimeoutSeconds,
	}
	if d.Loop != nil {
		node.Loop = &graph.LoopDefinition{
			MaxAttempts: d.Loop.MaxAttempts,
			Gate:        graph.LoopGateDefinition{Inputs: mapValues(d.Loop.Gate.Inputs, portToDomain), Config: d.Loop.Gate.Config},
			Bindings:    mapSlice(d.Loop.Bindings, func(v loopBindingDTO) graph.LoopBinding { return graph.LoopBinding(v) }),
		}
	}
	return node
}

func portToDomain(p portDTO) graph.PortDefinition { return graph.PortDefinition(p) }

func definitionToDTO(d *graph.Definition) definitionDTO {
	dto := definitionDTO{
		Units:         d.Units,
		SchemaVersion: d.SchemaVersion,
		ID:            d.ID,
		Name:          d.Name,
		Metadata:      d.Metadata,
		Inputs:        mapValues(d.Inputs, func(v graph.InputDefinition) inputDTO { return inputDTO(v) }),
		Nodes:         mapValues(d.Nodes, nodeToDTO),
		Edges:         mapSlice(d.Edges, func(v graph.EdgeDefinition) edgeDTO { return edgeDTO(v) }),
		GraphInputBindings: mapSlice(d.GraphInputBindings, func(v graph.GraphInputBindingDefinition) graphInputBindingDTO {
			return graphInputBindingDTO(v)
		}),
		Outputs: mapValues(d.Outputs, func(v graph.OutputDefinition) outputDTO { return outputDTO(v) }),
	}
	// An empty limits block is the same graph as none, so it is not written.
	if d.Limits != nil && d.Limits.RunTimeoutSeconds != nil {
		dto.Limits = &limitsDTO{RunTimeoutSeconds: d.Limits.RunTimeoutSeconds}
	}
	return dto
}

func nodeToDTO(n graph.NodeDefinition) nodeDTO {
	node := nodeDTO{
		Type:    n.Type,
		Inputs:  mapValues(n.Inputs, portToDTO),
		Config:  n.Config,
		Outputs: mapValues(n.Outputs, portToDTO),

		TimeoutSeconds: n.TimeoutSeconds,
	}
	if n.Loop != nil {
		node.Loop = &loopDTO{
			MaxAttempts: n.Loop.MaxAttempts,
			Gate:        loopGateDTO{Inputs: mapValues(n.Loop.Gate.Inputs, portToDTO), Config: n.Loop.Gate.Config},
			Bindings:    mapSlice(n.Loop.Bindings, func(v graph.LoopBinding) loopBindingDTO { return loopBindingDTO(v) }),
		}
	}
	return node
}

func portToDTO(p graph.PortDefinition) portDTO { return portDTO(p) }

// domainTypeNames maps DTO type names, which the YAML decoder prints in its
// errors, back to the domain names users have always seen. Parse error text is
// part of the CLI/API contract (and the editor derives line numbers from it).
var domainTypeNames = strings.NewReplacer(
	"yamlgraph.definitionDTO", "graph.Definition",
	"yamlgraph.inputDTO", "graph.InputDefinition",
	"yamlgraph.nodeDTO", "graph.NodeDefinition",
	"yamlgraph.portDTO", "graph.PortDefinition",
	"yamlgraph.edgeDTO", "graph.EdgeDefinition",
	"yamlgraph.graphInputBindingDTO", "graph.GraphInputBindingDefinition",
	"yamlgraph.outputDTO", "graph.OutputDefinition",
	"yamlgraph.limitsDTO", "graph.LimitsDefinition",
	"yamlgraph.loopDTO", "graph.LoopDefinition",
	"yamlgraph.loopGateDTO", "graph.LoopGateDefinition",
	"yamlgraph.loopBindingDTO", "graph.LoopBinding",
)

type renamedError struct {
	message string
	cause   error
}

func (e *renamedError) Error() string { return e.message }
func (e *renamedError) Unwrap() error { return e.cause }

func renameDTOTypes(err error) error {
	if err == nil {
		return nil
	}
	renamed := domainTypeNames.Replace(err.Error())
	if renamed == err.Error() {
		return err
	}
	return &renamedError{message: renamed, cause: err}
}
