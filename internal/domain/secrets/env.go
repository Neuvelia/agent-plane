package secrets

import (
	"fmt"
	"sort"
	"strings"
)

// Binding names one child environment variable and the secret it carries.
type Binding struct {
	Variable string
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
	noun := "variables"
	if len(e.bindings) == 1 {
		noun = "variable"
	}
	_, _ = fmt.Fprintf(f, "[secret env: %d %s]", len(e.bindings), noun)
}

// MarshalJSON always fails: a secret environment must never enter a payload.
func (Env) MarshalJSON() ([]byte, error) { return nil, errValueNotSerializable }

// MarshalText always fails for the same reason as MarshalJSON.
func (Env) MarshalText() ([]byte, error) { return nil, errValueNotSerializable }
