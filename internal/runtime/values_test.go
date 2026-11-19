package runtime

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

func recordedValues(evs []events.Event) []events.Event {
	var out []events.Event
	for _, e := range evs {
		if e.Type == events.ValueRecorded {
			out = append(out, e)
		}
	}
	return out
}

func valuePayload(t *testing.T, e events.Event) events.ValueRecordedPayload {
	t.Helper()
	p, ok := e.Payload.(events.ValueRecordedPayload)
	if !ok {
		t.Fatalf("payload = %#v", e.Payload)
	}
	return p
}

func hexDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// valueGraph: graph input job -> a.job; a.text -> b.text; a also has a JSON
// output and an optional output it never returns.
func valueGraph() *graph.Definition {
	def := definition(map[string]graph.NodeDefinition{
		"a": {Type: "test", Inputs: map[string]graph.PortDefinition{"job": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{
			"text": {Type: graph.ValueTypeString, Required: true}, "doc": {Type: graph.ValueTypeJSON, Required: true}, "maybe": {Type: graph.ValueTypeString},
		}},
		"b": {Type: "test", Inputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{"n": {Type: graph.ValueTypeNumber, Required: true}, "ok": {Type: graph.ValueTypeBoolean, Required: true}}},
	}, graph.EdgeDefinition{From: "a.outputs.text", To: "b.inputs.text"})
	def.Inputs = map[string]graph.InputDefinition{"job": {Type: graph.ValueTypeString, Required: true}, "unused": {Type: graph.ValueTypeString}}
	def.GraphInputBindings = []graph.GraphInputBindingDefinition{{From: "inputs.job", To: "a.inputs.job"}}
	return def
}

func valueExecutor(text string) executorFunc {
	return func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "a" {
			doc, _ := graph.NewJSONValue(json.RawMessage(`{"k":[1,2]}`))
			return map[string]graph.Value{"text": graph.NewStringValue(text), "doc": doc}, nil
		}
		n, _ := graph.NewNumberValue("1.50")
		return map[string]graph.Value{"n": n, "ok": graph.NewBooleanValue(false)}, nil
	}
}

func TestValueRecordingIsOffByDefault(t *testing.T) {
	result, err := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("do it")}, valueExecutor("hi"), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %#v, %v", result, err)
	}
	if got := recordedValues(result.Events); len(got) != 0 {
		t.Fatalf("recording off emitted %d values", len(got))
	}
}

func TestValueRecordingOrderAndShape(t *testing.T) {
	result, err := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("")}, valueExecutor("hi"), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %#v, %v", result, err)
	}
	evs := result.Events
	if evs[0].Type != events.RunStarted || evs[1].Type != events.ValueRecorded {
		t.Fatalf("graph input not recorded right after run_started: %v %v", evs[0].Type, evs[1].Type)
	}
	input := valuePayload(t, evs[1])
	if input.Port != "inputs.job" || evs[1].NodeID != "" || input.NodeRunID != "" || input.Value == nil || *input.Value != "" || input.Truncated || input.SizeBytes != 0 || input.SHA256 != hexDigest("") {
		t.Fatalf("empty graph input = %#v", input)
	}
	type key struct{ node, port, value, typ string }
	var got []key
	for i, e := range evs {
		if e.Type != events.ValueRecorded || e.NodeID == "" {
			continue
		}
		p := valuePayload(t, e)
		got = append(got, key{e.NodeID, p.Port, *p.Value, p.Type})
		// Every output value precedes its node's succeeded transition, with only
		// other values of the same node in between.
		j := i + 1
		for evs[j].Type == events.ValueRecorded && evs[j].NodeID == e.NodeID {
			j++
		}
		if evs[j].Type != events.NodeFinished || evs[j].NodeID != e.NodeID || evs[j].Payload.(events.NodeFinishedPayload).Status != "succeeded" {
			t.Fatalf("value %v not followed by node_finished: %#v", p.Port, evs[j])
		}
		if p.NodeRunID != e.NodeRunID || e.NodeRunID != nodeResult(t, result, e.NodeID).NodeRunID || p.AttemptID != "" {
			t.Fatalf("identifiers = %#v", e)
		}
	}
	want := []key{{"a", "doc", `{"k":[1,2]}`, "json"}, {"a", "text", "hi", "string"}, {"b", "n", "1.50", "number"}, {"b", "ok", "false", "boolean"}}
	if len(got) != len(want) {
		t.Fatalf("values = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("values = %#v, want %#v", got, want)
		}
	}
	// Survives serialization exactly.
	for _, e := range recordedValues(evs) {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var back events.Event
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValueRecordingTruncatesAndRedactsAcrossTheCut(t *testing.T) {
	secret := "TOPSECRET"
	// The secret straddles the 16 KiB cut: bytes 16380..16388.
	text := strings.Repeat("é", 8190) + secret + strings.Repeat("z", 100)
	result, err := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("uses " + secret)}, valueExecutor(text), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true, LogRedactions: []string{secret}})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %#v, %v", result, err)
	}
	encoded, _ := json.Marshal(result.Events)
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "TOPSEC") {
		t.Fatal("secret or its prefix reached events")
	}
	var found bool
	for _, e := range recordedValues(result.Events) {
		p := valuePayload(t, e)
		if p.Port == "inputs.job" && (!p.Redacted || *p.Value != "uses ***") {
			t.Fatalf("input = %#v", p)
		}
		if e.NodeID == "a" && p.Port == "text" {
			found = true
			redacted := strings.Repeat("é", 8190) + "***" + strings.Repeat("z", 100)
			if !p.Truncated || !p.Redacted || len(*p.Value) > events.MaxValueBytes || p.SizeBytes != int64(len(redacted)) || p.SHA256 != hexDigest(redacted) || !strings.HasPrefix(redacted, *p.Value) {
				t.Fatalf("truncated = len %d %#v", len(*p.Value), p.SizeBytes)
			}
		}
	}
	if !found {
		t.Fatal("text not recorded")
	}
}

