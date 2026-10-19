package events

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

type LegacyGateEvidence struct {
	Source        string `json:"source"`
	Path          string `json:"path,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	Stdout        []byte `json:"stdout,omitempty"`
	Stderr        []byte `json:"stderr,omitempty"`
	StdoutOmitted uint64 `json:"stdout_omitted,omitempty"`
	StderrOmitted uint64 `json:"stderr_omitted,omitempty"`
	Omitted       uint64 `json:"omitted,omitempty"`
}
type LegacyGateCheckPayload struct {
	CheckID  string               `json:"check_id"`
	Kind     string               `json:"kind"`
	Verdict  string               `json:"verdict"`
	Reason   string               `json:"reason"`
	Evidence []LegacyGateEvidence `json:"evidence"`
}

func (LegacyGateCheckPayload) eventPayload() {}
func (p LegacyGateCheckPayload) Validate() error {
	if strings.TrimSpace(p.CheckID) == "" || len(p.CheckID) > 256 || !utf8.ValidString(p.CheckID) || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 512 || !utf8.ValidString(p.Reason) {
		return errors.New("invalid gate identity/reason")
	}
	if p.Kind != "command" && p.Kind != "json_predicate" && p.Kind != "reported_file_scope" {
		return errors.New("invalid gate kind")
	}
	if p.Verdict != "green" && p.Verdict != "red" {
		return errors.New("invalid gate verdict")
	}
	if len(p.Evidence) == 0 || len(p.Evidence) > 32 {
		return errors.New("invalid gate evidence count")
	}
	if p.Kind == "command" && len(p.Evidence) != 1 {
		return errors.New("command requires exactly one evidence item")
	}
	for _, e := range p.Evidence {
		if !utf8.ValidString(e.Path) || len(e.Stdout) > MaxGateStream || len(e.Stderr) > MaxGateStream {
			return errors.New("invalid gate evidence")
		}
		source := p.Kind
		if p.Kind == "reported_file_scope" {
			source = "reported"
		}
		if e.Source != source {
			return errors.New("invalid gate evidence source")
		}
		if p.Kind == "command" {
			if e.ExitCode == nil || (*e.ExitCode != 0 && *e.ExitCode != 1) || (p.Verdict == "green") != (*e.ExitCode == 0) {
				return errors.New("inconsistent gate command verdict")
			}
		} else if e.ExitCode != nil || len(e.Stdout)+len(e.Stderr) > 0 || e.StdoutOmitted != 0 || e.StderrOmitted != 0 {
			return errors.New("unexpected gate process evidence")
		}
	}
	b, err := json.Marshal(p)
	if err != nil || len(b) > MaxGatePayload {
		return errors.New("gate payload exceeds budget")
	}
	return nil
}
func CloneLegacyGate(p LegacyGateCheckPayload) LegacyGateCheckPayload {
	p.Evidence = append([]LegacyGateEvidence(nil), p.Evidence...)
	for i := range p.Evidence {
		e := &p.Evidence[i]
		e.Stdout = append([]byte(nil), e.Stdout...)
		e.Stderr = append([]byte(nil), e.Stderr...)
		if e.ExitCode != nil {
			v := *e.ExitCode
			e.ExitCode = &v
		}
	}
	return p
}
