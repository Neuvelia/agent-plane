package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
)

// Workspace scan vocabulary (F3.8 requirement 13, ADR-0029). The domain owns
// only the typed facts and the pure matching rule; reading files is an adapter
// concern (internal/adapter/secretscan), and deciding what the facts mean for a
// run is the runtime's.

// ErrUnobservable means a scan cannot be performed or has no evidence of which
// files changed (no Git evidence, unsupported platform). It is a fact about the
// observation, not a finding: callers record "unobserved", never "clean".
var ErrUnobservable = errors.New("no workspace change evidence")

// ErrInvalidScanTarget marks a scan target with an illegal name or no value.
// The error never carries the value.
var ErrInvalidScanTarget = errors.New("scan target needs a valid secret name and a non-empty value")

// Default scan bounds (F3.8 requirement 13). Files counts the first paths in
// sorted order, not files successfully opened.
const (
	MaxScanFiles      = 256
	MaxScanFileBytes  = 8 << 20
	MaxScanTotalBytes = 32 << 20
)

// ScanLimits bounds one scan. A path or byte that exceeds a limit is reported
// as a ScanGap; it is never skipped silently.
type ScanLimits struct {
	Files      int
	FileBytes  int64
	TotalBytes int64
}

// DefaultScanLimits returns the limits the specification names.
func DefaultScanLimits() ScanLimits {
	return ScanLimits{Files: MaxScanFiles, FileBytes: MaxScanFileBytes, TotalBytes: MaxScanTotalBytes}
}

// ScanTarget is one granted secret to look for: its declared name and its raw
// value. It formats as ScanTarget(<name>) under every fmt verb and refuses JSON
// and text marshaling, like Value. Only Value().Reveal() reads the plaintext.
type ScanTarget struct {
	name  string
	value Value
}

// NewScanTarget validates name with ValidName and requires a non-empty value.
func NewScanTarget(name string, value Value) (ScanTarget, error) {
	if !ValidName(name) || value.Reveal() == "" {
		return ScanTarget{}, ErrInvalidScanTarget
	}
	return ScanTarget{name: name, value: value}, nil
}

// Name returns the declared secret name; it is safe to record.
func (t ScanTarget) Name() string { return t.name }

// Value returns the opaque value. It exists for scanners: reading its plaintext
// is the one sanctioned use of Reveal outside child-process hand-off.
func (t ScanTarget) Value() Value { return t.value }

// String implements fmt.Stringer and never reveals the value.
func (t ScanTarget) String() string { return fmt.Sprintf("ScanTarget(%s)", t.name) }

// GoString implements fmt.GoStringer and never reveals the value.
func (t ScanTarget) GoString() string { return t.String() }

// Format implements fmt.Formatter so that no verb prints the value.
func (t ScanTarget) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(t.String())) }

// MarshalJSON always fails: a scan target must never enter an event or payload.
func (ScanTarget) MarshalJSON() ([]byte, error) { return nil, errValueNotSerializable }

// MarshalText always fails for the same reason as MarshalJSON.
func (ScanTarget) MarshalText() ([]byte, error) { return nil, errValueNotSerializable }

// Match returns, sorted and without duplicates, the names of the targets whose
// raw value occurs in data. The comparison is byte-exact; there is no length
// threshold and no encoded form (v0 scans raw values only).
func Match(data []byte, targets []ScanTarget) []string {
	var names []string
	for _, target := range targets {
		if bytes.Contains(data, []byte(target.value.Reveal())) {
			names = append(names, target.name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// ScanFinding names a path whose content contains a granted secret and the
// secret's declared name. It carries no value and no matched bytes.
type ScanFinding struct {
	Path   string
	Secret string
}

// ScanGapReason is the closed vocabulary of reasons a path was not fully
// examined.
type ScanGapReason string

// Scan gap reasons.
const (
	GapFileTooLarge       ScanGapReason = "file_too_large"
	GapFileLimitReached   ScanGapReason = "file_limit_reached"
	GapTotalBudgetReached ScanGapReason = "total_budget_exhausted"
	GapNotRegularFile     ScanGapReason = "not_regular_file"
	GapUnreadable         ScanGapReason = "unreadable"
)

// ScanGap counts the paths left unexamined for one reason. A report with gaps is
// incomplete, which is different from clean.
type ScanGap struct {
	Reason ScanGapReason
	Count  uint64
}

// ScanReport is the outcome of one scan: findings sorted by path then secret
// name, gaps sorted by reason, and the number of paths whose content was
// examined. It holds no secret value.
type ScanReport struct {
	Findings []ScanFinding
	Gaps     []ScanGap
	Examined uint64
}
