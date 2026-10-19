package events

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestProgressSchemaCompatibility(t *testing.T) {
	cases := []struct {
		kind    Type
		payload Payload
	}{
		{TextDelta, TextDeltaPayload{SourceID: "item", Text: "hello"}},
		{ToolCallStarted, ToolCallPayload{CallID: "call", Name: "tool", Status: "in_progress"}},
		{ToolCallFinished, ToolCallPayload{CallID: "call", Name: "tool", Status: "completed"}},
		{CommandStarted, CommandPayload{CommandID: "cmd", Status: "in_progress"}},
		{CommandFinished, CommandPayload{CommandID: "cmd", Status: "completed"}},
		{FileChanged, FileChangedPayload{Path: "a.go", Action: "update", Provenance: "reported"}},
		{Status, StatusPayload{Code: "turn_started"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			e := Event{SchemaVersion: 3, Type: tc.kind, Payload: tc.payload, RunID: "run", NodeID: "node", NodeRunID: "nr"}
			b, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			var got Event
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
	live := make(chan Event)
	c := NewCollectorWithLiveEvents(context.Background(), "run", "graph", "version", live)
	chunk := strings.Repeat("x", 16<<10)
	for i := 0; i < 6400; i++ {
		if _, err := c.Append(Event{Type: TextDelta, Payload: TextDeltaPayload{Text: chunk}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Append(Event{Type: NodeFinished, Payload: NodeFinishedPayload{Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}}); err != nil {
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
	if es[len(es)-1].Type != RunFinished || es[len(es)-2].Type != ProgressOmitted || es[len(es)-3].Type != NodeFinished {
		t.Fatal("lost terminal or omission facts")
	}
	p := es[len(es)-2].Payload.(ProgressOmittedPayload)
	if p.RetainedEvents != s.RetainedOmittedEvents || p.LiveEvents != s.LiveOmittedEvents {
		t.Fatal("inaccurate marker")
	}
}

func TestSlowReaderSealsLivePrefixAndItemBounds(t *testing.T) {
	live := make(chan Event)
	c := NewCollectorWithLiveEvents(context.Background(), "r", "g", "v", live)
	c.Append(Event{Type: RunStarted, Payload: RunStartedPayload{}})
	first := <-live
	for range 5000 {
		c.Append(Event{Type: Status, Payload: StatusPayload{Code: "working"}})
	}
	c.Append(Event{Type: NodeFinished, Payload: NodeFinishedPayload{Status: "succeeded"}})
	c.Append(Event{Type: RunFinished, Payload: RunFinishedPayload{Status: "succeeded"}})
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
	if es[len(es)-3].Type != NodeFinished || es[len(es)-3].Sequence <= MaxRetainedProgressItems+1 {
		t.Fatal("lost lifecycle after sequence gap")
	}
	t.Logf("item-bound high-water and losses: %+v", s)
}

func TestProgressValidationAndCommandCopy(t *testing.T) {
	for _, e := range []Event{
		{Type: Status, Payload: StatusPayload{Code: "raw error"}},
		{Type: TextDelta, Payload: TextDeltaPayload{Text: strings.Repeat("x", MaxProgressContent+1)}},
		{Type: FileChanged, Payload: FileChangedPayload{Path: "a", Action: "update", Provenance: "observed"}},
	} {
		if e.Validate() == nil {
			t.Fatalf("invalid progress accepted: %+v", e)
		}
	}
	c := NewCollector("r", "g", "v")
	code := 0
	returned, err := c.Append(Event{Type: CommandFinished, Payload: CommandPayload{CommandID: "c", Status: "completed", ExitCode: &code}})
	if err != nil {
		t.Fatal(err)
	}
	code = 12
	*returned.Payload.(CommandPayload).ExitCode = 24
	snapshot := c.Snapshot()
	*snapshot[0].Payload.(CommandPayload).ExitCode = 36
	if *c.Snapshot()[0].Payload.(CommandPayload).ExitCode != 0 {
		t.Fatal("mutable command exit code")
	}
}

func TestLegacySchemasDecodeExistingShapes(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		var e Event
		if err := json.Unmarshal([]byte(`{"event_schema_version":`+version+`,"type":"log_chunk","payload":{"stream":"stdout","data":"eA=="}}`), &e); err != nil {
			t.Fatal(err)
		}
		if string(e.Payload.(LogChunkPayload).Data) != "x" {
			t.Fatal("legacy log changed")
		}
		if err := json.Unmarshal([]byte(`{"event_schema_version":`+version+`,"type":"text_delta","payload":{"text":"x"}}`), &e); err == nil {
			t.Fatal("new type accepted under old schema")
		}
	}
}

func TestNormalizedProgressRejectsInvalidUTF8Strings(t *testing.T) {
	bad := string([]byte{0x80})
	for _, e := range []Event{
		{Type: TextDelta, Payload: TextDeltaPayload{Text: bad}},
		{Type: TextDelta, Payload: TextDeltaPayload{SourceID: bad, Text: "safe"}},
		{Type: ToolCallStarted, Payload: ToolCallPayload{CallID: bad, Name: "tool", Status: "in_progress"}},
		{Type: ToolCallFinished, Payload: ToolCallPayload{CallID: "c", Name: bad, Status: "completed"}},
		{Type: CommandStarted, Payload: CommandPayload{CommandID: bad, Status: "in_progress"}},
		{Type: CommandFinished, Payload: CommandPayload{CommandID: "c", Status: bad}},
		{Type: FileChanged, Payload: FileChangedPayload{Path: bad, Action: "add", Provenance: "reported"}},
		{Type: FileChanged, Payload: FileChangedPayload{Path: "a", Action: bad, Provenance: "reported"}},
		{Type: FileChanged, Payload: FileChangedPayload{Path: "a", Action: "add", Provenance: bad}},
		{Type: Status, Payload: StatusPayload{Code: bad}},
	} {
		if err := e.Validate(); err == nil || !strings.Contains(err.Error(), "UTF-8") {
			t.Fatalf("missing UTF-8 validation: %v", err)
		}
		if _, err := json.Marshal(e); err == nil {
			t.Fatal("invalid string silently normalized during JSON encoding")
		}
	}
}
