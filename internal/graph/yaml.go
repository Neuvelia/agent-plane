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
