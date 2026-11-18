package graph

import (
	"fmt"

	"agentplane/internal/fields"
)

// HumanPromptLimit mirrors events.MaxApprovalPrompt; the graph package does
// not import events.
const (
	humanPromptLimit  = 1024
	humanContextLimit = 8
)

// validateHumanNodes enforces ADR-0017: a human node publishes exactly one
// string output "decision", takes only prompt/context_inputs configuration,
// and never carries a loop.
func (v *definitionValidator) validateHumanNodes() {
	for _, id := range sortedNodeIDs(v.definition.Nodes) {
		n := v.definition.Nodes[id]
		if n.Type != "human" {
			continue
		}
		loc := DiagnosticLocation{NodeID: id, Path: "nodes." + id}
		fail := func(message, hint string) {
			v.error("E_HUMAN_INVALID", message, loc, hint)
		}
		if n.Loop != nil {
			fail("human node cannot declare a loop", "remove the loop; approval is a single decision")
		}
		out, ok := n.Outputs["decision"]
		if !ok || out.Type != ValueTypeString || len(n.Outputs) != 1 {
			fail("human node must declare exactly one output \"decision\" of type string", "declare outputs: {decision: {type: string, required: true}}")
		}
		allowed := fields.AllowedKeys(HumanConfigFields(), n.Config)
		for key, value := range n.Config {
			if !allowed[key] {
				fail(fmt.Sprintf("unsupported human config key %q", key), "use only prompt and context_inputs")
				continue
			}
			switch key {
			case "prompt":
				s, isString := value.(string)
				if !isString || len(s) > humanPromptLimit {
					fail(fmt.Sprintf("human prompt must be a string of at most %d bytes", humanPromptLimit), "shorten config.prompt")
				}
			case "context_inputs":
				names, isList := stringItems(value)
				if !isList || len(names) > humanContextLimit {
					fail(fmt.Sprintf("context_inputs must list at most %d declared string inputs", humanContextLimit), "list input names, for example [summary]")
					continue
				}
				for _, name := range names {
					if in, declared := n.Inputs[name]; !declared || in.Type != ValueTypeString {
						fail(fmt.Sprintf("context_inputs entry %q is not a declared string input", name), "declare the input with type string")
					}
				}
			}
		}
	}
}

func stringItems(value any) ([]string, bool) {
	switch items := value.(type) {
	case []string:
		return items, true
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}
