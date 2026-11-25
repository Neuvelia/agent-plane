package graph

import "fmt"

func (v *definitionValidator) validateTimeouts() {
	for _, nodeID := range sortedNodeIDs(v.definition.Nodes) {
		node := v.definition.Nodes[nodeID]
		if node.TimeoutSeconds == nil {
			continue
		}
		path := fieldPath("nodes", nodeID, "timeout_seconds")
		location := DiagnosticLocation{NodeID: nodeID, Path: path}
		if node.Type == "human" {
			v.error("E_TIMEOUT_INVALID", fmt.Sprintf("human node %s cannot set timeout_seconds", quotedKey(nodeID)), location, "remove timeout_seconds; a human node ends by decision or run cancel")
			continue
		}
		v.checkTimeoutRange(*node.TimeoutSeconds, location)
	}
	if limits := v.definition.Limits; limits != nil && limits.RunTimeoutSeconds != nil {
		v.checkTimeoutRange(*limits.RunTimeoutSeconds, DiagnosticLocation{Path: "limits.run_timeout_seconds"})
	}
}

func (v *definitionValidator) checkTimeoutRange(seconds int, location DiagnosticLocation) {
	if seconds >= timeoutMinSeconds && seconds <= timeoutMaxSeconds {
		return
	}
	v.error("E_TIMEOUT_INVALID", fmt.Sprintf("%s is %d; it must be an integer from %d to %d", location.Path, seconds, timeoutMinSeconds, timeoutMaxSeconds), location, "use whole seconds from 1 to 86400, or omit the field for no limit")
}
