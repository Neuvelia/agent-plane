package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
)

const secretCanary = "ghp_Canary0Token9Value"

// fakeResolver answers by reference text and records every call. It is a
// hand-written fake of the SecretResolver port.
type fakeResolver struct {
	mu     sync.Mutex
	values map[string]string
	errs   map[string]error
	calls  []secrets.Ref
}

func (f *fakeResolver) Resolve(_ context.Context, ref secrets.Ref) (secrets.Value, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ref)
	if err := f.errs[ref.String()]; err != nil {
		return secrets.Value{}, err
	}
	value, ok := f.values[ref.String()]
	if !ok {
		return secrets.Value{}, fmt.Errorf("fake: %w", secrets.ErrNotFound)
	}
	return secrets.NewValue(value), nil
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// receiverExecutor counts node runs and declares which node types it can feed
// secrets to (the SecretReceiver capability).
type receiverExecutor struct {
	runs     atomic.Int32
	nodeType string
}

func newReceiver() *receiverExecutor { return &receiverExecutor{nodeType: "shell"} }

func (e *receiverExecutor) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	e.runs.Add(1)
	return map[string]graph.Value{}, nil
}

func (e *receiverExecutor) ReceivesSecrets(nodeType string) bool { return nodeType == e.nodeType }

// observedReceiver is a receiver that is also an ObservedExecutor.
type observedReceiver struct{ receiverExecutor }

func newObservedReceiver() *observedReceiver {
	return &observedReceiver{receiverExecutor{nodeType: "shell"}}
}

func (e *observedReceiver) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, _ ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	out, err := e.ExecuteNode(ctx, id, n, in)
	return out, nil, err
}

// secretDefinition declares secrets (name -> reference) and gives every node ID
// a shell node whose secret_env is the given variable -> secret mapping.
func secretDefinition(declared map[string]string, assignments map[string]map[string]string) *graph.Definition {
	def := definition(map[string]graph.NodeDefinition{})
	def.Secrets = map[string]graph.SecretDefinition{}
	for name, ref := range declared {
		def.Secrets[name] = graph.SecretDefinition{SecretRef: ref}
	}
	for id, assigned := range assignments {
		config := map[string]any{"command": "/bin/true"}
		if assigned != nil {
			env := map[string]any{}
			for variable, secret := range assigned {
				env[variable] = secret
			}
			config["secret_env"] = env
		}
		def.Nodes[id] = graph.NodeDefinition{Type: "shell", Config: config}
	}
	return def
}

func ghDefinition() *graph.Definition {
	return secretDefinition(map[string]string{"gh_token": "env/GITHUB_TOKEN"}, map[string]map[string]string{"a": {"GH_TOKEN": "gh_token"}, "sibling": nil})
}

func ghResolver() *fakeResolver {
	return &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": secretCanary}}
}

// persisted counts the events a run emitted; a failed preflight must emit none.
type persisted struct{ n atomic.Int32 }

func (p *persisted) options(resolver SecretResolver) Options {
	return Options{
		GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, SecretResolver: resolver,
		Persist: func(events.Event) error { p.n.Add(1); return nil },
	}
}

func preflightDiagnostics(t *testing.T, err error) []graph.Diagnostic {
	t.Helper()
	var preflight *run.PreflightError
	if !errors.As(err, &preflight) {
		t.Fatalf("err = %v, want *run.PreflightError", err)
	}
	return preflight.Diagnostics.Diagnostics
}

func diagnosticCodes(diagnostics []graph.Diagnostic) []string {
	codes := make([]string, len(diagnostics))
	for i, d := range diagnostics {
		codes[i] = d.Code
	}
	return codes
}

// requireSecretPreflightFailure runs the graph and asserts the failure code
// list, that nothing ran or was emitted, and that no diagnostic holds secret text.
func requireSecretPreflightFailure(t *testing.T, def *graph.Definition, exec Executor, resolver SecretResolver, wantCodes ...string) []graph.Diagnostic {
	t.Helper()
	sink := &persisted{}
	_, err := Run(context.Background(), def, nil, exec, sink.options(resolver))
	diagnostics := preflightDiagnostics(t, err)
	got := diagnosticCodes(diagnostics)
	if strings.Join(got, ",") != strings.Join(wantCodes, ",") {
		t.Fatalf("codes = %v, want %v", got, wantCodes)
	}
	if sink.n.Load() != 0 {
		t.Errorf("a failed preflight emitted %d events", sink.n.Load())
	}
	if counter, ok := exec.(interface{ runCount() int32 }); ok && counter.runCount() != 0 {
		t.Errorf("%d nodes ran before preflight failed", counter.runCount())
	}
	encoded, _ := json.Marshal(diagnostics)
	for _, form := range secrets.Forms(secretCanary) {
		if strings.Contains(string(encoded)+err.Error(), string(form)) {
			t.Errorf("diagnostics hold secret text %q", form)
		}
	}
	return diagnostics
}

func (e *receiverExecutor) runCount() int32 { return e.runs.Load() }
