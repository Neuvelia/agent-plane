package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"agentplane/internal/graph"
)

func TestDecodeTypedInputsExactValues(t *testing.T) {
	input := json.RawMessage(`{"text":{"type":"string","value":"a\\nb"},"number":{"type":"number","value":"1.00"},"flag":{"type":"boolean","value":false},"data":{"type":"json","value":{"b":2,"a":1}},"ref":{"type":"artifact_ref","value":"handle:1"},"nil":{"type":"json","value":null}}`)
	values, err := decodeTypedInputs(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 6 {
		t.Fatalf("values = %#v", values)
	}
	if got, ok := values["text"].AsString(); !ok || got != `a\nb` {
		t.Fatalf("string = %q, %v", got, ok)
	}
	if got, ok := values["number"].AsNumber(); !ok || got != "1.00" {
		t.Fatalf("number = %q, %v", got, ok)
	}
	if got, ok := values["flag"].AsBoolean(); !ok || got {
		t.Fatalf("boolean = %v, %v", got, ok)
	}
	if got, ok := values["data"].AsJSON(); !ok || string(got) != `{"b":2,"a":1}` {
		t.Fatalf("json = %s, %v", got, ok)
	}
	if got, ok := values["nil"].AsJSON(); !ok || string(got) != "null" {
		t.Fatalf("json null = %s, %v", got, ok)
	}
	if got, ok := values["ref"].AsArtifactRef(); !ok || got != "handle:1" {
		t.Fatalf("artifact = %q, %v", got, ok)
	}
}

func TestDecodeTypedInputsRejectsInvalidShapeAndCoercion(t *testing.T) {
	cases := []string{
		`null`, `[]`, `{"x":{"type":"string","value":"a"},"x":{"type":"string","value":"b"}}`,
		`{"x":{"type":"string","type":"number","value":"1"}}`,
		`{"x":{"type":"string","value":"a","extra":true}}`,
		`{"x":{"type":"string"}}`, `{"x":{"value":"a"}}`,
		`{"x":null}`, `{"x":{"type":null,"value":"a"}}`,
		`{"x":{"type":"unsupported","value":"a"}}`,
		`{"x":{"type":"string","value":null}}`,
		`{"x":{"type":"string","value":1}}`,
		`{"x":{"type":"number","value":1}}`,
		`{"x":{"type":"number","value":"01"}}`,
		`{"x":{"type":"boolean","value":"false"}}`,
		`{"x":{"type":"artifact_ref","value":""}}`,
		`{"x":{"type":"json"}}`,
		`{"x":{"type":"string","value":"a"}} true`,
	}
	for _, input := range cases {
		if _, err := decodeTypedInputs(json.RawMessage(input)); !errors.Is(err, ErrInvalidTypedInputs) {
			t.Errorf("input %s: error = %v", input, err)
		}
	}
	oversize := bytes.Repeat([]byte(" "), MaxFrameBytes+1)
	if _, err := decodeTypedInputs(oversize); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestEncodeTypedInputsExactAndSorted(t *testing.T) {
	first, err := decodeTypedInputs(json.RawMessage(`{"a":{"type":"number","value":"1.0"},"b":{"type":"json","value":{"z":1,"a":2}}}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := decodeTypedInputs(json.RawMessage(`{"b":{"value":{"z":1,"a":2},"type":"json"},"a":{"value":"1.0","type":"number"}}`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := encodeTypedInputsExact(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := encodeTypedInputsExact(second)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("encoding changed with input order: %x, %x, %v", a, b, err)
	}
	variants := []json.RawMessage{
		json.RawMessage(`{"a":{"type":"number","value":"1.00"},"b":{"type":"json","value":{"z":1,"a":2}}}`),
		json.RawMessage(`{"a":{"type":"number","value":"1.0"},"b":{"type":"json","value":{"a":2,"z":1}}}`),
	}
	for _, variant := range variants {
		values, err := decodeTypedInputs(variant)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := encodeTypedInputsExact(values)
		if err != nil || bytes.Equal(a, encoded) {
			t.Fatalf("byte-distinct value has same encoding: %s, %v", variant, err)
		}
	}
	if first["a"].Type() != graph.ValueTypeNumber {
		t.Fatal("original map changed")
	}
	if _, err := encodeTypedInputsExact(map[string]graph.Value{"zero": {}}); !errors.Is(err, ErrInvalidTypedInputs) {
		t.Fatalf("zero value encoding error = %v", err)
	}
}
