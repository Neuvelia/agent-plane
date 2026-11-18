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
	Units              []string
	SchemaVersion      SchemaVersion
	ID                 string
	Name               string
	Metadata           Metadata
	Inputs             map[string]InputDefinition
	Nodes              map[string]NodeDefinition
	Edges              []EdgeDefinition
	GraphInputBindings []GraphInputBindingDefinition
	Outputs            map[string]OutputDefinition
}

// InputDefinition declares one input accepted by the graph.
type InputDefinition struct {
	Type     ValueType
	Required bool
}

// NodeDefinition declares one bounded unit of work. Its stable ID is the key
// under Definition.Nodes.
type NodeDefinition struct {
	Type    string
	Loop    *LoopDefinition
	Inputs  map[string]PortDefinition
	Config  NodeConfig
	Outputs map[string]PortDefinition
}

// PortDefinition declares a typed node input or output.
type PortDefinition struct {
	Type     ValueType
	Required bool
}

// EdgeDefinition maps a node output to a node input. Reference validation is
// deliberately deferred to F0.2.
type EdgeDefinition struct {
	From NodeOutputReference
	To   NodeInputReference
}

// GraphInputBindingDefinition maps a graph input to a node input. Reference
// validation is deliberately deferred to F0.2.
type GraphInputBindingDefinition struct {
	From GraphInputReference
	To   NodeInputReference
}

// OutputDefinition exposes a node output as a graph output.
type OutputDefinition struct {
	From NodeOutputReference
}

// LoopDefinition checks each candidate before publishing any producer outputs.
type LoopDefinition struct {
	MaxAttempts int
	Gate        LoopGateDefinition
	Bindings    []LoopBinding
}
type LoopGateDefinition struct {
	Inputs map[string]PortDefinition
	Config NodeConfig
}
type LoopBinding struct {
	From string
	To   string
}

func (l *LoopDefinition) Attempts() int {
	if l.MaxAttempts == 0 {
		return loopDefaultAttempts
	}
	return l.MaxAttempts
}
func (l *LoopDefinition) GateNode() NodeDefinition {
	return NodeDefinition{Type: "gate", Inputs: l.Gate.Inputs, Config: l.Gate.Config}
}
