package events

import (
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"
)

// Secret and execution-policy facts (F3.8/F3.9, ADR-0029). They exist from
// SchemaPolicy; older schemas never carry them and stay readable unchanged.
//
// Renumber to 14 (after F2.8b=12, F3.4=13); whoever merges later bumps this
// constant and greps for schema literals. Tests derive from the constant.
const (
	PolicyEvaluated Type = "policy_evaluated"
	PolicyViolation Type = "policy_violation"
)

const (
	// SchemaPolicy is the first event schema that carries these facts.
	SchemaPolicy = 12
)

// Bounds per payload; secrets, values and evidence come from graphs and repositories.
const (
	MaxPolicySecrets       = 64
	MaxPolicyValues        = 64
	MaxPolicyValueBytes    = 512
	MaxPolicyEvidence      = 64
	MaxPolicyEvidenceBytes = 512
)

// Closed vocabularies: a UI can localize them without parsing text and a
// secret value cannot ride along in a free-form field.
const (
	PolicyModeAudit   = "audit"
	PolicyModeEnforce = "enforce"

	PolicyRootWorktree  = "worktree"
	PolicyRootWorkspace = "workspace"
	PolicyRootCWD       = "cwd"

	PolicyPermissionPathsWrite = "paths_write"
	PolicyPermissionNetwork    = "network"
	PolicyPermissionShell      = "shell"
	PolicyPermissionCommands   = "commands"

	PolicyStatePrevented     = "prevented"
	PolicyStateContained     = "contained"
	PolicyStateDelegated     = "delegated"
	PolicyStateDetected      = "detected"
	PolicyStateUnobserved    = "unobserved"
	PolicyStateUnenforceable = "unenforceable"

	PolicyReasonLaunchRefused          = "launch_refused"
	PolicyReasonOutOfScopeWrite        = "out_of_scope_write"
	PolicyReasonSecretInWorkspace      = "secret_in_workspace"
	PolicyReasonEnforcementUnavailable = "enforcement_unavailable"

	PolicyOutcomeBlocked  = "blocked"
	PolicyOutcomeRecorded = "recorded"
)

var (
	policySecretName     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	policySecretProvider = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	policySecretEnv      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// PolicyPermission is one permission as requested and as decided for this machine.
// State is what the machine can do; Applied is true only when the run enforces it.
type PolicyPermission struct {
	Name      string   `json:"name"`
	Requested bool     `json:"requested"`
	Value     []string `json:"value"`
	State     string   `json:"state"`
	Applied   bool     `json:"applied"`
}

// PolicySecret names an assigned secret. It never carries a value, length, hash or prefix.
type PolicySecret struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Env      string `json:"env"`
	Resolved bool   `json:"resolved"`
}

// PolicyEvaluatedPayload records the permissions and secret assignment decided for a node run.
type PolicyEvaluatedPayload struct {
	Mode            string             `json:"mode"`
	Applied         bool               `json:"applied"`
	ScopeRootKind   string             `json:"scope_root_kind"`
	ScopeRoot       string             `json:"scope_root"`
	Permissions     []PolicyPermission `json:"permissions"`
	Secrets         []PolicySecret     `json:"secrets"`
	RedactionValues uint64             `json:"redaction_values"` // count only
}

// PolicyViolationPayload records a permission that was, or would have been, violated.
type PolicyViolationPayload struct {
	Permission string   `json:"permission"`
	Reason     string   `json:"reason"`
	Outcome    string   `json:"outcome"`
	Evidence   []string `json:"evidence"` // sorted, unique
	Omitted    uint64   `json:"omitted"`
}

func (PolicyEvaluatedPayload) eventPayload() {}
func (PolicyViolationPayload) eventPayload() {}

func validPolicyPermissionName(s string) bool {
	switch s {
	case PolicyPermissionPathsWrite, PolicyPermissionNetwork, PolicyPermissionShell, PolicyPermissionCommands:
		return true
	}
	return false
}

func validPolicyState(s string) bool {
	switch s {
	case PolicyStatePrevented, PolicyStateContained, PolicyStateDelegated, PolicyStateDetected, PolicyStateUnobserved, PolicyStateUnenforceable:
		return true
	}
	return false
}

func validPolicyReason(s string) bool {
	switch s {
	case PolicyReasonLaunchRefused, PolicyReasonOutOfScopeWrite, PolicyReasonSecretInWorkspace, PolicyReasonEnforcementUnavailable:
		return true
	}
	return false
}

func policyStateApplicable(s string) bool {
	return s == PolicyStatePrevented || s == PolicyStateContained || s == PolicyStateDelegated
}

func validPolicyText(s string, limit int) bool {
	if s == "" || len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return false
		}
	}
	return true
}

