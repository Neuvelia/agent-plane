package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
	"agentplane/internal/gate"
	"agentplane/internal/runtime"
)

const deliveredCanary = "ghp_Delivered0Canary7Value"

// delivery is one observed executor call: which node and node type asked, and
// the secret variable names the executor was handed.
type delivery struct {
	node, nodeType string
	names          []string
	printed        string
}

// recordingExecutor is a hand-written Shell-like executor and SecretReceiver.
// It records what each call receives and delegates Gate nodes to the real Gate.
type recordingExecutor struct {
	mu         sync.Mutex
	deliveries []delivery
	attempts   int
	greenAfter int
}

func (r *recordingExecutor) ReceivesSecrets(nodeType string) bool { return nodeType == "shell" }

func (r *recordingExecutor) ValidateNodeConfig(string, graph.NodeDefinition) error { return nil }

func (r *recordingExecutor) PreflightNode(context.Context, string, graph.NodeDefinition) error {
	return nil
}

func (r *recordingExecutor) ExecuteNode(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
	out, _, err := r.ExecuteNodeObserved(ctx, id, n, in, runtime.ExecutionObservation{})
	return out, err
}

func (r *recordingExecutor) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	r.mu.Lock()
	r.deliveries = append(r.deliveries, delivery{node: id, nodeType: n.Type, names: o.SecretEnv.Names(), printed: fmt.Sprintf("%+v %v %#v", o, o.SecretEnv, o.SecretEnv)})
	r.mu.Unlock()
	if n.Type == "gate" {
		return (gate.Executor{}).ExecuteNodeObserved(ctx, id, n, in, o)
	}
	if o.Log != nil {
		o.Log("stdout", []byte("child saw "+deliveredCanary+"\n"))
	}
	if len(n.Outputs) == 0 {
		return map[string]graph.Value{}, nil, nil
	}
	r.mu.Lock()
	r.attempts++
	ok := r.attempts >= r.greenAfter
	r.mu.Unlock()
	value, err := graph.NewJSONValue(json.RawMessage(fmt.Sprintf(`{"ok":%t}`, ok)))
	return map[string]graph.Value{"result": value}, nil, err
}

func (r *recordingExecutor) callsFor(node, nodeType string) []delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	var calls []delivery
	for _, d := range r.deliveries {
		if d.node == node && d.nodeType == nodeType {
			calls = append(calls, d)
		}
	}
	return calls
}

func deliveryOptions(resolver runtime.SecretResolver, sink *[]events.Event) runtime.Options {
	var mu sync.Mutex
	return runtime.Options{
		GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, SecretResolver: resolver,
		Persist: func(e events.Event) error {
			mu.Lock()
			defer mu.Unlock()
			*sink = append(*sink, e)
			return nil
		},
	}
}

type tokenResolver struct{}

func (tokenResolver) Resolve(context.Context, secrets.Ref) (secrets.Value, error) {
	return secrets.NewValue(deliveredCanary), nil
}

func shellNode(secretEnv map[string]any) graph.NodeDefinition {
	config := graph.NodeConfig{"command": "/bin/true"}
	if secretEnv != nil {
		config["secret_env"] = secretEnv
	}
	return graph.NodeDefinition{Type: "shell", Config: config}
}

func secretGraph(nodes map[string]graph.NodeDefinition) *graph.Definition {
	return &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0, ID: "delivery", Nodes: nodes, Edges: []graph.EdgeDefinition{}, Inputs: map[string]graph.InputDefinition{},
		Outputs: map[string]graph.OutputDefinition{},
		Secrets: map[string]graph.SecretDefinition{"gh_token": {SecretRef: "env/GITHUB_TOKEN"}},
	}
}

func loopingShellNode(budget int) graph.NodeDefinition {
	node := shellNode(map[string]any{"GH_TOKEN": "gh_token"})
	node.Outputs = map[string]graph.PortDefinition{"result": {Type: graph.ValueTypeJSON, Required: true}}
	node.Loop = &graph.LoopDefinition{MaxAttempts: budget,
		Gate: graph.LoopGateDefinition{
			Inputs: map[string]graph.PortDefinition{"candidate": {Type: graph.ValueTypeJSON, Required: true}},
			Config: graph.NodeConfig{"check_id": "ok", "kind": "json_predicate", "input": "candidate", "field": []string{"ok"}, "operator": "eq", "expected": true},
		},
		Bindings: []graph.LoopBinding{{From: "candidate.outputs.result", To: "gate.inputs.candidate"}},
	}
	return node
}