func TestValueRecordingCutsAtRuneBoundary(t *testing.T) {
	text := "a" + strings.Repeat("€", 6000) // 3-byte runes; 16384-1 is not a multiple of 3
	result, _ := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, valueExecutor(text), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	for _, e := range recordedValues(result.Events) {
		if p := valuePayload(t, e); p.Port == "text" {
			if !p.Truncated || len(*p.Value) != 16384 || p.SizeBytes != int64(len(text)) || p.SHA256 != hexDigest(text) {
				t.Fatalf("cut = %d %v", len(*p.Value), p.Truncated)
			}
		}
	}
	text = "ab" + strings.Repeat("€", 6000)
	result, _ = Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, valueExecutor(text), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	for _, e := range recordedValues(result.Events) {
		if p := valuePayload(t, e); p.Port == "text" && len(*p.Value) != 16382 {
			t.Fatalf("cut inside a rune: %d", len(*p.Value))
		}
	}
}

func TestValueRecordingRunBudgetOmitsLaterValues(t *testing.T) {
	const nodes = 70 // 70 * 16 KiB > 1 MiB
	defs := map[string]graph.NodeDefinition{}
	for i := 0; i < nodes; i++ {
		defs[string(rune('a'+i/26))+string(rune('a'+i%26))] = graph.NodeDefinition{Type: "test", Outputs: map[string]graph.PortDefinition{"out": {Type: graph.ValueTypeString, Required: true}}}
	}
	big := strings.Repeat("v", events.MaxValueBytes+10)
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return map[string]graph.Value{"out": graph.NewStringValue(big)}, nil
	})
	result, err := Run(context.Background(), definition(defs), nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true, MaxConcurrency: 4})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %v %v", result.State, err)
	}
	total, omitted := 0, 0
	for _, e := range recordedValues(result.Events) {
		p := valuePayload(t, e)
		if !p.Truncated || p.SizeBytes != int64(len(big)) || p.SHA256 != hexDigest(big) {
			t.Fatalf("payload = %#v", p)
		}
		if p.Value == nil {
			omitted++
			continue
		}
		if omitted > 0 {
			t.Fatal("a value was carried after the budget was exhausted")
		}
		total += len(*p.Value)
	}
	if total != events.MaxRunValueBytes || omitted != nodes-events.MaxRunValueBytes/events.MaxValueBytes {
		t.Fatalf("carried %d bytes, omitted %d", total, omitted)
	}
	if len(recordedValues(result.Events)) != nodes {
		t.Fatal("every accepted port must still be recorded")
	}
}

func TestValueRecordingOmitsInvalidUTF8(t *testing.T) {
	result, _ := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, valueExecutor("ok\xffno"), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	for _, e := range recordedValues(result.Events) {
		if p := valuePayload(t, e); p.Port == "text" && (p.Value != nil || !p.Truncated || p.SizeBytes != 5 || p.SHA256 != hexDigest("ok\xffno")) {
			t.Fatalf("invalid UTF-8 = %#v", p)
		}
	}
}

