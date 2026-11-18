package events

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// AttemptDiff records what one Agent correction attempt changed in the
// workspace (F2.8b, ADR-0026). It exists from event schema 12 and only when
// diff recording is enabled. One event per file plus one summary event per
// attempt; the summary has an empty path. Paths and patch text come from
// repositories and are untrusted, so every field is bounded here.
const AttemptDiff Type = "attempt_diff"

const (
	// AttemptDiffSchema is the first event schema that carries attempt_diff.
	AttemptDiffSchema = 12

	// MaxAttemptDiffPatchBytes bounds the patch of one file.
	MaxAttemptDiffPatchBytes = 16 << 10
	// MaxAttemptDiffAttemptBytes bounds the patch bytes of one attempt.
	MaxAttemptDiffAttemptBytes = 256 << 10
	// MaxRunDiffBytes bounds the patch bytes of one run.
	MaxRunDiffBytes = 4 << 20
	// MaxAttemptDiffFile bounds the content read from one side of a file.
	MaxAttemptDiffFile = 1 << 20
	// MaxAttemptDiffFiles bounds the files listed per attempt; the rest are counted in omitted_files.
	MaxAttemptDiffFiles = 256
	// MaxAttemptDiffPathBytes bounds one repository path.
	MaxAttemptDiffPathBytes = 1024
)

// Change kinds of a file event.
const (
	DiffAdded    = "added"
	DiffModified = "modified"
	DiffDeleted  = "deleted"
	DiffMode     = "mode"
)

// Reasons a file has no patch; a closed vocabulary so a UI can localize it.
const (
	DiffOmitBinary     = "binary"
	DiffOmitTooLarge   = "too_large"
	DiffOmitIgnored    = "ignored"
	DiffOmitBudget     = "budget"
	DiffOmitUnreadable = "unreadable"
)

// Diagnostic codes of a summary whose capture was incomplete.
const (
	DiffGitUnavailable   = "E_DIFF_GIT_UNAVAILABLE"
	DiffGitFailed        = "E_DIFF_GIT_FAILED"
	DiffTimeout          = "E_DIFF_TIMEOUT"
	DiffScratch          = "E_DIFF_SCRATCH"
	DiffIgnoredLimit     = "E_DIFF_IGNORED_LIMIT"
	DiffUnsupportedEntry = "E_DIFF_UNSUPPORTED_ENTRY"
)

var (
	diffSHA256      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	diffOmitReasons = map[string]bool{DiffOmitBinary: true, DiffOmitTooLarge: true, DiffOmitIgnored: true, DiffOmitBudget: true, DiffOmitUnreadable: true}
	diffDiagnostics = map[string]bool{DiffGitUnavailable: true, DiffGitFailed: true, DiffTimeout: true, DiffScratch: true, DiffIgnoredLimit: true, DiffUnsupportedEntry: true}
)

// AttemptDiffPayload is either a file fact (Path set) or the attempt summary
// (Path empty). Redacted means configured secrets were masked before diffing,
// so a secret-to-secret edit can show no visible change.
type AttemptDiffPayload struct {
	Path          string `json:"path,omitempty"`
	Change        string `json:"change,omitempty"`
	Binary        bool   `json:"binary"`
	Redacted      bool   `json:"redacted"`
	Truncated     bool   `json:"truncated"`
	BeforeSHA256  string `json:"before_sha256,omitempty"`
	AfterSHA256   string `json:"after_sha256,omitempty"`
	BeforeSize    int    `json:"before_size"`
	AfterSize     int    `json:"after_size"`
	AddedLines    int    `json:"added_lines"`
	DeletedLines  int    `json:"deleted_lines"`
	Patch         string `json:"patch,omitempty"`
	OmittedReason string `json:"omitted_reason,omitempty"`

	// Summary only.
	Files          int    `json:"files,omitempty"`
	OmittedFiles   int    `json:"omitted_files,omitempty"`
	PatchBytes     int    `json:"patch_bytes,omitempty"`
	DiagnosticCode string `json:"diagnostic_code,omitempty"`
}

