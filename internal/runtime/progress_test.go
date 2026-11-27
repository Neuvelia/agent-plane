package runtime

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/app/trace"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type typedExecutor struct{ emit func(ExecutionObservation) }

func (e typedExecutor) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	return nil, nil
}
func (e typedExecutor) ExecuteNodeObserved(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, o ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	e.emit(o)
	return nil, nil, nil
}
func TestTypedProgressRedactionEveryBoundary(t *testing.T) {
	secret := "boundary-secret"
	for i := 1; i < len(secret); i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			var late func(events.Type, events.Payload)
			ex := typedExecutor{func(o ExecutionObservation) {
				late = o.Progress
				o.Progress(events.TextDelta, events.TextDeltaPayload{SourceID: secret, Text: "prefix " + secret[:i]})
				o.Progress(events.TextDelta, events.TextDeltaPayload{SourceID: secret, Text: secret[i:] + " end"})
				o.Progress(events.ToolCallStarted, events.ToolCallPayload{CallID: secret, Name: secret, Status: "in_progress"})
			}}
			d := definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}})
			r, err := Run(context.Background(), d, nil, ex, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{secret}})
			if err != nil {
				t.Fatal(err)
			}
			late(events.TextDelta, events.TextDeltaPayload{Text: "LATE"})
			b, _ := json.Marshal(r.Events)
			if strings.Contains(string(b), secret) || strings.Contains(string(b), "LATE") {
				t.Fatal("secret or late callback exposed")
			}
			var text string
			for _, e := range r.Events {
				if p, ok := e.Payload.(events.TextDeltaPayload); ok {
					text += p.Text
				}
			}
			if strings.Contains(text, secret) || !strings.Contains(text, "prefix ") || !strings.Contains(text, " end") {
				t.Fatalf("redaction lost text: %q", text)
			}
		})
	}
}

func TestNormalizedProgressOrderAndCorrelation(t *testing.T) {
	kinds := []events.Type{events.TextDelta, events.ToolCallStarted, events.ToolCallFinished, events.CommandStarted, events.CommandFinished, events.FileChanged, events.Status}
	payloads := []events.Payload{events.TextDeltaPayload{Text: "public"}, events.ToolCallPayload{CallID: "c", Name: "tool", Status: "in_progress"}, events.ToolCallPayload{CallID: "c", Name: "tool", Status: "completed"}, events.CommandPayload{CommandID: "c", Status: "in_progress"}, events.CommandPayload{CommandID: "c", Status: "completed"}, events.FileChangedPayload{Path: "a", Action: "add", Provenance: "reported"}, events.StatusPayload{Code: "working"}}
	ex := typedExecutor{func(o ExecutionObservation) {
		for i, k := range kinds {
			o.Progress(k, payloads[i])
		}
	}}
	r, e := Run(context.Background(), definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}}), nil, ex, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if e != nil {
		t.Fatal(e)
	}
	var found int
	for _, ev := range r.Events {
		if !events.IsNormalized(ev.Type) {
			continue
		}
		if ev.Type != kinds[found] || ev.RunID != r.RunID || ev.NodeID != "n" || ev.NodeRunID != r.Nodes[0].NodeRunID || ev.AttemptID != "" {
			t.Fatalf("unexpected event %+v", ev)
		}
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		var decoded events.Event
		if json.Unmarshal(b, &decoded) != nil {
			t.Fatal("JSON failed")
		}
		found++
	}
	if found != len(kinds) {
		t.Fatalf("missing types: %d", found)
	}
}

// roadmap: F1.3-AC2
func TestRun100MiBAbsentReaderAndFullQueueCancellation(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "cancel"}[cancelRun], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			full := make(chan struct{})
			release := make(chan struct{})
			ex := typedExecutor{func(o ExecutionObservation) {
				chunk := strings.Repeat("x", 16<<10)
				for range 6400 {
					o.Progress(events.TextDelta, events.TextDeltaPayload{Text: chunk})
				}
				close(full)
				<-release
			}}
			live := make(chan events.Event)
			done := make(chan run.Result, 1)
			go func() {
				r, e := Run(ctx, definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}}), nil, ex, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LiveEvents: live})
				if e != nil {
					panic(e)
				}
				done <- r
			}()
			<-full
			if cancelRun {
				cancel()
			}
			close(release)
			r := <-done
			close(live)
			if cancelRun && r.State != run.RunCancelled {
				t.Fatalf("state %s", r.State)
			}
			s := r.ProgressStats
			t.Logf("100MiB high-water and losses: %+v", s)
			if s.LivePeakBytes > trace.MaxLiveBytes || s.LivePeakItems > trace.MaxLiveItems || s.RetainedPeakBytes > trace.MaxRetainedProgressBytes || s.RetainedPeakItems > trace.MaxRetainedProgressItems || s.RetainedOmittedEvents == 0 || s.LiveOmittedEvents == 0 {
				t.Fatalf("bounds/losses %+v", s)
			}
			if r.Events[len(r.Events)-1].Type != events.RunFinished || r.Events[len(r.Events)-2].Type != events.ProgressOmitted {
				t.Fatal("missing terminal/omission")
			}
			found := false
			for _, e := range r.Events {
				if e.Type == events.NodeFinished {
					found = true
				}
			}
			if !found {
				t.Fatal("missing node terminal")
			}
		})
	}
}

