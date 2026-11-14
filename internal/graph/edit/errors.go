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
