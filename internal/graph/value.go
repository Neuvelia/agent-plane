package graph

import (
	"encoding/json"
	"errors"
	"strings"
)

// Value is one typed value crossing a declared graph binding. Its payload is
// private so callers cannot change the kind after construction.
type Value struct {
	kind        ValueType
	text        string
	number      json.Number
	boolean     bool
	jsonValue   json.RawMessage
	artifactRef string
}

// NewStringValue preserves a string without JSON encoding it.
func NewStringValue(value string) Value {
	return Value{kind: ValueTypeString, text: value}
}

// NewNumberValue accepts only JSON number syntax and preserves its decimal text.
func NewNumberValue(value json.Number) (Value, error) {
	if value == "" || strings.TrimSpace(string(value)) != string(value) || !json.Valid([]byte(value)) || (value[0] != '-' && (value[0] < '0' || value[0] > '9')) {
		return Value{}, errors.New("invalid JSON number")
	}
	return Value{kind: ValueTypeNumber, number: value}, nil
}

// NewBooleanValue preserves a boolean, including false as a present value.
func NewBooleanValue(value bool) Value {
	return Value{kind: ValueTypeBoolean, boolean: value}
}

// NewJSONValue accepts valid JSON and copies the supplied bytes.
func NewJSONValue(value json.RawMessage) (Value, error) {
	if !json.Valid(value) {
		return Value{}, errors.New("invalid JSON value")
	}
	return Value{kind: ValueTypeJSON, jsonValue: append(json.RawMessage(nil), value...)}, nil
}

// NewArtifactRefValue stores a nonempty opaque handle without reading it.
func NewArtifactRefValue(value string) (Value, error) {
	if value == "" {
		return Value{}, errors.New("artifact reference is empty")
	}
	return Value{kind: ValueTypeArtifactRef, artifactRef: value}, nil
}

// Type returns the declared kind of the value; the zero Value has no kind.
func (v Value) Type() ValueType { return v.kind }

// AsString returns the string payload only when the value is a string.
func (v Value) AsString() (string, bool) { return v.text, v.kind == ValueTypeString }

// AsNumber returns the number payload only when the value is a number.
func (v Value) AsNumber() (json.Number, bool) { return v.number, v.kind == ValueTypeNumber }

// AsBoolean returns the boolean payload only when the value is a boolean.
func (v Value) AsBoolean() (bool, bool) { return v.boolean, v.kind == ValueTypeBoolean }

// AsJSON returns a copy of the JSON payload only when the value is JSON.
func (v Value) AsJSON() (json.RawMessage, bool) {
	if v.kind != ValueTypeJSON {
		return nil, false
	}
	return append(json.RawMessage(nil), v.jsonValue...), true
}

// AsArtifactRef returns the opaque handle only for artifact references.
func (v Value) AsArtifactRef() (string, bool) { return v.artifactRef, v.kind == ValueTypeArtifactRef }

func (v Value) clone() Value {
	if v.kind == ValueTypeJSON {
		v.jsonValue = append(json.RawMessage(nil), v.jsonValue...)
	}
	return v
}
