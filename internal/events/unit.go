package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const ReturnEnvelope Type = "return_envelope"
const DeclaredScopeConflicts Type = "declared_scope_conflicts"
const MaxReturnEnvelope = 65536

// ReturnEnvelopePayload is derived from a retained sanitized CheckResult. Its
// intended scope is authored intent, never a claim about observed file changes.
type ReturnEnvelopePayload struct {
	Version            int            `json:"return_envelope_version"`
	UnitID             string         `json:"unit_id"`
	Verdict            CheckVerdict   `json:"verdict"`
	Reason             string         `json:"reason"`
	Evidence           []GateEvidence `json:"evidence"`
	IntendedScope      []string       `json:"intended_scope"`
	ScopeRedacted      bool           `json:"scope_redacted"`
	AttemptID          AttemptID      `json:"attempt_id"`
	CheckID            string         `json:"check_id"`
	SourceRunID        RunID          `json:"source_run_id"`
	SourceNodeID       string         `json:"source_node_id"`
	SourceNodeRunID    NodeRunID      `json:"source_node_run_id"`
	SourceGraphVersion string         `json:"source_graph_version"`
}

func (ReturnEnvelopePayload) eventPayload() {}

// NormalizeUnitScope preserves complete intent, including directory prefixes.
// ./ denotes the entire workspace. Bounds apply to the represented JSON too.
func NormalizeUnitScope(scope []string) ([]string, error) {
	out := make([]string, 0, len(scope))
	for _, s := range scope {
		if s == "" || !utf8.ValidString(s) || strings.ContainsAny(s, "\\\x00") || strings.HasPrefix(s, "/") || (len(s) > 1 && s[1] == ':') {
			return nil, errors.New("unit scope requires portable relative paths")
		}
		for _, segment := range strings.Split(s, "/") {
			if segment == ".." {
				return nil, errors.New("unit scope forbids parent traversal")
			}
		}
		directory := strings.HasSuffix(s, "/")
		s = path.Clean(s)
		if s == "." {
			s = "./"
		} else if directory {
			s += "/"
		}
		out = append(out, s)
	}
	if err := ValidateUnitScope(out, false); err != nil {
		return nil, err
	}
	return out, nil
}
func ValidateUnitScope(scope []string, redacted bool) error {
	if scope == nil || len(scope) > 32 {
		return errors.New("unit scope must contain at most 32 entries")
	}
	for _, s := range scope {
		if !validText(s, 512) {
			return errors.New("unit scope entry must be nonempty UTF-8 and at most 512 bytes")
		}
		if !redacted {
			if strings.ContainsAny(s, "\\\x00") || strings.HasPrefix(s, "/") || (len(s) > 1 && s[1] == ':') {
				return errors.New("invalid unit scope path")
			}
			for _, segment := range strings.Split(s, "/") {
				if segment == ".." {
					return errors.New("invalid unit scope traversal")
				}
			}
			canonical := path.Clean(s)
			if canonical == "." {
				canonical = "./"
			} else if strings.HasSuffix(s, "/") {
				canonical += "/"
			}
			if s != canonical {
				return errors.New("unit scope path must be normalized")
			}
		}
	}
	b, err := json.Marshal(scope)
	if err != nil || len(b) > 8192 {
		return errors.New("complete unit scope exceeds 8192 encoded bytes")
	}
	return nil
}
func (p ReturnEnvelopePayload) Validate() error {
	if p.Version != 1 || !validText(p.UnitID, 256) || p.UnitID != p.SourceNodeID || !validText(string(p.AttemptID), 256) || !validText(string(p.SourceRunID), 256) || !validText(string(p.SourceNodeRunID), 256) || !validText(p.SourceGraphVersion, 256) || !validText(p.CheckID, 256) || !validText(p.Reason, 512) {
		return errors.New("invalid return envelope version or identity/reason")
	}
	if p.Verdict != CheckGreen && p.Verdict != CheckRed {
		return errors.New("invalid return envelope verdict")
	}
	if err := ValidateUnitScope(p.IntendedScope, p.ScopeRedacted); err != nil {
		return err
	}
	// Reuse the evidence union validation without inventing a second result kind.
	if p.Evidence == nil || len(p.Evidence) > 32 || (p.Verdict == CheckRed && len(p.Evidence) == 0) {
		return errors.New("invalid return envelope evidence")
	}
	if len(p.Evidence) > 0 {
		kind := CheckKind(p.Evidence[0].Kind)
		check := GateCheckPayload{Version: 1, Scope: CheckScope{CheckedInputs: []string{"envelope"}}, CheckID: p.CheckID, Kind: kind, Verdict: p.Verdict, Reason: p.Reason, Evidence: p.Evidence}
		if err := check.Validate(); err != nil {
			return err
		}
	}
	b, err := json.Marshal(p)
	if err != nil || len(b) > MaxReturnEnvelope {
		return errors.New("return envelope exceeds 65536 encoded bytes")
	}
	return nil
}
func (p *ReturnEnvelopePayload) UnmarshalJSON(data []byte) error {
	type wire ReturnEnvelopePayload
	keys := []string{"return_envelope_version", "unit_id", "verdict", "reason", "evidence", "intended_scope", "scope_redacted", "attempt_id", "check_id", "source_run_id", "source_node_id", "source_node_run_id", "source_graph_version"}
	if !utf8.Valid(data) || len(data) > MaxReturnEnvelope {
		return errors.New("invalid return envelope encoding")
	}
	// Duplicate spellings are ambiguous even when their final values agree.
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("return envelope must be an object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return errors.New("duplicate return envelope field")
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	fields, err := checkObjectFields(data, keys...)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if len(fields[k]) == 0 || bytes.Equal(bytes.TrimSpace(fields[k]), []byte("null")) {
			return fmt.Errorf("return envelope requires %s", k)
		}
	}
	var v wire
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	c := ReturnEnvelopePayload(v)
	if err := c.Validate(); err != nil {
		return err
	}
	*p = c
	return nil
}
func CloneEnvelope(p ReturnEnvelopePayload) ReturnEnvelopePayload {
	p.IntendedScope = append([]string{}, p.IntendedScope...)
	p.Evidence = CloneGate(GateCheckPayload{Evidence: p.Evidence}).Evidence
	return p
}

type DeclaredScopeConflict struct {
	LeftNodeID  string `json:"left_node_id"`
	RightNodeID string `json:"right_node_id"`
	Reason      string `json:"reason"`
}

// Details report potential conflicts only, not observed changes or isolation.
type DeclaredScopeConflictsPayload struct {
	Details []DeclaredScopeConflict `json:"details"`
	Omitted uint64                  `json:"omitted"`
}

func (DeclaredScopeConflictsPayload) eventPayload() {}
func (p DeclaredScopeConflictsPayload) Validate() error {
	if p.Details == nil || len(p.Details) > 64 {
		return errors.New("invalid declared conflict detail count")
	}
	for _, d := range p.Details {
		if !utf8.ValidString(d.LeftNodeID) || d.LeftNodeID == "" || !utf8.ValidString(d.RightNodeID) || d.RightNodeID == "" || d.LeftNodeID >= d.RightNodeID || (d.Reason != "declared_scope_overlap" && d.Reason != "unknown_scope" && d.Reason != "unknown_workspace_identity") {
			return errors.New("invalid declared scope conflict")
		}
	}
	return nil
}
