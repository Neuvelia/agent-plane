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
	for _, name := range sortedKeys(v.definition.Secrets) {
		entry := v.definition.Secrets[name]
		path := mapPath("secrets", name)
		loc := DiagnosticLocation{Path: path}
		nameValid := secrets.ValidName(name)
		if !nameValid {
			v.error("E_SECRET_NAME_INVALID", fmt.Sprintf("secret name %q is invalid", name), loc, "use lowercase letters, digits and underscores, starting with a letter, at most 64 characters")
		}
		if entry.NotMapping {
			v.error("E_SECRET_ENTRY_INVALID", fmt.Sprintf("secret %q must be a mapping with exactly one field, secret_ref", name), loc, "write secret_ref: <provider>/<name>; a graph never holds a secret value")
			continue
		}
		if len(entry.Unknown) > 0 {
			v.error("E_SECRET_ENTRY_INVALID", fmt.Sprintf("secret %q has unsupported %s; only secret_ref is allowed", name, quotedList(entry.Unknown)), loc, "remove the extra fields; a graph never holds a secret value")
			continue
		}
		ref, ok := v.validateSecretRef(name, entry.SecretRef, loc)
		if ok && nameValid {
			refs[name] = ref
		}
	}
	return refs
}

func (v *definitionValidator) validateSecretRef(name, text string, loc DiagnosticLocation) (secrets.Ref, bool) {
	loc.Path += ".secret_ref"
	if text == "" {
		v.error("E_SECRET_REF_REQUIRED", fmt.Sprintf("secret %q has no secret_ref", name), loc, "add secret_ref: env/<VARIABLE_NAME> to name where the runner finds the value")
		return secrets.Ref{}, false
	}
	ref, err := secrets.ParseRef(text)
	if err != nil {
		v.error("E_SECRET_REF_INVALID", fmt.Sprintf("secret %q has an invalid secret_ref: %v", name, err), loc, "use <provider>/<name>, for example env/GITHUB_TOKEN; never write the secret value here")
		return secrets.Ref{}, false
	}
	switch secrets.StatusOf(ref.Provider) {
	case secrets.ProviderUnknown:
		v.error("E_SECRET_PROVIDER_UNKNOWN", fmt.Sprintf("secret %q uses unknown provider %q", name, ref.Provider), loc, "use the env provider: env/<VARIABLE_NAME>")
		return ref, false
	case secrets.ProviderReserved:
		v.error("E_SECRET_PROVIDER_UNAVAILABLE", fmt.Sprintf("secret %q uses provider %q, which is reserved and not available yet", name, ref.Provider), loc, "use the env provider: env/<VARIABLE_NAME>")
		return ref, false
	}
	return ref, true
}

// validateNodeSecretEnv checks one node's secret_env and returns the declared
// secrets it assigns; ok is false when the block is unusable.
func (v *definitionValidator) validateNodeSecretEnv(nodeID string, node NodeDefinition) ([]string, bool) {
	raw, present := node.Config[secretEnvKey]
	if !present {
		return nil, true
	}
	loc := DiagnosticLocation{NodeID: nodeID, Path: mapPath("nodes", nodeID) + ".config.secret_env"}
	switch node.Type {
	case "shell":
	case "agent":
		v.error("E_SECRET_ENV_AGENT_UNSUPPORTED", fmt.Sprintf("node %q: secret_env is not supported on agent nodes yet", nodeID), loc, "remove secret_env; secrets can be assigned to shell nodes only for now")
		return nil, false
	default:
		v.error("E_SECRET_ENV_NODE_TYPE", fmt.Sprintf("node %q is a %s node and cannot receive secrets", nodeID, node.Type), loc, "only shell nodes may use secret_env")
		return nil, false
	}
	entries, ok := configStringMap(raw)
	if !ok {
		v.error("E_SECRET_ENV_INVALID", fmt.Sprintf("node %q: secret_env must map environment variable names to declared secret names", nodeID), loc, "write secret_env: {GH_TOKEN: gh_token}")
		return nil, false
	}
	var assigned []string
	seen := map[string]string{}
	for _, variable := range sortedKeys(entries) {
		secret := entries[variable]
		if !secrets.ValidEnvName(variable) {
			v.error("E_SECRET_ENV_NAME_INVALID", fmt.Sprintf("node %q: %q is not a valid secret_env variable name", nodeID, variable), loc, "use a non-empty name without '=' that is not AGENTPLANE_ATTEMPT")
			continue
		}
		if other, dup := seen[strings.ToUpper(variable)]; dup {
			v.error("E_SECRET_ENV_DUPLICATE", fmt.Sprintf("node %q: secret_env names %q and %q, which are the same variable", nodeID, other, variable), loc, "keep one entry per variable name; names are compared case-insensitively")
			continue
		}
		seen[strings.ToUpper(variable)] = variable
		if _, declared := v.definition.Secrets[secret]; !declared {
			v.error("E_SECRET_ENV_UNDECLARED", fmt.Sprintf("node %q: secret_env[%s] names secret %q, which the graph does not declare", nodeID, variable, secret), loc, "declare the secret under top-level secrets with a secret_ref")
			continue
		}
		assigned = append(assigned, secret)
		if channel := collidingChannel(node, variable); channel != "" {
			v.error("E_SECRET_ENV_COLLISION", fmt.Sprintf("node %q: secret_env variable %s collides with %s", nodeID, variable, channel), loc, "give the secret a variable name no other channel of this node uses; names are compared case-insensitively")
		}
	}
	sort.Strings(assigned)
	return assigned, true
}

