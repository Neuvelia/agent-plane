package shell

import (
	"fmt"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
)

// Configuration errors reach the terminal, logs and API responses before the
// F3.8 guards run, so they quote author text only when it matches a grammar: a
// literal pasted in place of a name, such as "GH_TOKEN=ghp_xxx", is never
// echoed. This mirrors the graph validator's rule.

// keyText quotes a config key or input id that matches the graph identifier
// grammar.
func keyText(key string) string {
	if graph.ValidIdentifier(key) {
		return fmt.Sprintf("%q", key)
	}
	return "<invalid identifier>"
}

// envText quotes an environment variable name that matches the identifier
// grammar and returns fallback otherwise.
func envText(name, fallback string) string {
	if secrets.ValidEnvIdentifier(name) {
		return fmt.Sprintf("%q", name)
	}
	return fallback
}

// entryText names the entry key of a mapping field: field["KEY"] when the key
// matches the grammar, otherwise its position among the keys sorted by name
// (not the order in the file).
func entryText(field string, index int, key string) string {
	if secrets.ValidEnvIdentifier(key) {
		return fmt.Sprintf("%s[%q]", field, key)
	}
	return fmt.Sprintf("%s entry %d", field, index)
}
