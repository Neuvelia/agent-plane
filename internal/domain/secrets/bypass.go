package secrets

import (
	"sort"
	"strings"
)

// NodeChannels is what one node can reach: the secrets assigned to it and every
// environment name it names through env_allowlist, environment_refs, an env key
// or an input_env target. Names keep the node's own spelling.
type NodeChannels struct {
	NodeID   string
	Assigned []string
	Names    []string
}

// Bypass is a node that names the variable behind a secret it was not assigned.
type Bypass struct {
	NodeID   string
	Secret   string
	Variable string
}

// AmbientBypasses finds, for every env-provider reference, the nodes that are
// not assigned the secret yet name its backing variable. The comparison is
// case-insensitive on every OS. Order is by node, then secret, then variable.
func AmbientBypasses(refs map[string]Ref, nodes []NodeChannels) []Bypass {
	var found []Bypass
	for secret, ref := range refs {
		if ref.Provider != ProviderEnv {
			continue
		}
		for _, node := range nodes {
			found = append(found, node.bypasses(secret, ref.Name)...)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		if a.Secret != b.Secret {
			return a.Secret < b.Secret
		}
		return a.Variable < b.Variable
	})
	return found
}

func (n NodeChannels) bypasses(secret, variable string) []Bypass {
	if assigned(n.Assigned, secret) {
		return nil
	}
	var found []Bypass
	for _, name := range n.Names {
		if strings.EqualFold(name, variable) {
			found = append(found, Bypass{NodeID: n.NodeID, Secret: secret, Variable: name})
		}
	}
	return found
}

func assigned(secrets []string, name string) bool {
	for _, s := range secrets {
		if s == name {
			return true
		}
	}
	return false
}
