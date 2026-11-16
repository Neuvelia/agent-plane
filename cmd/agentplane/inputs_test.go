package main

import (
	"testing"

	"agentplane/internal/domain/graph"
)

func TestParseInputsPreservesDeclaredTypesAndFirstEquals(t *testing.T) {
	def := &graph.Definition{ID: "input-types", Inputs: map[string]graph.InputDefinition{
		"text":     {Type: graph.ValueTypeString},
		"number":   {Type: graph.ValueTypeNumber},
		"boolean":  {Type: graph.ValueTypeBoolean},
		"json":     {Type: graph.ValueTypeJSON},
		"artifact": {Type: graph.ValueTypeArtifactRef},
	}}
	values, diagnostics, err := parseInputs(def, inputFlags{"text=a=b", "number=1.25", "boolean=false", `json={"x":1}`, "artifact=ref-1"})
	if err != nil || diagnostics.HasErrors() {
		t.Fatalf("inputs: %v %#v", err, diagnostics)
	}
	if v, ok := values["text"].AsString(); !ok || v != "a=b" {
		t.Fatalf("string=%q %v", v, ok)
	}
	if v, ok := values["number"].AsNumber(); !ok || string(v) != "1.25" {
		t.Fatalf("number=%q %v", v, ok)
	}
	if v, ok := values["boolean"].AsBoolean(); !ok || v {
		t.Fatalf("boolean=%t %v", v, ok)
	}
	if v, ok := values["json"].AsJSON(); !ok || string(v) != `{"x":1}` {
		t.Fatalf("json=%q %v", v, ok)
	}
	if v, ok := values["artifact"].AsArtifactRef(); !ok || v != "ref-1" {
		t.Fatalf("artifact=%q %v", v, ok)
	}
}

func TestParseInputsRejectsDuplicateAndInvalidTypedValue(t *testing.T) {
	def := &graph.Definition{ID: "input-types", Inputs: map[string]graph.InputDefinition{"boolean": {Type: graph.ValueTypeBoolean}}}
	if _, _, err := parseInputs(def, inputFlags{"boolean=true", "boolean=false"}); err == nil {
		t.Fatal("duplicate input accepted")
	}
	_, diagnostics, err := parseInputs(def, inputFlags{"boolean=yes"})
	if err != nil || !diagnostics.HasErrors() || diagnostics.Diagnostics[0].Code != "E_CLI_INPUT_VALUE_INVALID" {
		t.Fatalf("invalid boolean: %v %#v", err, diagnostics)
	}
}
