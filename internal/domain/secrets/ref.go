package secrets

import (
	"errors"
	"regexp"
	"strings"
)

// Provider names. env reads the runner's environment; local is reserved for
// the OS keychain (F3.8b) and is reported as not available until it exists.
const (
	ProviderEnv   = "env"
	ProviderLocal = "local"
)

// ReservedEnvName is the child variable the runtime owns for every shell node.
const ReservedEnvName = "AGENTPLANE_ATTEMPT"

// ErrInvalidRef marks a string that is not a <provider>/<name> reference. The
// error never carries the offending text: a mistakenly pasted literal secret
// must not travel into diagnostics.
var ErrInvalidRef = errors.New("secret_ref must be <provider>/<name>: a lowercase provider, then a name of 1-128 printable ASCII characters without '/' or spaces")

var (
	providerPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	namePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	envIdentifier   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// Ref is a parsed secret reference.
type Ref struct {
	Provider string
	Name     string
}

func (r Ref) String() string { return r.Provider + "/" + r.Name }

// ParseRef checks the grammar only; whether the provider exists is StatusOf.
func ParseRef(s string) (Ref, error) {
	provider, name, found := strings.Cut(s, "/")
	if !found || !providerPattern.MatchString(provider) || !validRefName(name) {
		return Ref{}, ErrInvalidRef
	}
	return Ref{Provider: provider, Name: name}, nil
}

func validRefName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c <= 0x20 || c >= 0x7f || c == '/' {
			return false
		}
	}
	return true
}

// ProviderStatus says whether a provider can resolve references today.
type ProviderStatus int

// The statuses StatusOf reports.
const (
	ProviderUnknown ProviderStatus = iota
	ProviderReserved
	ProviderAvailable
)

// StatusOf classifies a provider name.
func StatusOf(provider string) ProviderStatus {
	switch provider {
	case ProviderEnv:
		return ProviderAvailable
	case ProviderLocal:
		return ProviderReserved
	}
	return ProviderUnknown
}

// ValidName reports whether s is a legal declared-secret name.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ValidEnvName reports whether s may name a child environment variable: not
// empty, without "=" or NUL, and not the runtime's reserved variable in any
// letter case.
func ValidEnvName(s string) bool {
	return s != "" && !strings.ContainsAny(s, "=\x00") && !strings.EqualFold(s, ReservedEnvName)
}

// ValidEnvIdentifier reports whether s is a portable environment variable
// identifier (the grammar events.PolicyEvaluated records). Diagnostics quote an
// author-typed variable name only when it matches, because text outside the
// grammar might be a pasted secret.
func ValidEnvIdentifier(s string) bool { return envIdentifier.MatchString(s) }
