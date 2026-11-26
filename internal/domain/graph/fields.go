package graph

import "agentplane/internal/fields"

// HumanConfigFields describes the human node configuration validateHumanNodes
// accepts (F2.2, ADR-0017). validateHumanNodes derives its allowed keys from it.
func HumanConfigFields() []fields.Field {
	return []fields.Field{
		{ID: "human.config.prompt", Key: "prompt", Kind: fields.Text, MaxBytes: humanPromptLimit},
		{ID: "human.config.context_inputs", Key: "context_inputs", Kind: fields.List, MaxItems: humanContextLimit,
			Item: &fields.Field{ID: "human.config.context_inputs.item", Kind: fields.InputName, InputTypes: []string{string(ValueTypeString)}}},
		{ID: "human.config.on_reject", Key: "on_reject", Kind: fields.Enum, Enum: []string{HumanRejectFail, HumanRejectContinue}, Default: HumanRejectFail},
	}
}

// HumanOutputPorts is the one output a human node declares; it cannot be edited.
func HumanOutputPorts() []fields.Port {
	return []fields.Port{{Name: "decision", Type: string(ValueTypeString), Required: true}}
}

// Loop budget bounds (ADR-0014, ADR-0018).
const (
	loopMinAttempts     = 1
	loopMaxAttempts     = 10
	loopDefaultAttempts = 3
)

// LoopFields describes the loop block's own settings; its embedded Gate is
// described by the Gate executor and its bindings by LoopGateKinds' rules.
func LoopFields() []fields.Field {
	return []fields.Field{
		{ID: "loop.max_attempts", Key: "max_attempts", Kind: fields.Int, Default: loopDefaultAttempts,
			Min: fields.IntPtr(loopMinAttempts), Max: fields.IntPtr(loopMaxAttempts)},
	}
}

// LoopProducer reports whether a node type may declare a loop.
func LoopProducer(nodeType string) bool { return nodeType == "agent" || nodeType == "shell" }

// LoopGateKinds lists the embedded Gate kinds a loop of nodeType may select,
// or nil when the type cannot loop. Only an Agent loop may check changed files.
func LoopGateKinds(nodeType string) []string {
	switch nodeType {
	case "shell":
		return []string{"command", "json_predicate", "reported_file_scope", "test_command"}
	case "agent":
		return []string{"command", "json_predicate", "reported_file_scope", "changed_files_scope", "test_command"}
	}
	return nil
}

// Timeout bounds in seconds (F3.4, ADR-0028).
const (
	timeoutMinSeconds = 1
	timeoutMaxSeconds = 86400
)

// NodeTimeoutFields describes the node-level timeout_seconds key. A human node
// cannot carry it; it has no default because absent means no limit.
func NodeTimeoutFields() []fields.Field {
	return []fields.Field{
		{ID: "node.timeout_seconds", Key: "timeout_seconds", Kind: fields.Int,
			Min: fields.IntPtr(timeoutMinSeconds), Max: fields.IntPtr(timeoutMaxSeconds)},
	}
}

// RunLimitFields describes the graph-level limits block.
func RunLimitFields() []fields.Field {
	return []fields.Field{
		{ID: "limits.run_timeout_seconds", Key: "run_timeout_seconds", Kind: fields.Int,
			Min: fields.IntPtr(timeoutMinSeconds), Max: fields.IntPtr(timeoutMaxSeconds)},
	}
}