func (AttemptDiffPayload) eventPayload() {}

func validateAttemptDiffEnvelope(e Event) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < AttemptDiffSchema {
		return fmt.Errorf("%s requires schema %d", e.Type, AttemptDiffSchema)
	}
	return validateAttemptScoped(e)
}

// Validate checks bounds and the rules that tie the fields together.
func (p AttemptDiffPayload) Validate() error {
	if p.BeforeSize < 0 || p.AfterSize < 0 || p.AddedLines < 0 || p.DeletedLines < 0 ||
		p.Files < 0 || p.OmittedFiles < 0 || p.PatchBytes < 0 {
		return errors.New("attempt diff counts and sizes must not be negative")
	}
	if p.Path == "" {
		return p.validateSummary()
	}
	return p.validateFile()
}

func (p AttemptDiffPayload) validateFile() error {
	if !validDiffPath(p.Path) {
		return errors.New("attempt diff path must be a bounded relative repository path")
	}
	if err := p.validateSides(); err != nil {
		return err
	}
	if p.Files != 0 || p.OmittedFiles != 0 || p.PatchBytes != 0 || p.DiagnosticCode != "" {
		return errors.New("summary fields are only valid on the attempt summary")
	}
	return p.validatePatch()
}

func (p AttemptDiffPayload) validatePatch() error {
	if len(p.Patch) > MaxAttemptDiffPatchBytes || !utf8.ValidString(p.Patch) {
		return errors.New("attempt diff patch must be bounded UTF-8")
	}
	if p.OmittedReason == "" {
		return nil
	}
	if !diffOmitReasons[p.OmittedReason] {
		return fmt.Errorf("unknown attempt diff omitted reason %q", p.OmittedReason)
	}
	if p.Patch != "" {
		return errors.New("a file with an omitted reason carries no patch")
	}
	return nil
}

func (p AttemptDiffPayload) validateSides() error {
	switch p.Change {
	case DiffAdded:
		if p.BeforeSHA256 != "" || p.BeforeSize != 0 {
			return errors.New("an added file has no before side")
		}
	case DiffDeleted:
		if p.AfterSHA256 != "" || p.AfterSize != 0 {
			return errors.New("a deleted file has no after side")
		}
	case DiffModified, DiffMode:
	default:
		return fmt.Errorf("unknown attempt diff change %q", p.Change)
	}
	return validDiffHashes(p.BeforeSHA256, p.AfterSHA256)
}

func validDiffHashes(hashes ...string) error {
	for _, h := range hashes {
		if h != "" && !diffSHA256.MatchString(h) {
			return errors.New("attempt diff hashes must be lowercase SHA-256 hex")
		}
	}
	return nil
}

func (p AttemptDiffPayload) hasFileFields() bool {
	return p.Change != "" || p.BeforeSHA256 != "" || p.AfterSHA256 != "" || p.BeforeSize != 0 ||
		p.AfterSize != 0 || p.Patch != "" || p.Binary
}

func (p AttemptDiffPayload) validateSummary() error {
	if p.hasFileFields() {
		return errors.New("the attempt summary carries no file fields")
	}
	if p.Files > MaxAttemptDiffFiles || p.PatchBytes > MaxAttemptDiffAttemptBytes {
		return errors.New("attempt diff summary exceeds the attempt bounds")
	}
	if p.DiagnosticCode != "" && !diffDiagnostics[p.DiagnosticCode] {
		return fmt.Errorf("unknown attempt diff diagnostic code %q", p.DiagnosticCode)
	}
	unreadable := p.OmittedReason == DiffOmitUnreadable
	if unreadable != (p.DiagnosticCode != "") || (p.OmittedReason != "" && !unreadable) {
		return errors.New("a summary is unreadable if and only if it has a diagnostic code")
	}
	return nil
}

func validDiffPath(p string) bool {
	if p == "" || len(p) > MaxAttemptDiffPathBytes || !utf8.ValidString(p) || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if !validDiffSegment(segment) {
			return false
		}
	}
	return true
}

func validDiffSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.EqualFold(s, ".git")
}
