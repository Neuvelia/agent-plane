package events

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Workspace isolation facts (F3.1, ADR-0024). They exist from event schema 11;
// older schemas never carry them and stay readable unchanged. Every payload is
// bounded: paths and refs come from repositories and graphs and are untrusted.
const (
	WorkspacePrepared      Type = "workspace_prepared"
	WorkspaceIntegrated    Type = "workspace_integrated"
	WorkspaceConflict      Type = "workspace_conflict"
	WorkspaceReleased      Type = "workspace_released"
	SourceWorkspaceChanged Type = "source_workspace_changed"
)

const (
	// WorkspaceSchema is the first event schema that carries these facts.
	WorkspaceSchema = 11

	// MaxWorkspacePaths bounds repository paths per payload; the rest is counted in Omitted.
	MaxWorkspacePaths = 64
	// MaxWorkspacePathBytes bounds one repository path.
	MaxWorkspacePathBytes = 1024
	// MaxWorkspaceLocalPath bounds one absolute local directory.
	MaxWorkspaceLocalPath = 4096
	// MaxWorkspaceRefBytes bounds one ref name.
	MaxWorkspaceRefBytes = 512

	// WorkspaceModeWorktree is the only isolation mode: a linked Git worktree.
	WorkspaceModeWorktree = "worktree"

	// WorkspaceRemoved is the release action for a worktree that was deleted.
	WorkspaceRemoved = "removed"
	// WorkspaceRetained is the release action for a worktree that was kept.
	WorkspaceRetained = "retained"
)

// Reasons are a closed vocabulary so a UI can localize them without parsing text.
const (
	ReleaseIntegrated   = "integrated"     // removed: accepted and merged
	ReleasePolicyNever  = "policy_never"   // removed: --retain-worktrees never
	ReleasePolicyAlways = "policy_always"  // retained: --retain-worktrees always
	ReleaseExhausted    = "exhausted"      // retained: attempt budget used up
	ReleaseConflict     = "conflict"       // retained: integration conflict
	ReleaseError        = "error"          // retained: operational failure
	ReleaseCancelled    = "cancelled"      // retained: run or node cancelled
	ReleaseCleanupError = "cleanup_failed" // retained: removal was attempted and failed
)

var (
	workspaceSHA  = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	workspaceRef  = regexp.MustCompile(`^refs/agentplane/[A-Za-z0-9/_-]+$`)
	workspaceErrC = regexp.MustCompile(`^[a-z][a-z_]{0,63}$`)
)

// WorkspacePreparedPayload records that an isolated worktree exists for a node run.
type WorkspacePreparedPayload struct {
	BaseSHA    string `json:"base_sha"`
	Mode       string `json:"mode"`
	Path       string `json:"path"`        // absolute local worktree directory
	SourceRoot string `json:"source_root"` // absolute local source repository
}

// WorkspaceIntegratedPayload records that an accepted unit was merged onto the integration tip.
type WorkspaceIntegratedPayload struct {
	UnitRef          string `json:"unit_ref"`
	UnitCommit       string `json:"unit_commit"`
	IntegratedRef    string `json:"integrated_ref"`
	IntegratedCommit string `json:"integrated_commit"`
	ChangedPaths     uint64 `json:"changed_paths"` // paths the unit changed relative to the previous tip
}

// WorkspaceConflictPayload records that merging an accepted unit conflicted; the tip stays unchanged.
type WorkspaceConflictPayload struct {
	UnitRef    string   `json:"unit_ref"`
	UnitCommit string   `json:"unit_commit"`
	TipCommit  string   `json:"tip_commit"` // integration tip left unchanged
	Paths      []string `json:"paths"`      // sorted, unique, at most MaxWorkspacePaths
	Omitted    uint64   `json:"omitted"`
}

// WorkspaceReleasedPayload records whether a node run's worktree was removed or retained, and why.
type WorkspaceReleasedPayload struct {
	Action       string `json:"action"` // removed | retained
	Reason       string `json:"reason"`
	Path         string `json:"path"`
	CleanupError string `json:"cleanup_error,omitempty"` // error class, only with reason cleanup_failed
}

