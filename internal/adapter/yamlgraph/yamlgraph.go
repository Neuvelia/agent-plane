package yamlgraph

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"go.yaml.in/yaml/v3"

	"agentplane/internal/domain/graph"
)

var (
	// ErrMissingSchemaVersion identifies a graph document without the required
	// schema_version field.
	ErrMissingSchemaVersion = errors.New("graph definition is missing required schema_version")

	// ErrUnsupportedSchemaVersion identifies a graph document using a schema
	// version this package cannot decode.
	ErrUnsupportedSchemaVersion = errors.New("unsupported graph schema_version")

	// ErrInvalidSchemaVersion identifies a schema_version value that is not a
	// YAML integer.
	ErrInvalidSchemaVersion = errors.New("graph schema_version must be an integer")

	// ErrMultipleDocuments identifies input containing more than one YAML
	// document. One graph definition must be exactly one YAML document.
	ErrMultipleDocuments = errors.New("graph definition must contain exactly one YAML document")
)

// UnsupportedSchemaVersionError reports the exact unsupported version.
type UnsupportedSchemaVersionError struct {
	Version graph.SchemaVersion
}

func (e *UnsupportedSchemaVersionError) Error() string {
	return fmt.Sprintf("unsupported graph schema_version %d (supported: %d)", e.Version, graph.SchemaVersionV0)
}

func (*UnsupportedSchemaVersionError) Unwrap() error {
	return ErrUnsupportedSchemaVersion
}

// Parse decodes one YAML graph definition. It performs structural decoding
// only; graph semantics are validated by later features.
func Parse(data []byte) (*graph.Definition, error) {
	if err := checkSchemaVersion(data); err != nil {
		return nil, err
	}
	var dto definitionDTO
	if err := decodeOneDocument(data, &dto, true); err != nil {
		return nil, fmt.Errorf("parse graph definition: %w", renameDTOTypes(err))
	}
	definition := dto.toDomain()
	if err := checkPresence(data, definition); err != nil {
		return nil, err
	}
	return definition, nil
}

// checkSchemaVersion reads only the schema_version field so that an unsupported
// document is rejected before its other fields are decoded strictly.
func checkSchemaVersion(data []byte) error {
	var envelope struct {
		SchemaVersion yaml.Node `yaml:"schema_version"`
	}
	if err := decodeOneDocument(data, &envelope, false); err != nil {
		return fmt.Errorf("parse graph definition: %w", err)
	}
	version := &envelope.SchemaVersion
	if version.Kind == 0 {
		return fmt.Errorf("parse graph definition: %w", ErrMissingSchemaVersion)
	}
	if version.Kind != yaml.ScalarNode || version.ShortTag() != "!!int" {
		return fmt.Errorf("parse graph definition: %w", ErrInvalidSchemaVersion)
	}
	var schemaVersion graph.SchemaVersion
	if err := version.Decode(&schemaVersion); err != nil {
		return fmt.Errorf("parse graph definition: %w: %w", ErrInvalidSchemaVersion, err)
	}
	if schemaVersion != graph.SchemaVersionV0 {
		return &UnsupportedSchemaVersionError{Version: schemaVersion}
	}
	return nil
}

// checkPresence inspects the YAML tree for facts the typed decode erases: YAML
// null otherwise turns optional values into their zero value.
func checkPresence(data []byte, definition *graph.Definition) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	if err := checkUnits(field(root, "units")); err != nil {
		return err
	}
	nodes := field(root, "nodes")
	if nodes == nil || nodes.Kind != yaml.MappingNode {
		return nil
	}
	for _, id := range sortedNodeIDs(definition.Nodes) {
		if err := checkLoop(field(field(nodes, id), "loop")); err != nil {
			return err
		}
	}
	return nil
}

func checkUnits(units *yaml.Node) error {
	if units == nil {
		return nil
	}
	if units.Kind != yaml.SequenceNode || len(units.Content) == 0 {
		return errors.New("parse graph definition: units must be a nonempty list of loop Agent node IDs")
	}
	for _, unit := range units.Content {
		unit = resolveAlias(unit)
		if unit.Kind != yaml.ScalarNode || unit.ShortTag() != "!!str" {
			return errors.New("parse graph definition: unit IDs must be strings")
		}
	}
	return nil
}

func checkLoop(loop *yaml.Node) error {
	if loop == nil {
		return nil
	}
	if loop.ShortTag() == "!!null" {
		return errors.New("parse graph definition: loop may not be null")
	}
	budget := field(loop, "max_attempts")
	if budget == nil {
		return nil
	}
	var attempts int
	if budget.Kind != yaml.ScalarNode || budget.ShortTag() != "!!int" || budget.Decode(&attempts) != nil || attempts < 1 || attempts > 10 {
		return errors.New("parse graph definition: max_attempts must be an integer from 1 to 10")
	}
	return nil
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// field returns the value under key in a mapping, following aliases and YAML
// merge keys, or nil when the key is absent.
func field(n *yaml.Node, key string) *yaml.Node {
	n = resolveAlias(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return resolveAlias(n.Content[i+1])
		}
	}
	return fieldFromMerges(n, key)
}

func fieldFromMerges(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value != "<<" {
			continue
		}
		merged := resolveAlias(n.Content[i+1])
		if merged.Kind != yaml.SequenceNode {
			if found := field(merged, key); found != nil {
				return found
			}
			continue
		}
		for _, entry := range merged.Content {
			if found := field(entry, key); found != nil {
				return found
			}
		}
	}
	return nil
}

// Marshal encodes a supported graph definition as YAML. It does not validate
// graph semantics.
func Marshal(definition *graph.Definition) ([]byte, error) {
	if definition == nil {
		return nil, errors.New("marshal graph definition: definition is nil")
	}
	if definition.SchemaVersion != graph.SchemaVersionV0 {
		return nil, &UnsupportedSchemaVersionError{Version: definition.SchemaVersion}
	}

	data, err := yaml.Marshal(definitionToDTO(definition))
	if err != nil {
		return nil, fmt.Errorf("marshal graph definition: %w", err)
	}
	return data, nil
}

func decodeOneDocument(data []byte, target any, knownFields bool) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(knownFields)
	if err := decoder.Decode(target); err != nil {
		return err
	}

	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return ErrMultipleDocuments
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func sortedNodeIDs(nodes map[string]graph.NodeDefinition) []string {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SemanticVersion is graph.SemanticVersion over the canonical YAML encoding.
func SemanticVersion(definition *graph.Definition) (string, error) {
	return graph.SemanticVersion(definition, Marshal)
}

// EncodeNode renders one node definition as a YAML node with the schema-v0
// field names, for tools that edit a document tree in place.
func EncodeNode(node graph.NodeDefinition) (*yaml.Node, error) {
	var out yaml.Node
	if err := out.Encode(nodeToDTO(node)); err != nil {
		return nil, fmt.Errorf("encode graph node: %w", err)
	}
	return &out, nil
}
