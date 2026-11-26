package graph

import "fmt"

func (v *definitionValidator) validateTimeouts() {
	for _, nodeID := range v.sortedNodes() {
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
		v.checkTimeoutRange(*node.TimeoutSeconds, location, "timeout_seconds")
	}
	if limits := v.definition.Limits; limits != nil && limits.RunTimeoutSeconds != nil {
		v.checkTimeoutRange(*limits.RunTimeoutSeconds, DiagnosticLocation{Path: "limits.run_timeout_seconds"}, "limits.run_timeout_seconds")
	}
}

// checkTimeoutRange names the field rather than the path: for a node whose id
// fails the grammar the path is only the nodes collection.
func (v *definitionValidator) checkTimeoutRange(seconds int, location DiagnosticLocation, field string) {
	if seconds >= timeoutMinSeconds && seconds <= timeoutMaxSeconds {
		return
	}
	v.error("E_TIMEOUT_INVALID", fmt.Sprintf("%s is %d; it must be an integer from %d to %d", field, seconds, timeoutMinSeconds, timeoutMaxSeconds), location, "use whole seconds from 1 to 86400, or omit the field for no limit")
}