// SourceWorkspaceChangedPayload is an observation about the source checkout,
// not a claim about who changed it. Indeterminate means inspection failed and
// nothing is known; it never means "unchanged".
type SourceWorkspaceChangedPayload struct {
	Paths         []string `json:"paths"`
	Omitted       uint64   `json:"omitted"`
	Indeterminate bool     `json:"indeterminate"`
}

func (WorkspacePreparedPayload) eventPayload()      {}
func (WorkspaceIntegratedPayload) eventPayload()    {}
func (WorkspaceConflictPayload) eventPayload()      {}
func (WorkspaceReleasedPayload) eventPayload()      {}
func (SourceWorkspaceChangedPayload) eventPayload() {}

func validWorkspaceSHA(s string) bool { return workspaceSHA.MatchString(s) }

func validWorkspaceRef(s string) bool {
	return len(s) <= MaxWorkspaceRefBytes && workspaceRef.MatchString(s) && !strings.Contains(s, "//")
}

func validLocalDirectory(p string) bool {
	return p != "" && len(p) <= MaxWorkspaceLocalPath && utf8.ValidString(p) && strings.HasPrefix(p, "/") &&
		!strings.ContainsAny(p, "\x00\r\n")
}

func validRepositoryPath(p string) bool {
	if p == "" || len(p) > MaxWorkspacePathBytes || !utf8.ValidString(p) || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

// BoundWorkspacePaths prepares repository paths for a payload: it sorts,
// removes duplicates, drops paths that cannot be represented (empty, absolute,
// parent traversal, invalid UTF-8, over MaxWorkspacePathBytes) and keeps at
// most MaxWorkspacePaths. Everything not kept, including every dropped path,
// is counted in omitted, so a long or hostile path is never silently lost.
func BoundWorkspacePaths(paths []string) (kept []string, omitted uint64) {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	kept = []string{}
	var previous string
	for i, p := range sorted {
		if i > 0 && p == previous {
			continue
		}
		previous = p
		if !validRepositoryPath(p) || len(kept) >= MaxWorkspacePaths {
			omitted++
			continue
		}
		kept = append(kept, p)
	}
	return kept, omitted
}

func validateRepositoryPaths(paths []string) error {
	if paths == nil {
		return errors.New("workspace paths must be a list")
	}
	if len(paths) > MaxWorkspacePaths {
		return fmt.Errorf("workspace paths exceed %d entries", MaxWorkspacePaths)
	}
	for i, p := range paths {
		if !validRepositoryPath(p) {
			return errors.New("invalid workspace path")
		}
		if i > 0 && paths[i-1] >= p {
			return errors.New("workspace paths must be sorted and unique")
		}
	}
	return nil
}

// Validate checks that the base commit, mode and directories are well formed.
func (p WorkspacePreparedPayload) Validate() error {
	if !validWorkspaceSHA(p.BaseSHA) {
		return errors.New("workspace base_sha must be a full commit id")
	}
	if p.Mode != WorkspaceModeWorktree {
		return fmt.Errorf("unknown workspace mode %q", p.Mode)
	}
	if !validLocalDirectory(p.Path) || !validLocalDirectory(p.SourceRoot) {
		return errors.New("workspace paths must be bounded absolute directories")
	}
	return nil
}

// Validate checks that the refs are under refs/agentplane and the commits are full ids.
func (p WorkspaceIntegratedPayload) Validate() error {
	if !validWorkspaceRef(p.UnitRef) || !validWorkspaceRef(p.IntegratedRef) {
		return errors.New("workspace refs must be under refs/agentplane")
	}
	if !validWorkspaceSHA(p.UnitCommit) || !validWorkspaceSHA(p.IntegratedCommit) {
		return errors.New("workspace commits must be full commit ids")
	}
	return nil
}

// Validate checks the refs, commits and bounded sorted paths; a conflict names at least one path.
func (p WorkspaceConflictPayload) Validate() error {
	if !validWorkspaceRef(p.UnitRef) {
		return errors.New("workspace unit_ref must be under refs/agentplane")
	}
	if !validWorkspaceSHA(p.UnitCommit) || !validWorkspaceSHA(p.TipCommit) {
		return errors.New("workspace commits must be full commit ids")
	}
	if err := validateRepositoryPaths(p.Paths); err != nil {
		return err
	}
	if uint64(len(p.Paths))+p.Omitted == 0 {
		return errors.New("a workspace conflict names at least one path")
	}
	return nil
}

// Validate checks that the action, reason and cleanup error class agree with each other.
func (p WorkspaceReleasedPayload) Validate() error {
	switch p.Action {
	case WorkspaceRemoved:
		if p.Reason != ReleaseIntegrated && p.Reason != ReleasePolicyNever {
			return fmt.Errorf("reason %q is not valid for a removed workspace", p.Reason)
		}
	case WorkspaceRetained:
		switch p.Reason {
		case ReleasePolicyAlways, ReleaseExhausted, ReleaseConflict, ReleaseError, ReleaseCancelled, ReleaseCleanupError:
		default:
			return fmt.Errorf("reason %q is not valid for a retained workspace", p.Reason)
		}
	default:
		return fmt.Errorf("unknown workspace release action %q", p.Action)
	}
	if !validLocalDirectory(p.Path) {
		return errors.New("workspace path must be a bounded absolute directory")
	}
	if (p.Reason == ReleaseCleanupError) != (p.CleanupError != "") {
		return errors.New("cleanup_error is required with, and only with, reason cleanup_failed")
	}
	if p.CleanupError != "" && !workspaceErrC.MatchString(p.CleanupError) {
		return errors.New("cleanup_error must be an error class, not text")
	}
	return nil
}

// Validate checks the bounded paths and that an indeterminate observation carries none.
func (p SourceWorkspaceChangedPayload) Validate() error {
	if err := validateRepositoryPaths(p.Paths); err != nil {
		return err
	}
	if p.Indeterminate {
		if len(p.Paths) != 0 || p.Omitted != 0 {
			return errors.New("an indeterminate source observation carries no paths")
		}
		return nil
	}
	if uint64(len(p.Paths))+p.Omitted == 0 {
		return errors.New("an unchanged source is not an event")
	}
	return nil
}

// validateWorkspace checks schema gating and correlation for the five facts.
// Prepared/released belong to a node run before or after its attempts;
// integrated/conflict belong to the accepted attempt; the source observation
// belongs to the run.
func validateWorkspace(e Event) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < WorkspaceSchema {
		return fmt.Errorf("%s requires schema %d", e.Type, WorkspaceSchema)
	}
	switch e.Type {
	case SourceWorkspaceChanged:
		return validateRunScoped(e)
	case WorkspacePrepared, WorkspaceReleased:
		return validateNodeRunScoped(e)
	case WorkspaceIntegrated, WorkspaceConflict:
		return validateAttemptScoped(e)
	}
	return nil
}

func validateRunScoped(e Event) error {
	if e.NodeID != "" || e.NodeRunID != "" || e.AttemptID != "" {
		return errors.New("source_workspace_changed is a run-level fact")
	}
	return nil
}

func validateNodeRunScoped(e Event) error {
	if e.NodeID == "" || e.NodeRunID == "" {
		return fmt.Errorf("%s requires node and node run IDs", e.Type)
	}
	if e.AttemptID != "" {
		return fmt.Errorf("%s is not attempt-scoped", e.Type)
	}
	return nil
}

func validateAttemptScoped(e Event) error {
	if e.NodeID == "" || e.NodeRunID == "" || e.AttemptID == "" {
		return fmt.Errorf("%s requires node, node run and attempt IDs", e.Type)
	}
	return nil
}
