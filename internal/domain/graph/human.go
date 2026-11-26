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

// Values of human config.on_reject (ADR-0036). The default, absent or "fail",
// is the ADR-0017 behavior: a rejection fails the node and the run.
const (
	HumanRejectFail     = "fail"
	HumanRejectContinue = "continue"
)

// validateHumanNodes enforces ADR-0017: a human node publishes exactly one
// string output "decision", takes only prompt/context_inputs configuration,
// and never carries a loop.
func (v *definitionValidator) validateHumanNodes() {
	for _, id := range v.sortedNodeIDs() {
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
				fail(fmt.Sprintf("unsupported human config key %s", quotedKey(key)), "use only prompt, context_inputs and on_reject")
				continue
			}
			switch key {
			case "on_reject":
				v.validateHumanOnReject(id, value, loc)
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
						fail(fmt.Sprintf("context_inputs entry %s is not a declared string input", quotedKey(name)), "declare the input with type string")
					}
				}
			}
		}
	}
}

// validateHumanOnReject accepts "fail" or "continue". "continue" routes a
// rejection into the graph, so it needs a declared path: some edge or graph
// output must consume the node's decision, otherwise a rejection would pass
// silently (ADR-0036).
func (v *definitionValidator) validateHumanOnReject(id string, value any, loc DiagnosticLocation) {
	mode, isString := value.(string)
	if !isString || (mode != HumanRejectFail && mode != HumanRejectContinue) {
		v.error("E_HUMAN_INVALID", `human on_reject must be "fail" or "continue"`, loc, `use on_reject: fail (default) or on_reject: continue`)
		return
	}
	if mode == HumanRejectContinue && !v.decisionConsumed(id) {
		v.error("E_HUMAN_REJECT_UNROUTED", "on_reject: continue needs a path: nothing consumes the decision output", loc,
			"bind "+id+".outputs.decision to a node input or graph output, or remove on_reject")
	}
}

func (v *definitionValidator) decisionConsumed(id string) bool {
	from := NodeOutputReference(id + ".outputs.decision")
	for _, e := range v.definition.Edges {
		if e.From != from {
			continue
		}
		// A target that ignores the input (a shell node without input_env)
		// would run the same on rejection, so it is not a path.
		to, ok := parseNodeInputReference(string(e.To))
		if !ok {
			return true
		}
		if target, found := v.definition.Nodes[to.nodeID]; !found || nodeConsumesInput(target, to.portID) {
			return true
		}
	}
	for _, o := range v.definition.Outputs {
		if o.From == from {
			return true
		}
	}
	return false
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