// validateEmbeddedGateSecretEnv rejects secret_env in a loop's embedded Gate,
// which never receives secrets.
func (v *definitionValidator) validateEmbeddedGateSecretEnv(nodeID string, node NodeDefinition) {
	if node.Loop == nil {
		return
	}
	if _, present := node.Loop.Gate.Config[secretEnvKey]; present {
		v.error("E_SECRET_ENV_NODE_TYPE", fmt.Sprintf("node %q: the loop gate is a gate node and cannot receive secrets", nodeID),
			DiagnosticLocation{NodeID: nodeID, Path: mapPath("nodes", nodeID) + ".loop.gate.config.secret_env"}, "remove secret_env from the loop gate")
	}
}

// collidingChannel names the first other channel of the node that uses variable
// (case-insensitive), or "".
func collidingChannel(node NodeDefinition, variable string) string {
	for _, c := range []struct {
		key   string
		names []string
	}{
		{"env", mapKeys(node.Config["env"])},
		{"env_allowlist", envList(node.Config["env_allowlist"])},
		{"input_env", mapValues(node.Config["input_env"])},
		{"environment_refs", envList(node.Config["environment_refs"])},
	} {
		for _, name := range c.names {
			if strings.EqualFold(name, variable) {
				return c.key
			}
		}
	}
	return ""
}

// nodeEnvNames lists every environment name a node names outside secret_env.
func nodeEnvNames(node NodeDefinition) []string {
	var names []string
	names = append(names, mapKeys(node.Config["env"])...)
	names = append(names, envList(node.Config["env_allowlist"])...)
	names = append(names, mapValues(node.Config["input_env"])...)
	names = append(names, envList(node.Config["environment_refs"])...)
	return names
}

func configStringMap(value any) (map[string]string, bool) {
	out := map[string]string{}
	switch m := value.(type) {
	case map[string]string:
		for k, s := range m {
			out[k] = s
		}
	case NodeConfig:
		return anyStringMap(m)
	case map[string]any:
		return anyStringMap(m)
	default:
		return nil, false
	}
	return out, true
}

func anyStringMap(m map[string]any) (map[string]string, bool) {
	out := make(map[string]string, len(m))
	for k, item := range m {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[k] = s
	}
	return out, true
}

func mapKeys(value any) []string {
	switch m := value.(type) {
	case map[string]string:
		return sortedKeys(m)
	case NodeConfig:
		return sortedKeys(m)
	case map[string]any:
		return sortedKeys(m)
	}
	return nil
}

func mapValues(value any) []string {
	m, ok := configStringMap(value)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, m[k])
	}
	return out
}

func envList(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func quotedList(items []string) string {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	quoted := make([]string, len(sorted))
	for i, s := range sorted {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	if len(quoted) == 1 {
		return "field " + quoted[0]
	}
	return "fields " + strings.Join(quoted, ", ")
}
