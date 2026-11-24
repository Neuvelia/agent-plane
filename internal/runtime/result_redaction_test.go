package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

const resultSecret = "TOPSECRET-4821"

func testResultRedactor(t *testing.T, logValues []string, resolved []resolvedSecret) *resultRedactor {
	t.Helper()
	template, err := newRedactionSet(logValues, resolved)
	if err != nil {
		t.Fatal(err)
	}
	return newResultRedactor(template)
}

func mustJSONValue(t *testing.T, raw string) graph.Value {
	t.Helper()
	value, err := graph.NewJSONValue(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustNumberValue(t *testing.T, raw string) graph.Value {
	t.Helper()
	value, err := graph.NewNumberValue(json.Number(raw))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func redactedPort(r *resultRedactor, value graph.Value) run.PortValue {
	return r.port(run.PortValue{Name: "out", Value: value})
}

func requireWithheld(t *testing.T, port run.PortValue, want graph.ValueType) {
	t.Helper()
	if !port.Redacted || port.Withheld != want || port.Value.Type() != "" {
		t.Fatalf("port = %#v, want withheld %s", port, want)
	}
}

func TestResultRedactorIsNilWithoutASecretSet(t *testing.T) {
	if r := testResultRedactor(t, nil, nil); r != nil {
		t.Fatalf("redactor = %#v, want nil", r)
	}
	var r *resultRedactor
	in := []run.PortValue{{Name: "a", Value: graph.NewStringValue(resultSecret)}}
	if got := r.ports(in); !reflect.DeepEqual(got, in) {
		t.Fatalf("nil redactor changed ports: %#v", got)
	}
}

func TestResultRedactorMasksSecretInStringOutput(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	port := redactedPort(r, graph.NewStringValue("token="+resultSecret+"!"))
	text, _ := port.Value.AsString()
	if text != "token=***!" || !port.Redacted || port.Withheld != "" {
		t.Fatalf("port = %#v (%q)", port, text)
	}
}

func TestResultRedactorMasksEncodedFormsOfSecret(t *testing.T) {
	r := testResultRedactor(t, nil, []resolvedSecret{{name: "tok", value: resultSecret}})
	encoded := base64.StdEncoding.EncodeToString([]byte(resultSecret))
	port := redactedPort(r, graph.NewStringValue("raw "+resultSecret+" b64 "+encoded))
	text, _ := port.Value.AsString()
	if strings.Contains(text, resultSecret) || strings.Contains(text, encoded) || !port.Redacted {
		t.Fatalf("text = %q", text)
	}
}

func TestResultRedactorMasksArtifactReference(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	ref, err := graph.NewArtifactRefValue("file://" + resultSecret)
	if err != nil {
		t.Fatal(err)
	}
	port := redactedPort(r, ref)
	text, ok := port.Value.AsArtifactRef()
	if !ok || text != "file://***" || !port.Redacted {
		t.Fatalf("port = %#v (%q)", port, text)
	}
}

func TestResultRedactorWithholdsJSONOutputHoldingSecret(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	requireWithheld(t, redactedPort(r, mustJSONValue(t, `{"k":"`+resultSecret+`"}`)), graph.ValueTypeJSON)
}

func TestResultRedactorWithholdsJSONSecretSpelledWithEscapes(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	escaped := `{"k":"TOPSECRET-4821"}`
	requireWithheld(t, redactedPort(r, mustJSONValue(t, escaped)), graph.ValueTypeJSON)
}

func TestResultRedactorWithholdsNumberAndBooleanChangedByRedaction(t *testing.T) {
	r := testResultRedactor(t, []string{"1234567890", "true"}, nil)
	requireWithheld(t, redactedPort(r, mustNumberValue(t, "1234567890")), graph.ValueTypeNumber)
	requireWithheld(t, redactedPort(r, graph.NewBooleanValue(true)), graph.ValueTypeBoolean)
}

func TestResultRedactorWithholdsValueWhoseMaskIsNotUTF8(t *testing.T) {
	// Only byte 0xFF is free, so the marker is a run of 0xFF: not valid UTF-8.
	secret := make([]byte, 0, 255)
	for b := 0; b < 255; b++ {
		secret = append(secret, byte(b))
	}
	r := testResultRedactor(t, []string{string(secret)}, nil)
	port := redactedPort(r, graph.NewStringValue("a"+string(secret)+"b"))
	requireWithheld(t, port, graph.ValueTypeString)
}

func TestResultRedactorLeavesUnrelatedValuesIdentical(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	for _, value := range []graph.Value{graph.NewStringValue("plain"), mustNumberValue(t, "1.50"), graph.NewBooleanValue(false), mustJSONValue(t, `{"a":[1,2]}`)} {
		port := redactedPort(r, value)
		if port.Redacted || port.Withheld != "" || !reflect.DeepEqual(port.Value, value) {
			t.Fatalf("port = %#v, want unchanged %#v", port, value)
		}
	}
}

func TestResultRedactorKeepsProvenanceAndPortOrder(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	provenance := &run.AcceptedProvenance{}
	in := []run.PortValue{{Name: "a", Provenance: provenance, Value: graph.NewStringValue(resultSecret)}, {Name: "b", Value: graph.NewStringValue("x")}}
	got := r.ports(in)
	if got[0].Name != "a" || got[1].Name != "b" || got[0].Provenance != provenance {
		t.Fatalf("ports = %#v", got)
	}
	if text, _ := in[0].Value.AsString(); text != resultSecret {
		t.Fatalf("input slice was mutated: %q", text)
	}
}

type causeError struct{ code int }

func (e *causeError) Error() string { return fmt.Sprintf("cause %d leaked %s", e.code, resultSecret) }

var errSentinel = errors.New("sentinel " + resultSecret)

// evidenceError models a typed error carrying raw process text in a field.
type evidenceError struct {
	Code   int
	Stdout string
}

func (*evidenceError) Error() string { return "evidence failed" }

func (e *evidenceError) Redacted(mask func(string) string) error {
	copied := *e
	copied.Stdout = mask(e.Stdout)
	return &copied
}

func TestResultRedactorMasksFailureErrorTextAndKeepsErrorsIs(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	cause := &causeError{code: 7}
	original := &run.Failure{NodeID: "n", Kind: run.FailureExecutor, Err: fmt.Errorf("wrapped: %w", errors.Join(cause, errSentinel))}
	got := r.failure(original)
	if strings.Contains(got.Err.Error(), resultSecret) || !strings.Contains(got.Err.Error(), "***") {
		t.Fatalf("message = %q", got.Err)
	}
	if !errors.Is(got.Err, errSentinel) || !errors.Is(got.Err, cause) {
		t.Fatalf("errors.Is lost the cause chain: %v", got.Err)
	}
	var target *causeError
	if errors.As(got.Err, &target) || errors.Unwrap(got.Err) != nil {
		t.Fatal("a type without a Redacted copy must not be reachable through errors.As or Unwrap")
	}
	if !strings.Contains(original.Err.Error(), resultSecret) {
		t.Fatal("the original failure was mutated")
	}
}

func TestResultRedactorExposesOnlyMaskedCopyOfTypedEvidence(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	original := &evidenceError{Code: 3, Stdout: "out " + resultSecret}
	got := r.failure(&run.Failure{Kind: run.FailureExecutor, Err: fmt.Errorf("wrapped: %w", original)})
	var target *evidenceError
	if !errors.As(got.Err, &target) || target.Code != 3 || strings.Contains(target.Stdout, resultSecret) || target.Stdout != "out ***" {
		t.Fatalf("typed copy = %#v", target)
	}
	if target == original || original.Stdout != "out "+resultSecret {
		t.Fatal("the raw typed error was exposed or mutated")
	}
	if !errors.Is(got.Err, original) {
		t.Fatal("errors.Is must still match the original cause")
	}
}

func TestResultRedactorKeepsFailureWithoutSecretText(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	original := &run.Failure{Kind: run.FailureScheduler, Err: errors.New("plain")}
	if got := r.failure(original); !errors.Is(got.Err, original.Err) || got.Err.Error() != "plain" {
		t.Fatalf("unchanged error was replaced: %#v", got.Err)
	}
	if r.failure(nil) != nil {
		t.Fatal("nil failure must stay nil")
	}
}

func resultGraph() *graph.Definition {
	def := definition(map[string]graph.NodeDefinition{
		"a": {Type: "test", Outputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}},
		"b": {Type: "test", Inputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{"echo": {Type: graph.ValueTypeString, Required: true}}},
	}, graph.EdgeDefinition{From: "a.outputs.text", To: "b.inputs.text"})
	def.Outputs["final"] = graph.OutputDefinition{From: "b.outputs.echo"}
	return def
}

func resultExecutor(received *sync.Map) executorFunc {
	return func(_ context.Context, id string, _ graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "a" {
			return map[string]graph.Value{"text": graph.NewStringValue("token=" + resultSecret)}, nil
		}
		text, _ := in["text"].AsString()
		received.Store("b", text)
		return map[string]graph.Value{"echo": graph.NewStringValue(text)}, nil
	}
}

func runResultGraph(t *testing.T, options Options) run.Result {
	t.Helper()
	options.GraphVersion, options.Clock, options.IDs = yamlgraph.Marshal, system.Clock{}, system.IDs{}
	var received sync.Map
	result, err := Run(context.Background(), resultGraph(), nil, resultExecutor(&received), options)
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %v %v", result.State, err)
	}
	if got, _ := received.Load("b"); got != "token="+resultSecret {
		t.Fatalf("downstream received %q, want the raw accepted value", got)
	}
	return result
}

func TestRunResultRedactsGraphAndNodeOutputsButNotDataflow(t *testing.T) {
	result := runResultGraph(t, Options{LogRedactions: []string{resultSecret}})
	encoded := fmt.Sprintf("%#v", result.Outputs)
	for _, node := range result.Nodes {
		encoded += fmt.Sprintf("%#v", node.Outputs)
	}
	if strings.Contains(encoded, resultSecret) {
		t.Fatalf("result holds the secret: %s", encoded)
	}
	text, _ := result.Outputs[0].Value.AsString()
	if text != "token=***" || !result.Outputs[0].Redacted {
		t.Fatalf("output = %#v", result.Outputs[0])
	}
}

func TestRunResultWithoutRedactionsIsUnchanged(t *testing.T) {
	result := runResultGraph(t, Options{})
	text, _ := result.Outputs[0].Value.AsString()
	if text != "token="+resultSecret || result.Outputs[0].Redacted {
		t.Fatalf("output = %#v", result.Outputs[0])
	}
}

func TestResultRedactorApplyDoesNotMutateTheMachineState(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	node := &run.NodeResult{NodeID: "a", Outputs: []run.PortValue{{Name: "t", Value: graph.NewStringValue(resultSecret)}}, Failure: &run.Failure{Err: errSentinel}}
	snapshot := run.Result{Nodes: []run.NodeResult{*node}, Outputs: node.Outputs, Failure: node.Failure}
	for i := 0; i < 2; i++ {
		copied := snapshot
		copied.Nodes = append([]run.NodeResult(nil), snapshot.Nodes...)
		r.apply(&copied)
		if text, _ := copied.Outputs[0].Value.AsString(); text != "***" {
			t.Fatalf("snapshot %d = %q", i, text)
		}
	}
	if text, _ := node.Outputs[0].Value.AsString(); text != resultSecret || !errors.Is(node.Failure.Err, errSentinel) || node.Failure.Err.Error() != errSentinel.Error() {
		t.Fatal("machine state was mutated")
	}
}

func TestResultRedactionIsRaceFree(t *testing.T) {
	r := testResultRedactor(t, []string{resultSecret}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if got := r.text("a " + resultSecret + " b"); got != "a *** b" {
					t.Errorf("text = %q", got)
				}
			}
		}()
	}
	wg.Wait()
}
