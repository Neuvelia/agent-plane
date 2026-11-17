package trace

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/domain/events"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestProgressSchemaCompatibility(t *testing.T) {
	cases := []struct {
		kind    events.Type
		payload events.Payload
	}{
		{events.TextDelta, events.TextDeltaPayload{SourceID: "item", Text: "hello"}},
		{events.ToolCallStarted, events.ToolCallPayload{CallID: "call", Name: "tool", Status: "in_progress"}},
		{events.ToolCallFinished, events.ToolCallPayload{CallID: "call", Name: "tool", Status: "completed"}},
		{events.CommandStarted, events.CommandPayload{CommandID: "cmd", Status: "in_progress"}},
		{events.CommandFinished, events.CommandPayload{CommandID: "cmd", Status: "completed"}},
		{events.FileChanged, events.FileChangedPayload{Path: "a.go", Action: "update", Provenance: "reported"}},
		{events.Status, events.StatusPayload{Code: "turn_started"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			e := events.Event{SchemaVersion: 3, Type: tc.kind, Payload: tc.payload, RunID: "run", NodeID: "node", NodeRunID: "nr"}
			b, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			var got events.Event
			if err = json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if got.Type != e.Type || got.RunID != e.RunID || got.NodeRunID != e.NodeRunID || got.NodeID != e.NodeID || got.AttemptID != "" {
				t.Fatalf("lost correlation: %+v", got)
			}
			for _, v := range []int{1, 2} {
				e.SchemaVersion = v
				if _, err = json.Marshal(e); err == nil {
					t.Fatal("old schema accepted new payload")
				}
			}
		})
	}
}

