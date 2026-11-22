package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const GateCheck Type = "gate_check"
const MaxGatePayload = 32768
const MaxGateStream = 8192

type CheckKind string

const (
	CheckCommand           CheckKind = "command"
	CheckJSONPredicate     CheckKind = "json_predicate"
	CheckReportedFileScope CheckKind = "reported_file_scope"
	CheckChangedFilesScope CheckKind = "changed_files_scope"
	CheckExecutionPolicy   CheckKind = "execution_policy"
	CheckSecretInWorkspace CheckKind = "secret_in_workspace"
)

func isPolicyCheckKind(k CheckKind) bool {
	return k == CheckExecutionPolicy || k == CheckSecretInWorkspace
}

type CheckVerdict string

const (
	CheckGreen CheckVerdict = "green"
	CheckRed   CheckVerdict = "red"
)

type EvidenceKind string
type EvidenceSource string

type ReferenceKind string
type ReferenceState string

// EvidenceReference describes supplied metadata, without resolving a resource.
type EvidenceReference struct {
	Kind       ReferenceKind  `json:"kind"`
	State      ReferenceState `json:"state"`
	Identifier string         `json:"identifier,omitempty"`
	Line       *int           `json:"line,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

type CheckScope struct {
	CheckedInputs []string `json:"checked_inputs"`
}

// GateEvidence is one typed fact behind a Check Result. SecretName (only on
// secret_in_workspace evidence) names the granted secret found at Path, never
// its value (F3.8 requirement 13).
type GateEvidence struct {
	Kind          EvidenceKind       `json:"kind"`
	Reference     *EvidenceReference `json:"reference,omitempty"`
	Source        EvidenceSource     `json:"source"`
	Path          string             `json:"path,omitempty"`
	SecretName    string             `json:"secret_name,omitempty"`
	ExitCode      *int               `json:"exit_code,omitempty"`
	Stdout        []byte             `json:"stdout,omitempty"`
	Stderr        []byte             `json:"stderr,omitempty"`
	StdoutOmitted uint64             `json:"stdout_omitted,omitempty"`
	StderrOmitted uint64             `json:"stderr_omitted,omitempty"`
	Omitted       uint64             `json:"omitted,omitempty"`
}
type GateCheckPayload struct {
	Version      int                       `json:"check_result_version"`
	Scope        CheckScope                `json:"scope"`
	DurationNS   int64                     `json:"duration_ns"`
	CheckID      string                    `json:"check_id"`
	Kind         CheckKind                 `json:"kind"`
	Verdict      CheckVerdict              `json:"verdict"`
	Reason       string                    `json:"reason"`
	Evidence     []GateEvidence            `json:"evidence"`
	ChangedFiles *ChangedFilesScopeDetails `json:"changed_files_scope,omitempty"`
}

// ChangedFilesScopeDetails describes the Git observation and authored policy.
type ChangedFilesScopeDetails struct {
	Mode          string   `json:"mode"`
	AllowedPaths  []string `json:"allowed_paths"`
	ScopeRedacted bool     `json:"scope_redacted"`
	BaselineHead  string   `json:"baseline_head"`
	ObservedCount int      `json:"observed_count"`
}

// CheckResult is the full versioned Gate result.
type CheckResult = GateCheckPayload

func (GateCheckPayload) eventPayload() {}

func (p *GateCheckPayload) UnmarshalJSON(data []byte) error {
	type wire GateCheckPayload
	if !utf8.Valid(data) {
		return errors.New("invalid check result UTF8")
	}
	fields, err := checkObjectFields(data, "check_result_version", "check_id", "kind", "verdict", "reason", "evidence", "scope", "duration_ns", "changed_files_scope")
	if err != nil {
		return err
	}
	for _, key := range []string{"check_result_version", "check_id", "kind", "verdict", "reason", "evidence", "scope", "duration_ns"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return fmt.Errorf("check result requires %s", key)
		}
	}
	var v wire
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	candidate := GateCheckPayload(v)
	if err := candidate.Validate(); err != nil {
		return err
	}
	*p = candidate
	return nil
}
func (p GateCheckPayload) Validate() error {
	if p.Version != 1 || p.DurationNS < 0 {
		return errors.New("unsupported check result version or invalid duration")
	}
	if len(p.Scope.CheckedInputs) == 0 || len(p.Scope.CheckedInputs) > 32 || !sort.StringsAreSorted(p.Scope.CheckedInputs) {
		return errors.New("invalid checked input scope")
	}
	for i, input := range p.Scope.CheckedInputs {
		if !validText(input, 256) || i > 0 && input == p.Scope.CheckedInputs[i-1] {
			return errors.New("invalid checked input scope")
		}
	}
	if strings.TrimSpace(p.CheckID) == "" || len(p.CheckID) > 256 || !utf8.ValidString(p.CheckID) || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 512 || !utf8.ValidString(p.Reason) {
		return errors.New("invalid gate identity/reason")
	}
	if p.Kind != CheckCommand && p.Kind != CheckJSONPredicate && p.Kind != CheckReportedFileScope && p.Kind != CheckChangedFilesScope && !isPolicyCheckKind(p.Kind) {
		return errors.New("invalid gate kind")
	}
	if p.Kind == CheckChangedFilesScope {
		if p.ChangedFiles == nil || (p.ChangedFiles.Mode != "warn" && p.ChangedFiles.Mode != "deny") || p.ChangedFiles.AllowedPaths == nil || p.ChangedFiles.ObservedCount < 0 || !validText(p.ChangedFiles.BaselineHead, 128) {
			return errors.New("invalid changed-files scope details")
		}
		if len(p.ChangedFiles.AllowedPaths) > 32 {
			return errors.New("too many allowed paths")
		}
		if !sort.StringsAreSorted(p.ChangedFiles.AllowedPaths) {
			return errors.New("changed-files allowed paths must be sorted")
		}
		for i, path := range p.ChangedFiles.AllowedPaths {
			if !validText(path, 512) {
				return errors.New("invalid allowed path")
			}
			if i > 0 && path == p.ChangedFiles.AllowedPaths[i-1] {
				return errors.New("duplicate allowed path")
			}
		}
		if p.ChangedFiles.Mode == "warn" && p.Verdict != CheckGreen {
			return errors.New("warn scope check cannot block")
		}
		if p.Reason == "changed_files_outside_scope" && p.ChangedFiles.Mode == "deny" && p.Verdict != CheckRed {
			return errors.New("deny violation must be red")
		}
	} else if p.ChangedFiles != nil {
		return errors.New("unexpected changed-files scope details")
	}
	if p.Verdict != "green" && p.Verdict != "red" {
		return errors.New("invalid gate verdict")
	}
	if p.Evidence == nil || p.Verdict == CheckRed && len(p.Evidence) == 0 || len(p.Evidence) > 32 {
		return errors.New("invalid gate evidence count")
	}
	if p.Kind == "command" && len(p.Evidence) != 1 {
		return errors.New("command requires exactly one evidence item")
	}
	for _, e := range p.Evidence {
		if e.Kind != EvidenceKind(p.Kind) {
			return errors.New("invalid evidence kind")
		}
		if e.Reference != nil {
			if err := e.Reference.Validate(); err != nil {
				return err
			}
		}
		if err := validEvidenceSecretName(p.Kind, e.SecretName); err != nil {
			return err
		}
		if !utf8.ValidString(e.Path) || len(e.Stdout) > MaxGateStream || len(e.Stderr) > MaxGateStream {
			return errors.New("invalid gate evidence")
		}
		source := p.Kind
		if p.Kind == "reported_file_scope" {
			source = "reported"
		}
		if p.Kind == CheckChangedFilesScope {
			source = "git"
		}
		if string(e.Source) != string(source) {
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
func CloneGate(p GateCheckPayload) GateCheckPayload {
	if p.ChangedFiles != nil {
		details := *p.ChangedFiles
		details.AllowedPaths = append([]string{}, details.AllowedPaths...)
		p.ChangedFiles = &details
	}
	p.Scope.CheckedInputs = append([]string(nil), p.Scope.CheckedInputs...)
	if p.Evidence != nil {
		p.Evidence = append([]GateEvidence{}, p.Evidence...)
	}
	for i := range p.Evidence {
		e := &p.Evidence[i]
		if e.Reference != nil {
			r := *e.Reference
			e.Reference = &r
			if r.Line != nil {
				v := *r.Line
				e.Reference.Line = &v
			}
		}
		e.Stdout = append([]byte(nil), e.Stdout...)
		e.Stderr = append([]byte(nil), e.Stderr...)
		if e.ExitCode != nil {
			v := *e.ExitCode
			e.ExitCode = &v
		}
	}
	return p
}

// validEvidenceSecretName allows the name only on secret_in_workspace evidence.
// The kind already requires SchemaPolicy, so the field needs no schema check of
// its own and older logs never carry it. The grammar repeats policySecretName
// because this package may not import domain/secrets.
func validEvidenceSecretName(kind CheckKind, name string) error {
	if name == "" {
		return nil
	}
	if kind != CheckSecretInWorkspace {
		return errors.New("secret_name is only valid on secret_in_workspace evidence")
	}
	if !policySecretName.MatchString(name) {
		return errors.New("invalid evidence secret_name")
	}
	return nil
}

func validText(s string, limit int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= limit && utf8.ValidString(s)
}
func (r EvidenceReference) Validate() error {
	if r.Kind != "file" && r.Kind != "test" && r.Kind != "diff" && r.Kind != "artifact" {
		return errors.New("invalid reference kind")
	}
	if r.Line != nil && (r.Kind != "file" || *r.Line <= 0 || r.State != "known") {
		return errors.New("invalid reference line")
	}
	switch r.State {
	case "known":
		if !validText(r.Identifier, 512) || r.Reason != "" {
			return errors.New("invalid known reference")
		}
	case "unavailable", "invalid":
		if r.Identifier != "" || !validText(r.Reason, 512) {
			return errors.New("invalid reference diagnostic")
		}
	default:
		return errors.New("invalid reference state")
	}
	return nil
}

func (e *GateEvidence) UnmarshalJSON(data []byte) error {
	type wire GateEvidence
	fields, err := checkObjectFields(data, "kind", "source", "reference", "exit_code", "path", "secret_name", "stdout", "stderr", "stdout_omitted", "stderr_omitted", "omitted")
	if err != nil {
		return err
	}
	for _, key := range []string{"kind", "source"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return fmt.Errorf("evidence requires %s", key)
		}
	}
	for _, key := range []string{"reference", "exit_code", "path", "secret_name", "stdout", "stderr", "stdout_omitted", "stderr_omitted", "omitted"} {
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return fmt.Errorf("evidence %s may not be null", key)
		}
	}
	var v wire
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	*e = GateEvidence(v)
	return nil
}
func (r *EvidenceReference) UnmarshalJSON(data []byte) error {
	type wire EvidenceReference
	fields, err := checkObjectFields(data, "kind", "state", "identifier", "line", "reason")
	if err != nil {
		return err
	}
	for k, v := range fields {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("reference %s may not be null", k)
		}
	}
	var v wire
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	candidate := EvidenceReference(v)
	// Presence is part of the reference union: forbidden selectors are invalid
	// even when encoding/json would decode and later omit their empty values.
	if candidate.State == "known" {
		if _, present := fields["reason"]; present {
			return errors.New("known reference forbids reason")
		}
	} else if candidate.State == "unavailable" || candidate.State == "invalid" {
		for _, key := range []string{"identifier", "line"} {
			if _, present := fields[key]; present {
				return fmt.Errorf("diagnostic reference forbids %s", key)
			}
		}
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*r = candidate
	return nil
}

// CheckResults selects current full results only. Legacy schema4 facts remain
// available through Event.Payload as LegacyGateCheckPayload, without upgrading.
// Empty identities match nothing; results are detached and sequence ordered.
func CheckResults(trace []Event, run RunID, node NodeRunID) []CheckResult {
	if run == "" || node == "" {
		return nil
	}
	selected := make([]Event, 0)
	for _, e := range trace {
		if e.RunID == run && e.NodeRunID == node && e.Type == GateCheck && e.SchemaVersion >= 5 && e.SchemaVersion <= SchemaVersion {
			if _, ok := e.Payload.(CheckResult); ok {
				selected = append(selected, e)
			}
		}
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Sequence < selected[j].Sequence })
	var results []CheckResult
	for _, e := range selected {
		results = append(results, CloneGate(e.Payload.(CheckResult)))
	}
	return results
}

// checkObjectFields enforces exact JSON spellings before encoding/json's
// case-insensitive struct matching can erase an unknown alias or its null value.
// Legacy schema4 payloads deliberately do not use this stricter v1 decoder.
func checkObjectFields(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("check result object may not be null")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		recognized := false
		for _, name := range allowed {
			if key == name {
				recognized = true
				break
			}
		}
		if !recognized {
			return nil, fmt.Errorf("unknown check result field %q", key)
		}
	}
	return fields, nil
}

func (s *CheckScope) UnmarshalJSON(data []byte) error {
	type wire CheckScope
	fields, err := checkObjectFields(data, "checked_inputs")
	if err != nil {
		return err
	}
	if len(fields["checked_inputs"]) == 0 || bytes.Equal(bytes.TrimSpace(fields["checked_inputs"]), []byte("null")) {
		return errors.New("scope requires checked_inputs")
	}
	var v wire
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = CheckScope(v)
	return nil
}
