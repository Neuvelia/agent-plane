package graph

import (
	"encoding/json"
	"testing"
)

func TestValueConstructorsRejectInvalidPayloads(t *testing.T) {
	for _, invalid := range []json.Number{"not-a-number", "1 ", "1\n"} {
		if _, err := NewNumberValue(invalid); err == nil {
			t.Fatalf("NewNumberValue(%q) error = nil", invalid)
		}
	}
	if _, err := NewJSONValue(json.RawMessage(`{"broken":`)); err == nil {
		t.Fatal("NewJSONValue(invalid) error = nil")
	}
	if _, err := NewArtifactRefValue(""); err == nil {
		t.Fatal("NewArtifactRefValue(empty) error = nil")
	}
}

func TestBooleanAndJSONNullRemainTypedValues(t *testing.T) {
	boolean := NewBooleanValue(false)
	if got, ok := boolean.AsBoolean(); !ok || got {
		t.Fatalf("boolean = (%v, %v), want present false", got, ok)
	}
	jsonNull, err := NewJSONValue(json.RawMessage("null"))
	if err != nil {
		t.Fatalf("NewJSONValue(null) error = %v", err)
	}
	if got, ok := jsonNull.AsJSON(); !ok || string(got) != "null" {
		t.Fatalf("JSON null = (%s, %v), want present null", got, ok)
	}
	if jsonNull.Type() != ValueTypeJSON {
		t.Fatalf("JSON null type = %q, want json", jsonNull.Type())
	}
}
