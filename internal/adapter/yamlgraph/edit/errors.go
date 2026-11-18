package edit

import "fmt"

// Error codes. They are part of the editor API contract (ADR-0020).
const (
	CodeParse         = "E_EDIT_PARSE"
	CodeReadOnly      = "E_EDIT_READONLY"
	CodeUnknownOp     = "E_EDIT_UNKNOWN_OP"
	CodeInvalidOp     = "E_EDIT_INVALID_OP"
	CodeTooManyOps    = "E_EDIT_TOO_MANY_OPS"
	CodeNodeExists    = "E_EDIT_NODE_EXISTS"
	CodeNodeNotFound  = "E_EDIT_NODE_NOT_FOUND"
	CodeNodeID        = "E_EDIT_NODE_ID_INVALID"
	CodeNodeType      = "E_EDIT_NODE_TYPE_INVALID"
	CodeEdgeNotFound  = "E_EDIT_EDGE_NOT_FOUND"
	CodeCannotConnect = "E_EDIT_CANNOT_CONNECT"
	CodeLayout        = "E_EDIT_LAYOUT_INVALID"
	CodeSafetyNet     = "E_EDIT_SAFETY_NET"

	// Codes added by F2.2 (Node Inspector).
	CodeConfigPath   = "E_EDIT_CONFIG_PATH"        // a config path crosses a value that is not a mapping
	CodeValue        = "E_EDIT_VALUE_INVALID"      // a value cannot be written as YAML the engine reads
	CodePortInvalid  = "E_EDIT_PORT_INVALID"       // a port name or type is not valid
	CodePortExists   = "E_EDIT_PORT_EXISTS"        // add_port or a rename to an existing port
	CodePortNotFound = "E_EDIT_PORT_NOT_FOUND"     // the port does not exist
	CodePortConflict = "E_EDIT_PORT_TYPE_CONFLICT" // a type change would break a connected binding
	CodeLoopInvalid  = "E_EDIT_LOOP_INVALID"       // set_loop that cannot be made valid
	CodeUnitsInvalid = "E_EDIT_UNITS_INVALID"      // set_units naming a missing or repeated node

	// Codes added by F2.3 (Edge / Data Mapping Editor).
	CodeGraphInputExists       = "E_EDIT_GRAPH_INPUT_EXISTS"        // add_graph_input or a rename onto an existing input
	CodeGraphInputNotFound     = "E_EDIT_GRAPH_INPUT_NOT_FOUND"     // the graph input does not exist
	CodeGraphInputTypeConflict = "E_EDIT_GRAPH_INPUT_TYPE_CONFLICT" // a retype would break a binding
	CodeBindingNotFound        = "E_EDIT_BINDING_NOT_FOUND"         // remove_graph_input_binding of a binding that is not there
	CodeGraphOutputInvalid     = "E_EDIT_GRAPH_OUTPUT_INVALID"      // a graph output name or source that cannot be used
)

// Error is a structured, actionable editing failure: what failed, which
// operation, which node, edge or path, and how to correct it.
type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	OpIndex    int    `json:"op_index"` // -1 when the failure is not tied to one operation
	Node       string `json:"node,omitempty"`
	Edge       string `json:"edge,omitempty"`
	Path       string `json:"path,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	// Reasons lists why a file is read-only (CodeReadOnly only).
	Reasons []string `json:"reasons,omitempty"`
}

func (e *Error) Error() string {
	if e.OpIndex >= 0 {
		return fmt.Sprintf("%s: operation %d: %s", e.Code, e.OpIndex, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func fail(code, message, suggestion string) *Error {
	return &Error{Code: code, Message: message, OpIndex: -1, Suggestion: suggestion}
}
