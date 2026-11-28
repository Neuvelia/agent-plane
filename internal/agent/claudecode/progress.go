package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"

	"agentplane/internal/agent"
	"agentplane/internal/agent/cliproc"
	"agentplane/internal/domain/events"
)

// maxTrackedTools bounds the table of tool calls awaiting a result.
const maxTrackedTools = 1024

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// fileTools are the tools whose successful result is projected as a reported
// file change; the value is the input field that carries the path.
// ASSUMED(spike): tool_use inputs name the path in these fields.
var fileTools = map[string]string{
	"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path",
}

const (
	statusRunning = "in_progress"
	statusDone    = "completed"
	statusFailed  = "failed"
)

// toolUse is the only data kept between a tool_use block and its result: the
// tool name and, for file tools, a confirmed workspace-relative path.
type toolUse struct{ name, path string }

// progress is a bounded private stream-json (JSONL) parser. Provider records,
// thinking, tool inputs/results and stderr never leave it: it projects only a
// fixed set of public fields into normalized events and keeps the validated
// final result.
//
// ASSUMED(spike): the record shapes below, from the F1.2b plan:
//
//	{"type":"system","subtype":"init",...}
//	{"type":"assistant","message":{"id":..,"content":[{"type":"text","text":..},
//	    {"type":"thinking",..},{"type":"tool_use","id":..,"name":..,"input":{..}}]}}
//	{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":..,"is_error":false,"content":..}]}}
//	{"type":"result","subtype":"success","is_error":false,"structured_output":{..}}
//
// A record larger than maxRecord is skipped without being buffered. Because a
// skipped record can never become the result, an oversized `result` leaves no
// result and fails closed in finish; an oversized record after a valid result
// is rejected as ambiguous, as is an unparseable or duplicate-key record.
type progress struct {
	workspaces Workspaces
	root       string
	normalized func(events.Type, events.Payload)
	cancel     context.CancelFunc
	pending    []byte
	draining   bool
	tools      map[string]toolUse
	result     *agent.AgentExecutionOutput
	err        error
}

func newProgress(workspaces Workspaces, root string, normalized func(events.Type, events.Payload), cancel context.CancelFunc) *progress {
	return &progress{workspaces: workspaces, root: root, normalized: normalized, cancel: cancel, tools: map[string]toolUse{}}
}

// Write splits the stream into lines. It always reports the full length so the
// child is never stopped by a slow or rejecting parser.
func (p *progress) Write(data []byte) (int, error) {
	n := len(data)
	for len(data) > 0 && p.err == nil {
		line, rest, found := bytes.Cut(data, []byte{'\n'})
		p.accumulate(line)
		if !found {
			break
		}
		p.endLine()
		data = rest
	}
	return n, nil
}

func (p *progress) accumulate(line []byte) {
	if !p.draining && len(p.pending)+len(line) > maxRecord {
		p.pending = nil
		p.draining = true
		p.skipOversized()
	}
	if !p.draining {
		p.pending = append(p.pending, line...)
	}
}

func (p *progress) endLine() {
	if !p.draining {
		p.record(p.pending)
	}
	p.pending = nil
	p.draining = false
}

func (p *progress) skipOversized() {
	if p.result != nil {
		p.reject(fail("result", "oversized record after the final result is ambiguous", nil))
	}
}

// unparseable skips a record that is not one unambiguous JSON object. After a
// valid result it is as ambiguous as an oversized record and is rejected.
func (p *progress) unparseable() {
	if p.result != nil {
		p.reject(fail("result", "unparseable record after the final result is ambiguous", nil))
	}
}

func (p *progress) reject(err error) {
	if p.err != nil {
		return
	}
	p.err = err
	if p.cancel != nil {
		p.cancel()
	}
}

// finish flushes an unterminated last record and requires a valid result.
func (p *progress) finish() error {
	if len(p.pending) > 0 && !p.draining && p.err == nil {
		p.record(p.pending)
	}
	p.pending = nil
	if p.err != nil {
		return p.err
	}
	if p.result == nil {
		return fail("result", "missing or oversized final result record", nil)
	}
	return nil
}

func (p *progress) record(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	var record map[string]json.RawMessage
	if cliproc.UniqueJSON(line) != nil || json.Unmarshal(line, &record) != nil || record == nil {
		p.unparseable()
		return
	}
	switch text(record["type"]) {
	case "system":
		if text(record["subtype"]) == "init" {
			p.emit(events.Status, events.StatusPayload{Code: "session_started"})
		}
	case "assistant":
		p.assistant(record["message"])
	case "user":
		p.user(record["message"])
	case "result":
		p.finalResult(record)
	}
}

func (p *progress) emit(kind events.Type, payload events.Payload) {
	if p.normalized != nil && (events.Event{Type: kind, Payload: payload}).Validate() == nil {
		p.normalized(kind, payload)
	}
}

