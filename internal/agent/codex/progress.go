package codex

import (
	"encoding/json"

	"agentplane/internal/domain/events"
)

// normalize projects exact supported JSONL public fields. Unknown fields,
// reasoning, arguments/results and command text never cross this boundary.
// Completed assistant text is not token streaming.
func (p *progress) normalize(kind string, record map[string]json.RawMessage) {
	if p.normalized == nil {
		return
	}
	emit := func(k events.Type, v events.Payload) {
		if (events.Event{Type: k, Payload: v}).Validate() == nil {
			p.normalized(k, v)
		}
	}
	switch kind {
	case "thread.started":
		emit(events.Status, events.StatusPayload{Code: "session_started"})
		return
	case "turn.started":
		emit(events.Status, events.StatusPayload{Code: "turn_started"})
		return
	case "turn.completed":
		emit(events.Status, events.StatusPayload{Code: "turn_completed"})
		return
	}
	if kind != "item.started" && kind != "item.completed" {
		return
	}
	var item map[string]json.RawMessage
	if json.Unmarshal(record["item"], &item) != nil || item == nil {
		return
	}
	text := func(raw json.RawMessage) string {
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return ""
		}
		return v
	}
	id, status := text(item["id"]), text(item["status"])
	switch text(item["type"]) {
	case "agent_message":
		if kind == "item.completed" {
			if public := text(item["text"]); public != "" { // Runtime redacts before splitting the bounded source record.
				p.normalized(events.TextDelta, events.TextDeltaPayload{SourceID: id, Text: public})
			}
		}
	case "command_execution":
		var code *int
		if raw, ok := item["exit_code"]; ok && json.Unmarshal(raw, &code) != nil {
			return
		}
		k := events.CommandStarted
		if kind == "item.completed" {
			k = events.CommandFinished
		}
		emit(k, events.CommandPayload{CommandID: id, Status: status, ExitCode: code})
	case "mcp_tool_call":
		k := events.ToolCallStarted
		if kind == "item.completed" {
			k = events.ToolCallFinished
		}
		emit(k, events.ToolCallPayload{CallID: id, Name: text(item["tool"]), Status: status})
	case "file_change":
		if kind != "item.completed" {
			return
		}
		var changes []map[string]json.RawMessage
		if json.Unmarshal(item["changes"], &changes) != nil {
			return
		}
		for _, c := range changes {
			emit(events.FileChanged, events.FileChangedPayload{Path: text(c["path"]), Action: text(c["kind"]), Provenance: "reported"})
		}
	}
}
