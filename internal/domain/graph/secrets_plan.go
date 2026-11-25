package graph

import (
	"sort"

	"agentplane/internal/domain/secrets"
)

// SecretGrant is one assignment: a child environment variable that receives a
// declared secret.
type SecretGrant struct {
	Variable string
	Secret   string
}

// SecretPlan is what the runner resolves and delivers (F3.8, ADR-0029). Build
// it with PlanSecrets after Validate (or CompileBindings, which runs it)
// reports no errors; it holds references and names only, never a value.
type SecretPlan struct {
	// Refs maps a declared secret name to its parsed reference.
	Refs map[string]secrets.Ref
	// Grants maps a node ID to its assignments, sorted by variable name. Only
	// Shell nodes appear: other node types never receive secrets.
	Grants map[string][]SecretGrant
}

// PlanSecrets lists the references and the Shell assignments of a definition.
// It reads secret_env through the same helpers as the validator, so there is
// one description of its shape; entries the validator would reject are skipped.
func PlanSecrets(definition *Definition) SecretPlan {
	plan := SecretPlan{Refs: map[string]secrets.Ref{}, Grants: map[string][]SecretGrant{}}
	if definition == nil {
		return plan
	}
	for name, entry := range definition.Secrets {
		if ref, err := secrets.ParseRef(entry.SecretRef); err == nil && secrets.ValidName(name) {
			plan.Refs[name] = ref
		}
	}
	for _, nodeID := range sortedNodeIDs(definition.Nodes) {
		if grants := nodeGrants(definition.Nodes[nodeID], plan.Refs); len(grants) > 0 {
			plan.Grants[nodeID] = grants
		}
	}
	return plan
}

func nodeGrants(node NodeDefinition, refs map[string]secrets.Ref) []SecretGrant {
	if node.Type != "shell" {
		return nil
	}
	entries, ok := configStringMap(node.Config[secretEnvKey])
	if !ok {
		return nil
	}
	var grants []SecretGrant
	for _, variable := range sortedKeys(entries) {
		if _, declared := refs[entries[variable]]; declared && secrets.ValidEnvName(variable) {
			grants = append(grants, SecretGrant{Variable: variable, Secret: entries[variable]})
		}
	}
	return grants
}

// Empty reports that no node is assigned a secret.
func (p SecretPlan) Empty() bool { return len(p.Grants) == 0 }

// Assigned lists, sorted, the secrets assigned to at least one node.
func (p SecretPlan) Assigned() []string {
	seen := map[string]bool{}
	for _, grants := range p.Grants {
		for _, g := range grants {
			seen[g.Secret] = true
		}
	}
	return sortedKeys(seen)
}

// NodeIDs lists, sorted, the nodes that are assigned a secret.
func (p SecretPlan) NodeIDs() []string {
	ids := make([]string, 0, len(p.Grants))
	for id := range p.Grants {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
