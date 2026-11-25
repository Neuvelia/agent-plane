package edit

import (
	"fmt"
	"strings"
)

// secretEnvKey is the config key that binds environment variables to declared
// secrets (F3.8, ADR-0029).
const secretEnvKey = "secret_env"

// guardSecrets refuses every operation that would write, drop or replace a
// secret binding. The editor never changes secret declarations or bindings
// (ADR-0029, ADR-0021 annotation); they are edited as YAML text. No operation
// reaches the top-level secrets block, so the guard covers the ways a binding
// can change: a config path under secret_env, a loop Gate config that carries
// secret_env, and removing a node that holds bindings.
func (s *session) guardSecrets(op Op) *Error {
	switch op.Op {
	case OpSetConfig, OpUnsetConfig:
		return refuseSecretPath(op, false)
	case OpSetLoopConfig, OpUnsetLoopConfig:
		return refuseSecretPath(op, true)
	case OpSetLoop:
		if op.Gate != nil && gateConfigNamesSecretEnv(op.Gate) {
			return secretsReadOnly(op.Node, "nodes."+op.Node+".loop.gate.config."+secretEnvKey)
		}
	case OpRemoveNode:
		if n, ok := s.want.Nodes[op.ID]; ok {
			if _, bound := n.Config[secretEnvKey]; bound {
				return secretsReadOnly(op.ID, "nodes."+op.ID+".config."+secretEnvKey)
			}
		}
	}
	return nil
}

func refuseSecretPath(op Op, loop bool) *Error {
	if len(op.Path) == 0 || op.Path[0] != secretEnvKey {
		return nil
	}
	return secretsReadOnly(op.Node, strings.Join(append([]string{"nodes", op.Node, configRoot(loop)}, op.Path...), "."))
}

func gateConfigNamesSecretEnv(gate *LoopGate) bool {
	_, typed, err := decodeValue(gate.Config)
	if err != nil {
		return false // set_loop reports the malformed config itself
	}
	cfg, ok := asMapping(typed)
	if !ok {
		return false
	}
	_, named := cfg[secretEnvKey]
	return named
}

func secretsReadOnly(node, path string) *Error {
	message := fmt.Sprintf("secret declarations and bindings are read-only in the editor; %s cannot be changed here", path)
	e := nodeError(CodeSecretsReadOnly, node, message, "edit secrets and secret_env in the YAML text")
	e.Path = path
	return e
}