func TestProgressUnicodeSplittingPreservesPublicText(t *testing.T) {
	text := strings.Repeat("界", 12000)
	ex := typedExecutor{func(o ExecutionObservation) { o.Progress(events.TextDelta, events.TextDeltaPayload{Text: text}) }}
	r, e := Run(context.Background(), definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}}), nil, ex, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{"unrelated-secret"}})
	if e != nil {
		t.Fatal(e)
	}
	var got string
	for _, ev := range r.Events {
		if _, ok := ev.Payload.(events.TextDeltaPayload); ok {
			b, _ := json.Marshal(ev)
			var decoded events.Event
			if json.Unmarshal(b, &decoded) != nil {
				t.Fatal("invalid JSON")
			}
			got += decoded.Payload.(events.TextDeltaPayload).Text
		}
	}
	if got != text {
		t.Fatal("unicode progress corrupted by split/redaction buffering")
	}
}

func TestProgressSourceStateBoundAndRecovery(t *testing.T) {
	template := mustLogRedactor(t, []string{"secret"})
	var eventsSeen []events.Event
	p := newProgressRedactor(template, func(k events.Type, v events.Payload) {
		eventsSeen = append(eventsSeen, events.Event{Type: k, Payload: v})
	})
	for i := 0; i < maxTextSources; i++ {
		p.write(events.TextDelta, events.TextDeltaPayload{SourceID: string(rune('A' + i)), Text: "s"})
	}
	for range 1000 {
		p.write(events.TextDelta, events.TextDeltaPayload{SourceID: "rejected", Text: "secret"})
	}
	if len(p.streams) != maxTextSources {
		t.Fatalf("unbounded source state %d", len(p.streams))
	}
	var notices int
	for _, e := range eventsSeen {
		if v, ok := e.Payload.(events.StatusPayload); ok && v.Code == "progress_limited" {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("admission notices %d", notices)
	}
	p.write(events.TextDelta, events.TextDeltaPayload{SourceID: "A", Text: "ecret!"})
	if len(p.streams) != maxTextSources-1 {
		t.Fatal("resolved source not released")
	}
	p.write(events.TextDelta, events.TextDeltaPayload{SourceID: "rejected", Text: "public!"})
	found := false
	for _, e := range eventsSeen {
		if v, ok := e.Payload.(events.TextDeltaPayload); ok && v.SourceID == "rejected" && v.Text == "public!" {
			found = true
		}
	}
	if !found {
		t.Fatal("admission did not recover")
	}
	// Ordinary completed sources do not accumulate identities.
	for i := 0; i < 1000; i++ {
		p.write(events.TextDelta, events.TextDeltaPayload{SourceID: strings.Repeat("x", i+1), Text: "public!"})
	}
	if len(p.streams) != maxTextSources-1 {
		t.Fatal("completed sources accumulated")
	}
}

func TestNormalizedRedactionRejectsUnsafeUTF8BeforeExecution(t *testing.T) {
	ascii := make([]byte, 128)
	for i := range ascii {
		ascii[i] = byte(i)
	}
	for _, tc := range []struct {
		redactions []string
		text       string
	}{{[]string{string(ascii), "SENSITIVE", "\uFFFD"}, "SENSITIVE"}, {[]string{string([]byte{0xa9}), "\uFFFD"}, "é"}} {
		called := false
		ex := typedExecutor{func(o ExecutionObservation) {
			called = true
			o.Progress(events.TextDelta, events.TextDeltaPayload{Text: tc.text})
		}}
		result, err := Run(context.Background(), definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}}), nil, ex, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: tc.redactions})
		if err == nil {
			encoded, marshalErr := json.Marshal(result.Events)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			var decoded []events.Event
			if decodeErr := json.Unmarshal(encoded, &decoded); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			for _, e := range decoded {
				if v, ok := e.Payload.(events.TextDeltaPayload); ok && strings.Contains(v.Text, "\uFFFD") {
					t.Fatalf("JSON conversion disclosed configured U+FFFD secret: %q", v.Text)
				}
			}
			t.Fatal("unsafe normalized redactions accepted")
		}
		if called || result.RunID != "" || len(result.Events) != 0 {
			t.Fatal("unsafe configuration started execution")
		}
	}
}

func TestLegacyByteLogsKeepNonUTF8MarkerCompatibility(t *testing.T) {
	ascii := make([]byte, 128)
	for i := range ascii {
		ascii[i] = byte(i)
	}
	ex := streamingExecutorFunc(func(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, log func(string, []byte)) (map[string]graph.Value, error) {
		log("stdout", []byte("SENSITIVE"))
		return nil, nil
	})
	result, err := Run(context.Background(), definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}}), nil, ex, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{string(ascii), "SENSITIVE", "\uFFFD"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Events)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []events.Event
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for _, e := range decoded {
		if v, ok := e.Payload.(events.LogChunkPayload); ok {
			data = append(data, v.Data...)
		}
	}
	if string(data) != string([]byte{0x80, 0x80, 0x80}) {
		t.Fatalf("legacy byte marker changed: %x", data)
	}
}

func TestNormalizedUTF8SecretsStayRedactedAfterJSON(t *testing.T) {
	ex := typedExecutor{func(o ExecutionObservation) {
		o.Progress(events.TextDelta, events.TextDeltaPayload{Text: "before é and SENSITIVE \uFFFD after"})
	}}
	result, err := Run(context.Background(), definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}}), nil, ex, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{"SENSITIVE", "\uFFFD"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Events)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []events.Event
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	var text string
	for _, e := range decoded {
		if v, ok := e.Payload.(events.TextDeltaPayload); ok {
			text += v.Text
		}
	}
	if text != "before é and *** *** after" {
		t.Fatalf("serialized redaction changed: %q", text)
	}
}