func TestSecretEnvDeliveredOnlyToAssignedNode(t *testing.T) {
	exec := &recordingExecutor{}
	def := secretGraph(map[string]graph.NodeDefinition{"a": shellNode(map[string]any{"GH_TOKEN": "gh_token"}), "sibling": shellNode(nil)})
	var sink []events.Event
	if _, err := runtime.Run(context.Background(), def, nil, exec, deliveryOptions(tokenResolver{}, &sink)); err != nil {
		t.Fatal(err)
	}
	if got := exec.callsFor("a", "shell"); len(got) != 1 || strings.Join(got[0].names, ",") != "GH_TOKEN" {
		t.Errorf("assigned node deliveries = %+v, want one with GH_TOKEN", got)
	}
	if got := exec.callsFor("sibling", "shell"); len(got) != 1 || len(got[0].names) != 0 {
		t.Errorf("sibling deliveries = %+v, want one with no secrets", got)
	}
}

func TestSecretEnvEmptyWithoutAssignment(t *testing.T) {
	exec := &recordingExecutor{}
	def := secretGraph(map[string]graph.NodeDefinition{"a": shellNode(nil)})
	def.Secrets = nil
	var sink []events.Event
	if _, err := runtime.Run(context.Background(), def, nil, exec, deliveryOptions(nil, &sink)); err != nil {
		t.Fatal(err)
	}
	if got := exec.callsFor("a", "shell"); len(got) != 1 || len(got[0].names) != 0 {
		t.Errorf("deliveries = %+v, want none", got)
	}
}

func TestSecretEnvReachesLoopProducerEachAttemptButNotGate(t *testing.T) {
	exec := &recordingExecutor{greenAfter: 3}
	def := secretGraph(map[string]graph.NodeDefinition{"work": loopingShellNode(3)})
	var sink []events.Event
	if _, err := runtime.Run(context.Background(), def, nil, exec, deliveryOptions(tokenResolver{}, &sink)); err != nil {
		t.Fatal(err)
	}
	producer := exec.callsFor("work", "shell")
	if len(producer) != 3 {
		t.Fatalf("producer ran %d times, want 3", len(producer))
	}
	for i, call := range producer {
		if strings.Join(call.names, ",") != "GH_TOKEN" {
			t.Errorf("attempt %d names = %v, want GH_TOKEN", i+1, call.names)
		}
	}
	gates := exec.callsFor("work", "gate")
	if len(gates) != 3 {
		t.Fatalf("gate ran %d times, want 3", len(gates))
	}
	for i, call := range gates {
		if len(call.names) != 0 {
			t.Errorf("embedded Gate %d received secrets %v", i+1, call.names)
		}
	}
}

func TestSecretValueAbsentFromResultEventsAndPrintedObservation(t *testing.T) {
	exec := &recordingExecutor{greenAfter: 1}
	def := secretGraph(map[string]graph.NodeDefinition{"work": loopingShellNode(2)})
	var sink []events.Event
	result, err := runtime.Run(context.Background(), def, nil, exec, deliveryOptions(tokenResolver{}, &sink))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(sink)
	if err != nil {
		t.Fatal(err)
	}
	resultJSON, err := json.Marshal(result.Events)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range exec.deliveries {
		for _, form := range secrets.Forms(deliveredCanary) {
			if strings.Contains(d.printed, string(form)) {
				t.Errorf("printing an observation revealed %q", form)
			}
		}
	}
	for _, form := range secrets.Forms(deliveredCanary) {
		for name, data := range map[string][]byte{"persisted": encoded, "result events": resultJSON} {
			if strings.Contains(string(data), string(form)) {
				t.Errorf("%s hold a secret form %q", name, form)
			}
		}
	}
}
