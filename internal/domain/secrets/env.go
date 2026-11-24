package secrets

import (
	"fmt"
	"sort"
	"strings"
)

// Binding names one child environment variable and the secret it carries.
// Secret is the declared secret name; it lets a scan name what it found.
type Binding struct {
	Variable string
	Secret   string
	Value    Value
}

// Env is the secret environment of one node run. The zero value is empty. Its
// bindings are copied and sorted by upper-cased variable name. Like Value it
// prints only a count and refuses serialization; Environ is the one place the
// plaintext is produced and only the process launcher should call it.
type Env struct{ bindings []Binding }

// NewEnv copies bindings into an Env.
func NewEnv(bindings []Binding) Env {
	copied := append([]Binding(nil), bindings...)
	sort.SliceStable(copied, func(i, j int) bool {
		return strings.ToUpper(copied[i].Variable) < strings.ToUpper(copied[j].Variable)
	})
	return Env{bindings: copied}
}

// Names lists the variable names in order. They are safe to put in events.
func (e Env) Names() []string {
	names := make([]string, len(e.bindings))
	for i, b := range e.bindings {
		names[i] = b.Variable
	}
	return names
}

// Environ returns NAME=value entries for a child process environment.
func (e Env) Environ() []string {
	entries := make([]string, len(e.bindings))
	for i, b := range e.bindings {
		entries[i] = b.Variable + "=" + b.Value.Reveal()
	}
	return entries
}

// Targets returns one scan target per distinct granted secret, sorted by name.
// A binding without a valid name or with an empty value is skipped. The targets
// hold the plaintext only inside Value and never print it (F3.8 requirement 13).
func (e Env) Targets() []ScanTarget {
	byName := make(map[string]ScanTarget, len(e.bindings))
	for _, b := range e.bindings {
		if target, err := NewScanTarget(b.Secret, b.Value); err == nil {
			byName[b.Secret] = target
		}
	}
	targets := make([]ScanTarget, 0, len(byName))
	for _, target := range byName {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	return targets
}

// Pick returns NAME=value entries only for the requested variables, each at
// most once and in Env order. Variables that are not bound produce no entry, so
// plaintext of unrequested bindings is never materialized.
func (e Env) Pick(variables map[string]string) []string {
	var entries []string
	seen := make(map[string]bool, len(variables))
	for _, b := range e.bindings {
		if _, wanted := variables[b.Variable]; wanted && !seen[b.Variable] {
			seen[b.Variable] = true
			entries = append(entries, b.Variable+"="+b.Value.Reveal())
		}
	}
	return entries
}

// Format implements fmt.Formatter so that no verb prints a value.
func (e Env) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "[secret env: %d variables]", len(e.bindings))
}

// MarshalJSON always fails: a secret environment must never enter a payload.
func (Env) MarshalJSON() ([]byte, error) { return nil, errValueNotSerializable }

// MarshalText always fails for the same reason as MarshalJSON.
func (Env) MarshalText() ([]byte, error) { return nil, errValueNotSerializable }