func TestBoundedProgressAbsentReader100MiB(t *testing.T) {
	live := make(chan events.Event)
	c := NewCollectorWithLiveEvents(context.Background(), system.Clock{}, "run", "graph", "version", live)
	chunk := strings.Repeat("x", 16<<10)
	for i := 0; i < 6400; i++ {
		if _, err := c.Append(events.Event{Type: events.TextDelta, Payload: events.TextDeltaPayload{Text: chunk}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Append(events.Event{Type: events.NodeFinished, Payload: events.NodeFinishedPayload{Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	close(live) // Run/collector finalization must have joined the delivery pump.
	s := c.Stats()
	t.Logf("100MiB high-water and losses: %+v", s)
	if s.RetainedPeakBytes > MaxRetainedProgressBytes || s.RetainedPeakItems > MaxRetainedProgressItems || s.LivePeakBytes > MaxLiveBytes || s.LivePeakItems > MaxLiveItems {
		t.Fatalf("bounds exceeded: %+v", s)
	}
	if s.RetainedOmittedEvents == 0 || s.LiveOmittedEvents == 0 {
		t.Fatalf("missing loss: %+v", s)
	}
	es := c.Snapshot()
	if es[len(es)-1].Type != events.RunFinished || es[len(es)-2].Type != events.ProgressOmitted || es[len(es)-3].Type != events.NodeFinished {
		t.Fatal("lost terminal or omission facts")
	}
	p := es[len(es)-2].Payload.(events.ProgressOmittedPayload)
	if p.RetainedEvents != s.RetainedOmittedEvents || p.LiveEvents != s.LiveOmittedEvents {
		t.Fatal("inaccurate marker")
	}
}

func TestSlowReaderSealsLivePrefixAndItemBounds(t *testing.T) {
	live := make(chan events.Event)
	c := NewCollectorWithLiveEvents(context.Background(), system.Clock{}, "r", "g", "v", live)
	c.Append(events.Event{Type: events.RunStarted, Payload: events.RunStartedPayload{}})
	first := <-live
	for range 5000 {
		c.Append(events.Event{Type: events.Status, Payload: events.StatusPayload{Code: "working"}})
	}
	c.Append(events.Event{Type: events.NodeFinished, Payload: events.NodeFinishedPayload{Status: "succeeded"}})
	c.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}})
	close(live)
	if _, ok := <-live; ok {
		t.Fatal("delivery continued after joined pump")
	}
	s := c.Stats()
	if s.LivePeakItems != MaxLiveItems || s.RetainedPeakItems != MaxRetainedProgressItems {
		t.Fatalf("item bounds not exercised: %+v", s)
	}
	es := c.Snapshot()
	if first.Sequence != 1 || es[0].Sequence != 1 {
		t.Fatal("prefix changed")
	}
	var last uint64
	for _, e := range es {
		if e.Sequence <= last {
			t.Fatal("nonmonotonic retained order")
		}
		last = e.Sequence
	}
	if es[len(es)-3].Type != events.NodeFinished || es[len(es)-3].Sequence <= MaxRetainedProgressItems+1 {
		t.Fatal("lost lifecycle after sequence gap")
	}
	t.Logf("item-bound high-water and losses: %+v", s)
}

func TestProgressValidationAndCommandCopy(t *testing.T) {
	for _, e := range []events.Event{
		{Type: events.Status, Payload: events.StatusPayload{Code: "raw error"}},
		{Type: events.TextDelta, Payload: events.TextDeltaPayload{Text: strings.Repeat("x", events.MaxProgressContent+1)}},
		{Type: events.FileChanged, Payload: events.FileChangedPayload{Path: "a", Action: "update", Provenance: "observed"}},
	} {
		if e.Validate() == nil {
			t.Fatalf("invalid progress accepted: %+v", e)
		}
	}
	c := NewCollector(system.Clock{}, "r", "g", "v")
	code := 0
	returned, err := c.Append(events.Event{Type: events.CommandFinished, Payload: events.CommandPayload{CommandID: "c", Status: "completed", ExitCode: &code}})
	if err != nil {
		t.Fatal(err)
	}
	code = 12
	*returned.Payload.(events.CommandPayload).ExitCode = 24
	snapshot := c.Snapshot()
	*snapshot[0].Payload.(events.CommandPayload).ExitCode = 36
	if *c.Snapshot()[0].Payload.(events.CommandPayload).ExitCode != 0 {
		t.Fatal("mutable command exit code")
	}
}

func TestLegacySchemasDecodeExistingShapes(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		var e events.Event
		if err := json.Unmarshal([]byte(`{"event_schema_version":`+version+`,"type":"log_chunk","payload":{"stream":"stdout","data":"eA=="}}`), &e); err != nil {
			t.Fatal(err)
		}
		if string(e.Payload.(events.LogChunkPayload).Data) != "x" {
			t.Fatal("legacy log changed")
		}
		if err := json.Unmarshal([]byte(`{"event_schema_version":`+version+`,"type":"text_delta","payload":{"text":"x"}}`), &e); err == nil {
			t.Fatal("new type accepted under old schema")
		}
	}
}

func TestNormalizedProgressRejectsInvalidUTF8Strings(t *testing.T) {
	bad := string([]byte{0x80})
	for _, e := range []events.Event{
		{Type: events.TextDelta, Payload: events.TextDeltaPayload{Text: bad}},
		{Type: events.TextDelta, Payload: events.TextDeltaPayload{SourceID: bad, Text: "safe"}},
		{Type: events.ToolCallStarted, Payload: events.ToolCallPayload{CallID: bad, Name: "tool", Status: "in_progress"}},
		{Type: events.ToolCallFinished, Payload: events.ToolCallPayload{CallID: "c", Name: bad, Status: "completed"}},
		{Type: events.CommandStarted, Payload: events.CommandPayload{CommandID: bad, Status: "in_progress"}},
		{Type: events.CommandFinished, Payload: events.CommandPayload{CommandID: "c", Status: bad}},
		{Type: events.FileChanged, Payload: events.FileChangedPayload{Path: bad, Action: "add", Provenance: "reported"}},
		{Type: events.FileChanged, Payload: events.FileChangedPayload{Path: "a", Action: bad, Provenance: "reported"}},
		{Type: events.FileChanged, Payload: events.FileChangedPayload{Path: "a", Action: "add", Provenance: bad}},
		{Type: events.Status, Payload: events.StatusPayload{Code: bad}},
	} {
		if err := e.Validate(); err == nil || !strings.Contains(err.Error(), "UTF-8") {
			t.Fatalf("missing UTF-8 validation: %v", err)
		}
		if _, err := json.Marshal(e); err == nil {
			t.Fatal("invalid string silently normalized during JSON encoding")
		}
	}
}