func TestValueRecordingSkipsFailedSkippedAndCancelledNodes(t *testing.T) {
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, errors.New("boom")
	})
	result, _ := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	if result.State != run.RunFailed {
		t.Fatal(result.State)
	}
	for _, e := range recordedValues(result.Events) {
		if e.NodeID != "" {
			t.Fatalf("failed or skipped node recorded %#v", e)
		}
	}
	// Output contract failure: a returned but invalid output is not accepted.
	exec = func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		return map[string]graph.Value{"text": graph.NewStringValue("partial")}, nil
	}
	result, _ = Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	for _, e := range recordedValues(result.Events) {
		if e.NodeID != "" {
			t.Fatalf("rejected output recorded %#v", e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	exec = func(ctx context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		cancel()
		<-ctx.Done()
		return map[string]graph.Value{"text": graph.NewStringValue("late")}, ctx.Err()
	}
	result, _ = Run(ctx, valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	if result.State != run.RunCancelled {
		t.Fatal(result.State)
	}
	for _, e := range recordedValues(result.Events) {
		if e.NodeID != "" {
			t.Fatalf("cancelled node recorded %#v", e)
		}
	}
}

// The graph contract has no identifier length cap; recording must not turn a
// valid long port name into an invalid event (appendEvent panics on one).
func TestValueRecordingAcceptsLongPortNames(t *testing.T) {
	out, in := "o"+strings.Repeat("u", 259), "i"+strings.Repeat("n", 259)
	def := definition(map[string]graph.NodeDefinition{
		"a": {Type: "test", Inputs: map[string]graph.PortDefinition{in: {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{out: {Type: graph.ValueTypeString, Required: true}}},
	})
	def.Inputs = map[string]graph.InputDefinition{in: {Type: graph.ValueTypeString, Required: true}}
	def.GraphInputBindings = []graph.GraphInputBindingDefinition{{From: graph.GraphInputReference("inputs." + in), To: graph.NodeInputReference("a.inputs." + in)}}
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return map[string]graph.Value{out: graph.NewStringValue("done")}, nil
	})
	result, err := Run(context.Background(), def, map[string]graph.Value{in: graph.NewStringValue("go")}, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %v %v", result.State, err)
	}
	got := map[string]string{}
	for _, e := range recordedValues(result.Events) {
		p := valuePayload(t, e)
		got[p.Port] = *p.Value
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var back events.Event
		if err := json.Unmarshal(data, &back); err != nil || back.Payload.(events.ValueRecordedPayload).Port != p.Port {
			t.Fatalf("round trip: %v", err)
		}
	}
	if len(got) != 2 || got[out] != "done" || got["inputs."+in] != "go" {
		t.Fatalf("values = %v", got)
	}
}

func TestValueRecordingOmitsJSONWhoseStringsHoldASecret(t *testing.T) {
	secret := `q"b\s&<é`
	marshalled, _ := json.Marshal(secret) // "q\"b\\s&<é"
	for _, doc := range []string{
		`{"v":` + string(marshalled) + `}`,
		`{"v":"q\"b\\s&<é"}`, // ASCII-only escapes
		`{"q\"b\\s&<é":1}`,   // as an object key, arbitrary escapes
		`["x q\"b\\s&<é y"]`, // inside a longer string
	} {
		exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
			if id == "a" {
				v, err := graph.NewJSONValue(json.RawMessage(doc))
				return map[string]graph.Value{"text": graph.NewStringValue("t"), "doc": v}, err
			}
			n, _ := graph.NewNumberValue("1")
			return map[string]graph.Value{"n": n, "ok": graph.NewBooleanValue(true)}, nil
		})
		result, err := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true, LogRedactions: []string{secret}})
		if err != nil || result.State != run.RunSucceeded {
			t.Fatalf("%s: run = %v %v", doc, result.State, err)
		}
		var found bool
		for _, e := range recordedValues(result.Events) {
			p := valuePayload(t, e)
			if e.NodeID != "a" || p.Port != "doc" {
				continue
			}
			found = true
			if p.Value != nil || !p.Truncated || !p.Redacted || p.SizeBytes != int64(len(doc)) || p.SHA256 != hexDigest(doc) {
				t.Fatalf("%s: payload = %#v", doc, p)
			}
		}
		if !found {
			t.Fatalf("%s: doc not recorded", doc)
		}
	}
	// A JSON value without the secret is still carried whole.
	result, _ := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, valueExecutor("t"), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true, LogRedactions: []string{secret}})
	for _, e := range recordedValues(result.Events) {
		if p := valuePayload(t, e); p.Port == "doc" && (p.Value == nil || *p.Value != `{"k":[1,2]}` || p.Redacted) {
			t.Fatalf("clean doc = %#v", p)
		}
	}
}

func TestValueRecordingRejectsNonUTF8RedactionsBeforeStart(t *testing.T) {
	result, err := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, valueExecutor("hi"), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true, LogRedactions: []string{"\xff\xfe"}})
	if err == nil || result.RunID != "" || len(result.Events) != 0 {
		t.Fatalf("invalid redactions started a recording run: %v", err)
	}
	if _, err := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("x")}, valueExecutor("hi"), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{"\xff\xfe"}}); err != nil {
		t.Fatalf("recording off must keep the existing byte-log behavior: %v", err)
	}
}

// F2.3 E5b.1: the renderer keys a recorded value by (node, port) and treats two
// node runs for one key as a contradiction, so the engine must never emit two
// value events for one key in a run.
func TestValueRecordingEmitsAtMostOneEventPerKey(t *testing.T) {
	result, err := Run(context.Background(), valueGraph(), map[string]graph.Value{"job": graph.NewStringValue("do it")}, valueExecutor("hi"), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, RecordValues: true})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %#v, %v", result, err)
	}
	seen := map[string]uint64{}
	for _, e := range recordedValues(result.Events) {
		key := string(e.NodeID) + "\x00" + valuePayload(t, e).Port
		if first, dup := seen[key]; dup {
			t.Fatalf("key %q recorded at sequences %d and %d", key, first, e.Sequence)
		}
		seen[key] = e.Sequence
	}
	if len(seen) != 5 {
		t.Fatalf("recorded %d keys, want the graph input job and the 4 accepted outputs of a and b", len(seen))
	}
}
