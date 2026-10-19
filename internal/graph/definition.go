package graph

// SchemaVersion identifies the graph definition contract used by a document.
type SchemaVersion int

// SchemaVersionV0 is the graph definition schema introduced by F0.1.
const SchemaVersionV0 SchemaVersion = 0

// ValueType names a value that may cross a graph or node port.
type ValueType string

const (
	ValueTypeString      ValueType = "string"
	ValueTypeNumber      ValueType = "number"
	ValueTypeBoolean     ValueType = "boolean"
	ValueTypeJSON        ValueType = "json"
	ValueTypeArtifactRef ValueType = "artifact_ref"
)

// Metadata holds non-semantic graph information. Future semantic hashing must
// exclude this field.
type Metadata map[string]any

// NodeConfig holds configuration interpreted by a node-type implementation.
// The graph package preserves this data but does not interpret it.
type NodeConfig map[string]any

type NodeOutputReference string
type NodeInputReference string
type GraphInputReference string

// Definition is the typed representation of a graph definition document.
type Definition struct {
	SchemaVersion      SchemaVersion                 `yaml:"schema_version"`
	ID                 string                        `yaml:"id"`
	Name               string                        `yaml:"name,omitempty"`
	Metadata           Metadata                      `yaml:"metadata,omitempty"`
	Inputs             map[string]InputDefinition    `yaml:"inputs"`
	Nodes              map[string]NodeDefinition     `yaml:"nodes"`
	Edges              []EdgeDefinition              `yaml:"edges"`
	GraphInputBindings []GraphInputBindingDefinition `yaml:"graph_input_bindings,omitempty"`
	Outputs            map[string]OutputDefinition   `yaml:"outputs"`
}

// InputDefinition declares one input accepted by the graph.
type InputDefinition struct {
	Type     ValueType `yaml:"type"`
	Required bool      `yaml:"required,omitempty"`
}

// NodeDefinition declares one bounded unit of work. Its stable ID is the key
// under Definition.Nodes.
type NodeDefinition struct {
	Type    string                    `yaml:"type"`
	Loop    *LoopDefinition           `yaml:"loop,omitempty"`
	Inputs  map[string]PortDefinition `yaml:"inputs,omitempty"`
	Config  NodeConfig                `yaml:"config,omitempty"`
	Outputs map[string]PortDefinition `yaml:"outputs,omitempty"`
}

// PortDefinition declares a typed node input or output.
type PortDefinition struct {
	Type     ValueType `yaml:"type"`
	Required bool      `yaml:"required,omitempty"`
}

// EdgeDefinition maps a node output to a node input. Reference validation is
// deliberately deferred to F0.2.
type EdgeDefinition struct {
	From NodeOutputReference `yaml:"from"`
	To   NodeInputReference  `yaml:"to"`
}

// GraphInputBindingDefinition maps a graph input to a node input. Reference
// validation is deliberately deferred to F0.2.
type GraphInputBindingDefinition struct {
	From GraphInputReference `yaml:"from"`
	To   NodeInputReference  `yaml:"to"`
}

// OutputDefinition exposes a node output as a graph output.
type OutputDefinition struct {
	From NodeOutputReference `yaml:"from"`
}

// LoopDefinition checks each candidate before publishing any producer outputs.
type LoopDefinition struct {
	MaxAttempts int                `yaml:"max_attempts,omitempty"`
	Gate        LoopGateDefinition `yaml:"gate"`
	Bindings    []LoopBinding      `yaml:"bindings"`
}
type LoopGateDefinition struct {
	Inputs map[string]PortDefinition `yaml:"inputs,omitempty"`
	Config NodeConfig                `yaml:"config,omitempty"`
}
type LoopBinding struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

func (l *LoopDefinition) Attempts() int {
	if l.MaxAttempts == 0 {
		return 3
	}
	return l.MaxAttempts
}
func (l *LoopDefinition) GateNode() NodeDefinition {
	return NodeDefinition{Type: "gate", Inputs: l.Gate.Inputs, Config: l.Gate.Config}
}
