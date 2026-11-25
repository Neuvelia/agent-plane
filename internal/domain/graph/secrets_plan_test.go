package graph

import (
	"reflect"
	"testing"
)

func planNode(nodeType string, secretEnv map[string]string) NodeDefinition {
	config := NodeConfig{"command": "/bin/true"}
	if secretEnv != nil {
		config[secretEnvKey] = secretEnv
	}
	return NodeDefinition{Type: nodeType, Config: config}
}

func planDefinition(declared map[string]string, nodes map[string]NodeDefinition) *Definition {
	secretDefs := map[string]SecretDefinition{}
	for name, ref := range declared {
		secretDefs[name] = SecretDefinition{SecretRef: ref}
	}
	return &Definition{
		ID: "plan", Inputs: map[string]InputDefinition{}, Edges: []EdgeDefinition{}, Outputs: map[string]OutputDefinition{},
		Secrets: secretDefs, Nodes: nodes,
	}
}

func TestPlanSecretsGrantsSortedByVariable(t *testing.T) {
	definition := planDefinition(
		map[string]string{"a_tok": "env/A", "b_tok": "env/B"},
		map[string]NodeDefinition{"n": planNode("shell", map[string]string{"ZED": "a_tok", "ALPHA": "b_tok"})},
	)
	plan := PlanSecrets(definition)
	want := []SecretGrant{{Variable: "ALPHA", Secret: "b_tok"}, {Variable: "ZED", Secret: "a_tok"}}
	if !reflect.DeepEqual(plan.Grants["n"], want) {
		t.Errorf("grants = %v, want %v", plan.Grants["n"], want)
	}
	if got := plan.Assigned(); !reflect.DeepEqual(got, []string{"a_tok", "b_tok"}) {
		t.Errorf("Assigned = %v", got)
	}
	if got := plan.NodeIDs(); !reflect.DeepEqual(got, []string{"n"}) {
		t.Errorf("NodeIDs = %v", got)
	}
	if ref := plan.Refs["a_tok"]; ref.Provider != "env" || ref.Name != "A" {
		t.Errorf("ref = %v", ref)
	}
}

func TestPlanSecretsSkipsUnassignedAndNonShell(t *testing.T) {
	definition := planDefinition(
		map[string]string{"used": "env/U", "unused": "env/X"},
		map[string]NodeDefinition{
			"sh":   planNode("shell", map[string]string{"TOKEN": "used"}),
			"ag":   planNode("agent", map[string]string{"TOKEN": "unused"}),
			"gate": planNode("gate", map[string]string{"TOKEN": "unused"}),
			"bare": planNode("shell", nil),
		},
	)
	plan := PlanSecrets(definition)
	if got := plan.NodeIDs(); !reflect.DeepEqual(got, []string{"sh"}) {
		t.Errorf("NodeIDs = %v, want only the shell node", got)
	}
	if got := plan.Assigned(); !reflect.DeepEqual(got, []string{"used"}) {
		t.Errorf("Assigned = %v, want only the assigned secret", got)
	}
}

func TestPlanSecretsIgnoresUndeclaredAndInvalidReferences(t *testing.T) {
	definition := planDefinition(
		map[string]string{"bad": "no-slash"},
		map[string]NodeDefinition{"n": planNode("shell", map[string]string{"A": "bad", "B": "ghost"})},
	)
	if plan := PlanSecrets(definition); !plan.Empty() {
		t.Errorf("plan = %#v, want empty", plan)
	}
}

func TestPlanSecretsEmptyWithoutSecrets(t *testing.T) {
	definition := planDefinition(nil, map[string]NodeDefinition{"n": planNode("shell", nil)})
	plan := PlanSecrets(definition)
	if !plan.Empty() || len(plan.Assigned()) != 0 || len(plan.NodeIDs()) != 0 {
		t.Errorf("plan = %#v, want empty", plan)
	}
	if !PlanSecrets(nil).Empty() {
		t.Error("a nil definition must plan nothing")
	}
}
