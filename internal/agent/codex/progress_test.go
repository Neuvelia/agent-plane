package codex

import (
	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"context"
	"strings"
	"testing"
)

// Supported shapes are documented by the official non-interactive-mode JSONL
// example: https://learn.chatgpt.com/docs/non-interactive-mode . Public item
// fields alone are projected; command contents and reasoning are excluded.
func TestNormalizedSupportedRecordsRecoverMalformed(t *testing.T) {
	var got []events.Event
	p := progress{normalized: func(k events.Type, v events.Payload) { got = append(got, events.Event{Type: k, Payload: v}) }}
	records := []string{
		`{broken`,
		`{"type":"item.completed","type":"evil"}`,
		strings.Repeat("x", maxRecord+20),
		`{"type":"thread.started","thread_id":"private"}`,
		`{"type":"item.started","item":{"id":"c","type":"command_execution","command":"SECRET","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"c","type":"command_execution","command":"SECRET","aggregated_output":"SECRET","exit_code":0,"status":"completed"}}`,
		`{"type":"item.started","item":{"id":"m","type":"mcp_tool_call","server":"server","tool":"search","arguments":{"secret":"SECRET"},"status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"server","tool":"search","result":"SECRET","status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"f","type":"file_change","changes":[{"path":"a.go","kind":"update"}],"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"r","type":"reasoning","text":"SECRET"}}`,
		`{"type":"unknown","secret":"SECRET"}`,
		`{"type":"item.completed","item":{"id":"a","type":"agent_message","text":"public"}}`,
	}
	for _, r := range records {
		for i := 0; i < len(r); {
			n := i + 997
			if n > len(r) {
				n = len(r)
			}
			p.Write([]byte(r[i:n]))
			i = n
		}
		p.Write([]byte("\n"))
	}
	if err := p.finish(); err != nil {
		t.Fatal(err)
	}
	want := []events.Type{events.Status, events.CommandStarted, events.CommandFinished, events.ToolCallStarted, events.ToolCallFinished, events.FileChanged, events.TextDelta}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, e := range got {
		if e.Type != want[i] {
			t.Fatalf("%d: %s", i, e.Type)
		}
	}
}

func TestMalformedOptionalProgressStillAcceptsValidFinalResult(t *testing.T) {
	a := Adapter{Binary: fake(t, `printf '{broken\n'
printf '{"type":"item.completed","type":"duplicate"}\n'
/usr/bin/head -c 1048600 /dev/zero
printf '\n{"type":"item.completed","item":{"type":"agent_message","id":"a","text":"public"}}\n'
printf '{"summary":"done","artifacts":[]}' >"$result"
`)}
	var text string
	out, err := a.ExecuteWithNormalizedProgress(context.Background(), agent.AgentExecutionInput{Workspace: repo(t), Task: "PRIVATE_TASK"}, func(_ events.Type, p events.Payload) {
		if v, ok := p.(events.TextDeltaPayload); ok {
			text += v.Text
		}
	})
	if err != nil || out.Summary != "done" || text != "public" {
		t.Fatalf("out=%+v text=%q err=%v", out, text, err)
	}
}

func TestNormalizerRejectsCaseAliasesAndRawStatuses(t *testing.T) {
	var got []events.Payload
	p := progress{normalized: func(_ events.Type, v events.Payload) { got = append(got, v) }}
	for _, line := range []string{
		`{"type":"item.completed","item":{"Type":"agent_message","Text":"PRIVATE_UNKNOWN"}}`,
		`{"type":"item.completed","item":{"type":"command_execution","id":"c","status":"PRIVATE_ERROR"}}`,
		`{"type":"item.completed","item":{"type":"mcp_tool_call","id":"m","tool":"tool","status":"PRIVATE_ERROR"}}`,
		`{"type":"item.completed","item":{"type":"file_change","changes":[{"Path":"PRIVATE_UNKNOWN","kind":"update"}]}}`,
	} {
		p.record([]byte(line))
	}
	if len(got) != 0 {
		t.Fatalf("unknown alias/raw error projected: %+v", got)
	}
}
