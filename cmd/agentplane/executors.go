package main

import (
	"reflect"

	"agentplane/internal/agent"
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
func selectExecutor(def *graph.Definition, remedy func(agent.AdapterID) string) runtime.Executor {
	if needsNodeExecutor(def) {
		return nodeExecutor{agents: agent.Executor{Remedy: remedy}}
	}
	return shell.Executor{}
}

// remedyFrom turns a map of adapter id -> message into the Executor.Remedy
// callback; an adapter without a message gets no remedy.
func remedyFrom(messages map[agent.AdapterID]string) func(agent.AdapterID) string {
	return func(id agent.AdapterID) string { return messages[id] }
}

// cliAgentRemedy is the remedy `run` gives for an unavailable adapter: the
// flags that register it. Only adapters with such flags have a message.
func cliAgentRemedy(id agent.AdapterID) string {
	return remedyFrom(map[agent.AdapterID]string{
		"codex":  "run with --adapter codex --codex-bin ABSOLUTE_PATH of the installed Codex CLI",
		"claude": "run with --adapter claude --claude-bin ABSOLUTE_PATH of the installed Claude Code CLI",
	})(id)
}

// registeredAgentRemedy is the remedy `run` gives once --adapter has
// registered the adapter: repeating the flags the user already passed would
// not help, so it points at the binary they named instead.
func registeredAgentRemedy(id agent.AdapterID) string {
	return remedyFrom(map[agent.AdapterID]string{
		"codex":  "check that the Codex CLI named by --codex-bin is installed and runs",
		"claude": "check that the Claude Code CLI named by --claude-bin is installed and runs",
	})(id)
}

// serveAgentRemedy is the remedy `serve` (the desktop engine) gives: it runs no
// Agent adapter, so an Agent node runs from the CLI.
func serveAgentRemedy(id agent.AdapterID) string {
	const prefix = "the desktop engine runs no Agent adapter"
	messages := map[agent.AdapterID]string{
		"codex":  prefix + "; run this graph with `agentplane run --adapter codex --codex-bin ABSOLUTE_PATH`",
		"claude": prefix + "; run this graph with `agentplane run --adapter claude --claude-bin ABSOLUTE_PATH`",
	}
	if message, ok := messages[id]; ok {
		return message
	}
	return prefix
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
