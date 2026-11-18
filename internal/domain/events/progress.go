package events

import (
	"fmt"
	"unicode/utf8"
)

const (
	TextDelta          Type = "text_delta"
	ToolCallStarted    Type = "tool_call_started"
	ToolCallFinished   Type = "tool_call_finished"
	CommandStarted     Type = "command_started"
	CommandFinished    Type = "command_finished"
	FileChanged        Type = "file_changed"
	Status             Type = "status"
	ProgressOmitted    Type = "progress_omitted"
	MaxProgressContent      = 16 << 10
)

type TextDeltaPayload struct {
	SourceID string `json:"source_id,omitempty"`
	Text     string `json:"text"`
}
type ToolCallPayload struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
type CommandPayload struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}
type FileChangedPayload struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	Provenance string `json:"provenance"`
}
type StatusPayload struct {
	Code string `json:"code"`
}
type ProgressOmittedPayload struct {
	RetainedEvents uint64 `json:"retained_events"`
	RetainedBytes  uint64 `json:"retained_bytes"`
	LiveEvents     uint64 `json:"live_events"`
	LiveBytes      uint64 `json:"live_bytes"`
	LiveStopped    bool   `json:"live_stopped"`
}

func (TextDeltaPayload) eventPayload()       {}
func (ToolCallPayload) eventPayload()        {}
func (CommandPayload) eventPayload()         {}
func (FileChangedPayload) eventPayload()     {}
func (StatusPayload) eventPayload()          {}
func (ProgressOmittedPayload) eventPayload() {}

func IsNormalized(t Type) bool {
	switch t {
	case TextDelta, ToolCallStarted, ToolCallFinished, CommandStarted, CommandFinished, FileChanged, Status:
		return true
	}
	return false
}
func IsProgress(t Type) bool { return t == LogChunk || IsNormalized(t) }

// validateProgress limits content before encoding. Enums keep public statuses
// finite and prevent accidental raw provider-error or command-output forwarding.
func validateProgress(kind Type, p Payload) error {
	var size int
	valid := true
	validUTF8 := true
	switch v := p.(type) {
	case TextDeltaPayload:
		size = len(v.SourceID) + len(v.Text)
		validUTF8 = utf8.ValidString(v.SourceID) && utf8.ValidString(v.Text)
	case ToolCallPayload:
		size = len(v.CallID) + len(v.Name) + len(v.Status)
		validUTF8 = utf8.ValidString(v.CallID) && utf8.ValidString(v.Name) && utf8.ValidString(v.Status)
		valid = v.CallID != "" && v.Name != "" && validWorkStatus(v.Status)
	case CommandPayload:
		size = len(v.CommandID) + len(v.Status)
		validUTF8 = utf8.ValidString(v.CommandID) && utf8.ValidString(v.Status)
		valid = v.CommandID != "" && validWorkStatus(v.Status)
	case FileChangedPayload:
		size = len(v.Path) + len(v.Action) + len(v.Provenance)
		validUTF8 = utf8.ValidString(v.Path) && utf8.ValidString(v.Action) && utf8.ValidString(v.Provenance)
		valid = v.Path != "" && v.Provenance == "reported" && (v.Action == "add" || v.Action == "update" || v.Action == "delete")
	case StatusPayload:
		size = len(v.Code)
		validUTF8 = utf8.ValidString(v.Code)
		switch v.Code {
		case "session_started", "turn_started", "turn_completed", "working", "progress_limited":
		default:
			valid = false
		}
	}
	if !validUTF8 {
		return fmt.Errorf("normalized %s requires valid UTF-8 strings", kind)
	}
	if size > MaxProgressContent {
		return fmt.Errorf("progress content exceeds %d bytes", MaxProgressContent)
	}
	if !valid {
		return fmt.Errorf("invalid normalized %s status/action", kind)
	}
	return nil
}
func validWorkStatus(s string) bool {
	return s == "in_progress" || s == "completed" || s == "failed" || s == "cancelled"
}
