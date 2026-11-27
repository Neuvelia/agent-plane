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

// Values of human config.on_reject (ADR-0043). The default, absent or "fail",
// is the ADR-0017 behavior: a rejection fails the node and the run.
const (
	HumanRejectFail     = "fail"
	HumanRejectContinue = "continue"
)

// validateHumanNodes enforces ADR-0017: a human node publishes exactly one
// string output "decision", takes only prompt/context_inputs configuration,
// and never carries a loop.
func (v *definitionValidator) validateHumanNodes() {
	for _, id := range v.sortedNodes() {
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

// validateHumanOnReject accepts "fail" or "continue". "continue" lets a
// rejection succeed the node, and every node bound to the decision then runs
// (ADR-0043). So the decision must reach only deterministic checks: each node
// that receives it must be a Gate that reads it, and at least one Gate or graph
// output must judge it. Any other consumer (a Shell, an Agent, or a loop node,
// whose producer runs before its Gate) would run its side effects on a
// rejection.
func (v *definitionValidator) validateHumanOnReject(id string, value any, loc DiagnosticLocation) {
	mode, isString := value.(string)
	if !isString || (mode != HumanRejectFail && mode != HumanRejectContinue) {
		v.error("E_HUMAN_INVALID", `human on_reject must be "fail" or "continue"`, loc, `use on_reject: fail (default) or on_reject: continue`)
		return
	}
	if mode != HumanRejectContinue {
		return
	}
	judged, unchecked := v.decisionConsumers(id)
	for _, target := range unchecked {
		v.error("E_HUMAN_REJECT_UNROUTED", fmt.Sprintf("on_reject: continue lets node %q run after a rejection: only a Gate that reads the decision may receive it", target), loc,
			"bind "+id+".outputs.decision only to Gate inputs the Gate reads or to graph outputs, or remove on_reject")
	}
	if !judged && len(unchecked) == 0 {
		v.error("E_HUMAN_REJECT_UNROUTED", "on_reject: continue needs a deterministic check of the decision: no Gate or graph output reads it", loc,
			"bind "+id+".outputs.decision to a Gate input the Gate reads, or to a graph output, or remove on_reject")
	}
}

// decisionConsumers reports whether the decision of human node id reaches a
// graph output or a Gate node that reads it, and lists, sorted and unique, the
// other nodes that receive it.
func (v *definitionValidator) decisionConsumers(id string) (bool, []string) {
	from := NodeOutputReference(id + ".outputs.decision")
	judged := false
	for _, o := range v.definition.Outputs {
		judged = judged || o.From == from
	}
	unchecked := map[string]bool{}
	for _, e := range v.definition.Edges {
		to, ok := parseNodeInputReference(string(e.To))
		if e.From != from || !ok {
			continue
		}
		target, found := v.definition.Nodes[to.nodeID]
		if !found {
			continue
		}
		if target.Type == nodeTypeGate && v.reads(target, to.portID) {
			judged = true
			continue
		}
		unchecked[to.nodeID] = true
	}
	return judged, sortedKeys(unchecked)
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