func (p *progress) assistant(raw json.RawMessage) {
	message, blocks := messageBlocks(raw)
	for _, block := range blocks {
		switch text(block["type"]) {
		case "text":
			if public := text(block["text"]); public != "" {
				p.emit(events.TextDelta, events.TextDeltaPayload{SourceID: text(message["id"]), Text: public})
			}
		case "tool_use":
			p.toolStarted(block)
		}
		// thinking and every other block type are private and skipped.
	}
}

func (p *progress) user(raw json.RawMessage) {
	_, blocks := messageBlocks(raw)
	for _, block := range blocks {
		if text(block["type"]) == "tool_result" {
			p.toolFinished(block)
		}
	}
}

func (p *progress) toolStarted(block map[string]json.RawMessage) {
	id, name := text(block["id"]), text(block["name"])
	if id == "" || len(id) > 128 || !toolNamePattern.MatchString(name) || len(p.tools) >= maxTrackedTools {
		return
	}
	tool := toolUse{name: name}
	if field, ok := fileTools[name]; ok {
		tool.path = p.reportedPath(block["input"], field)
	}
	p.tools[id] = tool
	switch {
	case name == "Bash":
		p.emit(events.CommandStarted, events.CommandPayload{CommandID: id, Status: statusRunning})
	case !isFileTool(name):
		p.emit(events.ToolCallStarted, events.ToolCallPayload{CallID: id, Name: name, Status: statusRunning})
	}
}

func (p *progress) toolFinished(block map[string]json.RawMessage) {
	id := text(block["tool_use_id"])
	tool, ok := p.tools[id]
	if !ok {
		return
	}
	delete(p.tools, id)
	status := statusDone
	if resultFailed(block) {
		status = statusFailed
	}
	switch {
	case tool.name == "Bash":
		p.emit(events.CommandFinished, events.CommandPayload{CommandID: id, Status: status})
	case isFileTool(tool.name):
		p.fileChanged(tool, status)
	default:
		p.emit(events.ToolCallFinished, events.ToolCallPayload{CallID: id, Name: tool.name, Status: status})
	}
}

// fileChanged projects a successful file-tool result. The action is always
// "update" because the real kind is not reliably known; Git evidence decides.
func (p *progress) fileChanged(tool toolUse, status string) {
	if status == statusDone && tool.path != "" {
		p.emit(events.FileChanged, events.FileChangedPayload{Path: tool.path, Action: "update", Provenance: "reported"})
	}
}

// reportedPath extracts the tool's target path and keeps it only when it is
// confirmed to lie inside the workspace.
func (p *progress) reportedPath(rawInput json.RawMessage, field string) string {
	var input map[string]json.RawMessage
	if json.Unmarshal(rawInput, &input) != nil {
		return ""
	}
	reported := text(input[field])
	if reported == "" {
		return ""
	}
	rel, ok := workspaceRelative(p.workspaces, p.root, reported)
	if !ok {
		return ""
	}
	return rel
}

// finalResult records the single successful result. A failure subtype, an
// error flag, a duplicate or an invalid structured output fails the
// invocation with a safe code; the provider's message is never read.
func (p *progress) finalResult(record map[string]json.RawMessage) {
	if p.result != nil {
		p.reject(fail("result", "duplicate final result record", nil))
		return
	}
	if text(record["subtype"]) != "success" || flagSet(record["is_error"]) {
		p.reject(fail("result", "Claude Code reported execution failure; details withheld", nil))
		return
	}
	out, err := parseOutput(record["structured_output"])
	if err != nil {
		p.reject(err)
		return
	}
	p.result = &out
	p.emit(events.Status, events.StatusPayload{Code: "turn_completed"})
}

func isFileTool(name string) bool {
	_, ok := fileTools[name]
	return ok
}

// messageBlocks returns the message object and its content blocks; a string
// content or any other shape yields no blocks.
func messageBlocks(raw json.RawMessage) (map[string]json.RawMessage, []map[string]json.RawMessage) {
	var message map[string]json.RawMessage
	if json.Unmarshal(raw, &message) != nil {
		return nil, nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(message["content"], &blocks) != nil {
		return message, nil
	}
	return message, blocks
}

// text returns a JSON string value or "" for any other type.
func text(raw json.RawMessage) string {
	var v string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v
}

// flagSet is true for JSON true; absent or false is false. Any non-boolean
// value is treated as set so ambiguity fails closed.
func flagSet(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var v bool
	return json.Unmarshal(raw, &v) != nil || v
}

// resultFailed reports whether a tool_result block marks an error.
func resultFailed(block map[string]json.RawMessage) bool { return flagSet(block["is_error"]) }
