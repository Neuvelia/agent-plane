package graph

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
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
	Version SchemaVersion
}

func (e *UnsupportedSchemaVersionError) Error() string {
	return fmt.Sprintf("unsupported graph schema_version %d (supported: %d)", e.Version, SchemaVersionV0)
}

func (e *UnsupportedSchemaVersionError) Unwrap() error {
	return ErrUnsupportedSchemaVersion
}

// Parse decodes one YAML graph definition. It performs structural decoding
// only; graph semantics are validated by later features.
func Parse(data []byte) (*Definition, error) {
	var envelope struct {
		SchemaVersion yaml.Node `yaml:"schema_version"`
	}
	if err := decodeOneDocument(data, &envelope, false); err != nil {
		return nil, fmt.Errorf("parse graph definition: %w", err)
	}
	if envelope.SchemaVersion.Kind == 0 {
		return nil, fmt.Errorf("parse graph definition: %w", ErrMissingSchemaVersion)
	}
	if envelope.SchemaVersion.Kind != yaml.ScalarNode || envelope.SchemaVersion.ShortTag() != "!!int" {
		return nil, fmt.Errorf("parse graph definition: %w", ErrInvalidSchemaVersion)
	}

	var schemaVersion SchemaVersion
	if err := envelope.SchemaVersion.Decode(&schemaVersion); err != nil {
		return nil, fmt.Errorf("parse graph definition: %w: %w", ErrInvalidSchemaVersion, err)
	}
	if schemaVersion != SchemaVersionV0 {
		return nil, &UnsupportedSchemaVersionError{Version: schemaVersion}
	}

	var definition Definition
	if err := decodeOneDocument(data, &definition, true); err != nil {
		return nil, fmt.Errorf("parse graph definition: %w", err)
	}
	// Inspect presence separately: YAML null otherwise erases optional pointers.
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}

	resolve := func(n *yaml.Node) *yaml.Node {
		for n != nil && n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		return n
	}
	var field func(*yaml.Node, string) *yaml.Node
	field = func(n *yaml.Node, key string) *yaml.Node {
		n = resolve(n)
		if n != nil && n.Kind == yaml.MappingNode {
			for i := 0; i < len(n.Content); i += 2 {
				if n.Content[i].Value == key {
					return resolve(n.Content[i+1])
				}
			}
			// YAML merge keys participate in presence and budget validation too.
			for i := 0; i < len(n.Content); i += 2 {
				if n.Content[i].Value == "<<" {
					merged := resolve(n.Content[i+1])
					if merged.Kind == yaml.SequenceNode {
						for _, entry := range merged.Content {
							if found := field(entry, key); found != nil {
								return found
							}
						}
					} else if found := field(merged, key); found != nil {
						return found
					}
				}
			}
		}
		return nil
	}
	if len(document.Content) > 0 {
		if units := field(document.Content[0], "units"); units != nil {
			if units.Kind != yaml.SequenceNode || len(units.Content) == 0 {
				return nil, errors.New("parse graph definition: units must be a nonempty list of loop Agent node IDs")
			}
			for _, unit := range units.Content {
				unit = resolve(unit)
				if unit.Kind != yaml.ScalarNode || unit.ShortTag() != "!!str" {
					return nil, errors.New("parse graph definition: unit IDs must be strings")
				}
			}
		}
		nodes := field(document.Content[0], "nodes")
		if nodes != nil && nodes.Kind == yaml.MappingNode {
			for _, id := range sortedNodeIDs(definition.Nodes) {
				loop := field(field(nodes, id), "loop")
				if loop == nil {
					continue
				}
				if loop.ShortTag() == "!!null" {
					return nil, errors.New("parse graph definition: loop may not be null")
				}
				if budget := field(loop, "max_attempts"); budget != nil {
					var n int
					if budget.Kind != yaml.ScalarNode || budget.ShortTag() != "!!int" || budget.Decode(&n) != nil || n < loopMinAttempts || n > loopMaxAttempts {
						return nil, fmt.Errorf("parse graph definition: max_attempts must be an integer from %d to %d", loopMinAttempts, loopMaxAttempts)
					}
				}
			}
		}
	}
	return &definition, nil
}

// Marshal encodes a supported graph definition as YAML. It does not validate
// graph semantics.
func Marshal(definition *Definition) ([]byte, error) {
	if definition == nil {
		return nil, errors.New("marshal graph definition: definition is nil")
	}
	if definition.SchemaVersion != SchemaVersionV0 {
		return nil, &UnsupportedSchemaVersionError{Version: definition.SchemaVersion}
	}

	data, err := yaml.Marshal(definition)
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
