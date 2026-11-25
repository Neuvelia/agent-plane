package graph

import (
	"fmt"
	"sort"
	"strings"

	"agentplane/internal/domain/secrets"
)

// secretEnvScan carries one node's secret_env state across its entries.
type secretEnvScan struct {
	nodeID  string
	node    NodeDefinition
	loc     DiagnosticLocation
	entries map[string]string
	seen    map[string]string
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
	scan := &secretEnvScan{nodeID: nodeID, node: node, loc: loc, entries: entries, seen: map[string]string{}}
	var assigned []string
	for i, variable := range sortedKeys(entries) {
		if secret, ok := v.validateSecretEnvEntry(scan, i, variable); ok {
			assigned = append(assigned, secret)
		}
	}
	sort.Strings(assigned)
	return assigned, true
}

// validateSecretEnvEntry checks one variable in order: name, duplicate,
// declaration, collision. It returns the assigned secret when the entry names a
// declared one; a collision is reported but the assignment still counts.
func (v *definitionValidator) validateSecretEnvEntry(scan *secretEnvScan, index int, variable string) (string, bool) {
	if !secrets.ValidEnvName(variable) {
		v.error("E_SECRET_ENV_NAME_INVALID", fmt.Sprintf("node %q: secret_env entry %d has an invalid variable name", scan.nodeID, index), scan.loc, "use a non-empty name without '=' that is not AGENTPLANE_ATTEMPT")
		return "", false
	}
	if other, dup := scan.seen[strings.ToUpper(variable)]; dup {
		v.error("E_SECRET_ENV_DUPLICATE", fmt.Sprintf("node %q: secret_env names %s and %s, which are the same variable", scan.nodeID, quotedEnv(other, "an earlier entry"), quotedEnv(variable, fmt.Sprintf("the entry at position %d", index))), scan.loc, "keep one entry per variable name; names are compared case-insensitively")
		return "", false
	}
	scan.seen[strings.ToUpper(variable)] = variable
	secret := scan.entries[variable]
	if _, declared := v.definition.Secrets[secret]; !declared {
		v.error("E_SECRET_ENV_UNDECLARED", fmt.Sprintf("node %q: secret_env[%s] names %s, which the graph does not declare", scan.nodeID, envLabel(index, variable), secretNameText(secret)), scan.loc, "declare the secret under top-level secrets with a secret_ref")
		return "", false
	}
	if channel := collidingChannel(scan.node, variable); channel != "" {
		v.error("E_SECRET_ENV_COLLISION", fmt.Sprintf("node %q: secret_env variable %s collides with %s", scan.nodeID, envLabel(index, variable), channel), scan.loc, "give the secret a variable name no other channel of this node uses; names are compared case-insensitively")
	}
	return secret, true
}

// envLabel names a secret_env entry in a message. The key is text the author
// typed, so it is shown only when it matches the identifier grammar; anything
// else might be a pasted secret and is replaced by its position. The position is
// the index among the entry keys sorted by name, not the order in the file.
func envLabel(index int, variable string) string {
	if secrets.ValidEnvIdentifier(variable) {
		return variable
	}
	return fmt.Sprintf("(entry %d)", index)
}

// quotedEnv quotes variable when it matches the identifier grammar and returns
// fallback otherwise. "Position" fallbacks index the keys sorted by name.
func quotedEnv(variable, fallback string) string {
	if secrets.ValidEnvIdentifier(variable) {
		return fmt.Sprintf("%q", variable)
	}
	return fallback
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
