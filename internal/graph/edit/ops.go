package edit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

// OpsVersion is the version of the operation vocabulary (ADR-0020).
const OpsVersion = 1

// MaxOps bounds one batch (ADR-0021).
const MaxOps = 500

// Position is a canvas position in pixels.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Operation names of ops_version 1 defined by F2.1.
const (
	OpAddNode    = "add_node"
	OpRemoveNode = "remove_node"
	OpRenameNode = "rename_node"
	OpSetName    = "set_name"
	OpSetLayout  = "set_layout"
	OpAddEdge    = "add_edge"
	OpRemoveEdge = "remove_edge"
	OpCanConnect = "can_connect"
)

// Op is one editor operation. Which fields apply depends on Op.
type Op struct {
	Op     string              `json:"op"`
	ID     string              `json:"id,omitempty"`
	Type   string              `json:"type,omitempty"`
	NewID  string              `json:"new_id,omitempty"`
	Name   *string             `json:"name,omitempty"`
	From   string              `json:"from,omitempty"`
	To     string              `json:"to,omitempty"`
	Layout *Position           `json:"layout,omitempty"`
	Nodes  map[string]Position `json:"nodes,omitempty"`
	Clear  bool                `json:"clear,omitempty"`
}

// presentation operations never change the semantic version.
func (o Op) presentation() bool { return o.Op == OpSetName || o.Op == OpSetLayout }

func (o Op) query() bool { return o.Op == OpCanConnect }

// Decode parses a JSON array of operations strictly: unknown fields and unknown
// operations are errors, and each operation's required fields are checked.
func Decode(data []byte) ([]Op, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ops []Op
	if err := dec.Decode(&ops); err != nil {
		return nil, fail(CodeInvalidOp, "operations are not a valid JSON array of operation objects: "+err.Error(), "send [{\"op\": ...}, ...]")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fail(CodeInvalidOp, "operations must be exactly one JSON array", "")
	}
	if len(ops) > MaxOps {
		return nil, fail(CodeTooManyOps, fmt.Sprintf("a batch may hold at most %d operations, got %d", MaxOps, len(ops)), "split the batch")
	}
	for i, op := range ops {
		if err := op.check(); err != nil {
			err.OpIndex = i
			return nil, err
		}
	}
	return ops, nil
}

func (o Op) check() *Error {
	need := func(cond bool, what string) *Error {
		if cond {
			return nil
		}
		return &Error{Code: CodeInvalidOp, Message: fmt.Sprintf("%s requires %s", o.Op, what), OpIndex: -1}
	}
	switch o.Op {
	case OpAddNode:
		return firstErr(need(o.ID != "", "id"), need(o.Type != "", "type"))
	case OpRemoveNode:
		return need(o.ID != "", "id")
	case OpRenameNode:
		return firstErr(need(o.ID != "", "id"), need(o.NewID != "", "new_id"))
	case OpSetName:
		return need(o.Name != nil, "name (use an empty string to clear it)")
	case OpSetLayout:
		return need(o.Clear != (len(o.Nodes) > 0), "exactly one of nodes or clear")
	case OpAddEdge, OpRemoveEdge, OpCanConnect:
		return firstErr(need(o.From != "", "from"), need(o.To != "", "to"))
	default:
		return &Error{Code: CodeUnknownOp, Message: fmt.Sprintf("unknown operation %q", o.Op), OpIndex: -1, Suggestion: "operations of ops_version 1: add_node, remove_node, rename_node, set_name, set_layout, add_edge, remove_edge, can_connect"}
	}
}

func firstErr(errs ...*Error) *Error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// round turns a canvas coordinate into the integer that is stored.
func (p Position) round() (int, int, bool) {
	const limit = 100000
	if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
		return 0, 0, false
	}
	x, y := int(math.Round(p.X)), int(math.Round(p.Y))
	if x < -limit || x > limit || y < -limit || y > limit {
		return 0, 0, false
	}
	return x, y, true
}
