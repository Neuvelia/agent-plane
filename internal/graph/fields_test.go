package graph_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/fields"
	"agentplane/internal/fields/fieldstest"
	"agentplane/internal/gate"
	"agentplane/internal/graph"
)

// validateOne runs graph.Validate on a graph holding n and returns the
// diagnostics with the given code for that node as one error.
func validateOne(n graph.NodeDefinition, code string) error {
	def := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "g", Nodes: map[string]graph.NodeDefinition{"n": n}}
	var msgs []string
	for _, d := range graph.Validate(def).Diagnostics {
		if d.Code == code && d.Location.NodeID == "n" {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return errors.New(strings.Join(msgs, "; "))
}

func humanBase() graph.NodeDefinition {
	return graph.NodeDefinition{
		Type:    "human",
		Config:  graph.NodeConfig{},
		Inputs:  map[string]graph.PortDefinition{"summary": {Type: graph.ValueTypeString, Required: true}},
		Outputs: map[string]graph.PortDefinition{"decision": {Type: graph.ValueTypeString, Required: true}},
	}
}

func TestHumanConfigFieldsEqualTheValidator(t *testing.T) {
	fieldstest.Run(t, fieldstest.Spec{
		Fields: graph.HumanConfigFields(),
		Base:   humanBase(),
		Samples: map[string]any{
			"human.config.prompt":         "Approve the change?",
			"human.config.context_inputs": []any{"summary"},
		},
		Validate: func(n graph.NodeDefinition) error { return validateOne(n, "E_HUMAN_INVALID") },
	})
}

func TestHumanOutputPortsEqualTheValidator(t *testing.T) {
	fieldstest.RunPorts(t, fieldstest.PortSpec{
		Ports:       graph.HumanOutputPorts(),
		Base:        humanBase(),
		Satisfy:     func(*graph.NodeDefinition, fields.Rule) {},
		TypeChecked: true,
		Validate:    func(n graph.NodeDefinition) error { return validateOne(n, "E_HUMAN_INVALID") },
	})
}

func TestLoopMaxAttemptsBoundsEqualTheParser(t *testing.T) {
	f := graph.LoopFields()[0]
	if f.Key != "max_attempts" || f.Min == nil || f.Max == nil {
		t.Fatalf("unexpected loop descriptor %+v", f)
	}
	parse := func(n int) error {
		src := fmt.Sprintf(`schema_version: 0
id: g
inputs: {}
nodes:
  n:
    type: shell
    config: {command: /bin/echo}
    outputs: {stdout: {type: string}}
    loop:
      max_attempts: %d
      gate: {config: {check_id: c, kind: json_predicate}}
      bindings: []
edges: []
outputs: {}
`, n)
		_, err := graph.Parse([]byte(src))
		return err
	}
	for _, n := range []int{*f.Min, *f.Max} {
		if err := parse(n); err != nil {
			t.Errorf("max_attempts %d is refused: %v", n, err)
		}
	}
	for _, n := range []int{*f.Min - 1, *f.Max + 1} {
		if parse(n) == nil {
			t.Errorf("max_attempts %d is accepted", n)
		}
	}
	if (&graph.LoopDefinition{}).Attempts() != f.Default {
		t.Errorf("default %v differs from the runtime default %d", f.Default, (&graph.LoopDefinition{}).Attempts())
	}
}

func TestLoopProducersAndGateKindsEqualTheValidator(t *testing.T) {
	executorKinds := gate.ConfigFields()[1].Enum
	for _, typ := range []string{"shell", "agent", "gate", "human"} {
		for _, kind := range executorKinds {
			n := graph.NodeDefinition{Type: typ, Config: graph.NodeConfig{"allowed_paths": []any{"src"}},
				Loop: &graph.LoopDefinition{Gate: graph.LoopGateDefinition{Config: graph.NodeConfig{"kind": kind}}}}
			err := validateOne(n, "E_LOOP_INVALID")
			producerRefused := err != nil && strings.Contains(err.Error(), "loop producer must be")
			if producerRefused == graph.LoopProducer(typ) {
				t.Errorf("%s: LoopProducer=%v but validator says %v", typ, graph.LoopProducer(typ), err)
			}
			if !graph.LoopProducer(typ) {
				if graph.LoopGateKinds(typ) != nil {
					t.Errorf("%s cannot loop but offers gate kinds", typ)
				}
				continue
			}
			kindRefused := err != nil && strings.Contains(err.Error(), kind+" Gate requires")
			offered := false
			for _, k := range graph.LoopGateKinds(typ) {
				offered = offered || k == kind
			}
			if offered == kindRefused {
				t.Errorf("%s loop, gate kind %s: offered=%v but validator refusal=%v", typ, kind, offered, err)
			}
		}
	}
}

func TestHumanAndLoopKeysArePinnedToTheSpec(t *testing.T) {
	fieldstest.PinKeys(t, graph.HumanConfigFields(), "prompt", "context_inputs")
	fieldstest.PinKeys(t, graph.LoopFields(), "max_attempts")
}
