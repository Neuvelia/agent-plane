package main

import (
	"reflect"

	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
)

// shellNodeType is the node type the composed executor hands secrets to.
const shellNodeType = "shell"

// selectExecutor chooses the executor for a graph without an Agent adapter:
// the bare Shell executor for plain Shell graphs, so their output rules stay
// exactly as they were, and the composed node executor for everything else.
// run and serve share it so both entry points deliver secrets alike.
func selectExecutor(def *graph.Definition) runtime.Executor {
	if needsNodeExecutor(def) {
		return nodeExecutor{}
	}
	return shell.Executor{}
}

// needsNodeExecutor reports whether the graph needs the composed executor: a
// node that is not Shell, a loop, or a node that is assigned secrets. Only the
// composed executor hands runtime-owned values (a granted secret_env) to Shell.
func needsNodeExecutor(def *graph.Definition) bool {
	for _, node := range def.Nodes {
		if node.Type != shellNodeType || node.Loop != nil || assignsSecrets(node) {
			return true
		}
	}
	return false
}

// assignsSecrets is true only for a non-empty secret_env: an empty mapping
// assigns nothing and must not change the executor.
func assignsSecrets(node graph.NodeDefinition) bool {
	value := node.Config["secret_env"]
	if value == nil {
		return false
	}
	if mapping := reflect.ValueOf(value); mapping.Kind() == reflect.Map {
		return mapping.Len() > 0
	}
	return true // malformed: let validation report it, with the composed executor
}