// Validate checks vocabularies, bounds, ordering and that Applied never claims
// protection that the mode or the state does not give.
func (p PolicyEvaluatedPayload) Validate() error {
	if p.Mode != PolicyModeAudit && p.Mode != PolicyModeEnforce {
		return fmt.Errorf("unknown policy mode %q", p.Mode)
	}
	if p.Applied && p.Mode != PolicyModeEnforce {
		return errors.New("policy cannot be applied in audit mode")
	}
	switch p.ScopeRootKind {
	case PolicyRootWorktree, PolicyRootWorkspace, PolicyRootCWD:
	default:
		return fmt.Errorf("unknown scope root kind %q", p.ScopeRootKind)
	}
	if p.ScopeRoot != "" && !validLocalDirectory(p.ScopeRoot) {
		return errors.New("scope_root must be empty or a bounded absolute directory")
	}
	if err := p.validatePermissions(); err != nil {
		return err
	}
	return p.validateSecrets()
}

func (p PolicyEvaluatedPayload) validatePermissions() error {
	if p.Permissions == nil || len(p.Permissions) > 4 {
		return errors.New("policy permissions must be a list of at most four")
	}
	for i, perm := range p.Permissions {
		if i > 0 && p.Permissions[i-1].Name >= perm.Name {
			return errors.New("policy permissions must be sorted and unique")
		}
		if err := perm.validate(p.Mode); err != nil {
			return err
		}
	}
	return nil
}

func (perm PolicyPermission) validate(mode string) error {
	if !validPolicyPermissionName(perm.Name) {
		return fmt.Errorf("unknown policy permission %q", perm.Name)
	}
	if !validPolicyState(perm.State) {
		return fmt.Errorf("unknown policy state %q", perm.State)
	}
	if !validPolicyList(perm.Value, MaxPolicyValues, MaxPolicyValueBytes, false) {
		return errors.New("policy permission value must be a bounded list")
	}
	if perm.Applied && (mode != PolicyModeEnforce || !perm.Requested || !policyStateApplicable(perm.State)) {
		return fmt.Errorf("permission %s cannot be applied in this mode or state", perm.Name)
	}
	return nil
}

func (p PolicyEvaluatedPayload) validateSecrets() error {
	if p.Secrets == nil || len(p.Secrets) > MaxPolicySecrets {
		return fmt.Errorf("policy secrets must be a list of at most %d", MaxPolicySecrets)
	}
	for i, s := range p.Secrets {
		if !policySecretName.MatchString(s.Name) || !policySecretProvider.MatchString(s.Provider) || !policySecretEnv.MatchString(s.Env) {
			return errors.New("invalid policy secret name, provider or env")
		}
		if i > 0 && p.Secrets[i-1].Name >= s.Name {
			return errors.New("policy secrets must be sorted and unique")
		}
	}
	return nil
}

// Validate checks vocabularies and that evidence is bounded, sorted and unique.
func (p PolicyViolationPayload) Validate() error {
	if !validPolicyPermissionName(p.Permission) {
		return fmt.Errorf("unknown policy permission %q", p.Permission)
	}
	if !validPolicyReason(p.Reason) {
		return fmt.Errorf("unknown policy violation reason %q", p.Reason)
	}
	if p.Outcome != PolicyOutcomeBlocked && p.Outcome != PolicyOutcomeRecorded {
		return fmt.Errorf("unknown policy violation outcome %q", p.Outcome)
	}
	if !validPolicyList(p.Evidence, MaxPolicyEvidence, MaxPolicyEvidenceBytes, true) {
		return fmt.Errorf("policy evidence must be a sorted, unique list of at most %d bounded entries", MaxPolicyEvidence)
	}
	return nil
}

// validPolicyList requires a non-nil list of bounded text entries; when
// sortedUnique is set the entries must also be strictly ascending.
func validPolicyList(list []string, maxLen, maxBytes int, sortedUnique bool) bool {
	if list == nil || len(list) > maxLen {
		return false
	}
	for i, s := range list {
		if !validPolicyText(s, maxBytes) || sortedUnique && i > 0 && list[i-1] >= s {
			return false
		}
	}
	return true
}

func validatePolicy(e Event) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < SchemaPolicy {
		return fmt.Errorf("%s requires schema %d", e.Type, SchemaPolicy)
	}
	if e.Type == PolicyEvaluated {
		return validateNodeRunScoped(e)
	}
	if e.NodeID == "" || e.NodeRunID == "" {
		return fmt.Errorf("%s requires node and node run IDs", e.Type)
	}
	return nil
}

func clonePolicyPayload(payload Payload) (Payload, bool) {
	switch p := payload.(type) {
	case PolicyEvaluatedPayload:
		perms := make([]PolicyPermission, len(p.Permissions))
		for i, perm := range p.Permissions {
			perm.Value = append([]string{}, perm.Value...)
			perms[i] = perm
		}
		p.Permissions = perms
		p.Secrets = append([]PolicySecret{}, p.Secrets...)
		return p, true
	case PolicyViolationPayload:
		p.Evidence = append([]string{}, p.Evidence...)
		return p, true
	}
	return nil, false
}
