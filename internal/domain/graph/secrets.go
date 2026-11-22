package graph

import (
	"fmt"
	"sort"
	"strings"

	"agentplane/internal/domain/secrets"
)

// SecretDefinition declares one secret by reference only (F3.8, ADR-0029). The
// graph never holds a value. Unknown and NotMapping are filled by the parser
// with key names and a shape flag, never content, so validation can name what
// was wrong without echoing a literal secret someone pasted by mistake.
type SecretDefinition struct {
	SecretRef  string
	Unknown    []string
	NotMapping bool
}

// secretEnvKey is the node config key that assigns secrets to child variables.
const secretEnvKey = "secret_env"

func (v *definitionValidator) validateSecrets() {
	refs := v.validateSecretDeclarations()
	var channels []secrets.NodeChannels
	for _, nodeID := range sortedNodeIDs(v.definition.Nodes) {
		node := v.definition.Nodes[nodeID]
		v.validateEmbeddedGateSecretEnv(nodeID, node)
		assigned, ok := v.validateNodeSecretEnv(nodeID, node)
		if !ok {
			assigned = nil
		}
		channels = append(channels, secrets.NodeChannels{NodeID: nodeID, Assigned: assigned, Names: nodeEnvNames(node)})
	}
	for _, b := range secrets.AmbientBypasses(refs, channels) {
		v.error("E_SECRET_AMBIENT_BYPASS",
			fmt.Sprintf("node %q names %s, the variable behind secret %q, but is not assigned that secret", b.NodeID, b.Variable, b.Secret),
			DiagnosticLocation{NodeID: b.NodeID, Path: mapPath("nodes", b.NodeID) + ".config"},
			fmt.Sprintf("assign the secret with secret_env or stop naming %s in this node; this check applies in every mode", b.Variable))
	}
}

// validateSecretDeclarations returns the references of the valid declarations.
func (v *definitionValidator) validateSecretDeclarations() map[string]secrets.Ref {
	refs := map[string]secrets.Ref{}
	for i, name := range sortedKeys(v.definition.Secrets) {
		if ref, ok := v.validateSecretEntry(i, name); ok {
			refs[name] = ref
		}
	}
	return refs
}

// secretLabel names a secret in a message. Names are map keys the author typed,
// so text that does not match the name grammar might be a pasted secret and is
// replaced by its position; the same rule applies to the path.
func secretLabel(i int, name string) (label, path string) {
	if secrets.ValidName(name) {
		return fmt.Sprintf("secret %q", name), mapPath("secrets", name)
	}
	return fmt.Sprintf("the secret at secrets[%d]", i), fmt.Sprintf("secrets[%d]", i)
}

func (v *definitionValidator) validateSecretEntry(i int, name string) (secrets.Ref, bool) {
	entry := v.definition.Secrets[name]
	label, path := secretLabel(i, name)
	loc := DiagnosticLocation{Path: path}
	nameValid := secrets.ValidName(name)
	if !nameValid {
		v.error("E_SECRET_NAME_INVALID", fmt.Sprintf("the name of %s is invalid", label), loc, "use lowercase letters, digits and underscores, starting with a letter, at most 64 characters")
	}
	if entry.NotMapping {
		v.error("E_SECRET_ENTRY_INVALID", fmt.Sprintf("%s must be a mapping with exactly one field, secret_ref", label), loc, "write secret_ref: <provider>/<name>; a graph never holds a secret value")
		return secrets.Ref{}, false
	}
	if len(entry.Unknown) > 0 {
		v.error("E_SECRET_ENTRY_INVALID", fmt.Sprintf("%s has unsupported %s; only secret_ref is allowed", label, describeFields(entry.Unknown)), loc, "remove the extra fields; a graph never holds a secret value")
		return secrets.Ref{}, false
	}
	ref, ok := v.validateSecretRef(label, entry.SecretRef, loc)
	return ref, ok && nameValid
}

func (v *definitionValidator) validateSecretRef(label, text string, loc DiagnosticLocation) (secrets.Ref, bool) {
	loc.Path += ".secret_ref"
	if text == "" {
		v.error("E_SECRET_REF_REQUIRED", fmt.Sprintf("%s has no secret_ref", label), loc, "add secret_ref: env/<VARIABLE_NAME> to name where the runner finds the value")
		return secrets.Ref{}, false
	}
	ref, err := secrets.ParseRef(text)
	if err != nil {
		v.error("E_SECRET_REF_INVALID", fmt.Sprintf("%s has an invalid secret_ref: %v", label, err), loc, "use <provider>/<name>, for example env/GITHUB_TOKEN; never write the secret value here")
		return secrets.Ref{}, false
	}
	switch secrets.StatusOf(ref.Provider) {
	case secrets.ProviderUnknown:
		v.error("E_SECRET_PROVIDER_UNKNOWN", fmt.Sprintf("%s uses an unknown provider (available: %s)", label, secrets.ProviderEnv), loc, "use the env provider: env/<VARIABLE_NAME>")
		return ref, false
	case secrets.ProviderReserved:
		v.error("E_SECRET_PROVIDER_UNAVAILABLE", fmt.Sprintf("%s uses provider %q, which is reserved and not available yet", label, ref.Provider), loc, "use the env provider: env/<VARIABLE_NAME>")
		return ref, false
	}
	return ref, true
}

// secretNameText quotes a secret name only when it matches the name grammar; a
// value pasted in its place is never echoed.
func secretNameText(name string) string {
	if secrets.ValidName(name) {
		return fmt.Sprintf("secret %q", name)
	}
	return "an invalid secret name"
}

// describeFields lists unsupported field names, quoting only those that match
// the name grammar and counting the rest.
func describeFields(items []string) string {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	var quoted []string
	other := 0
	for _, s := range sorted {
		if secrets.ValidName(s) {
			quoted = append(quoted, fmt.Sprintf("%q", s))
		} else {
			other++
		}
	}
	switch {
	case other == 0 && len(quoted) == 1:
		return "field " + quoted[0]
	case other == 0:
		return "fields " + strings.Join(quoted, ", ")
	case len(quoted) == 0:
		return fmt.Sprintf("fields (%d, names not shown)", other)
	}
	return fmt.Sprintf("fields %s and %d more (names not shown)", strings.Join(quoted, ", "), other)
}
