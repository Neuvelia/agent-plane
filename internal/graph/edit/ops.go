package edit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
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

// Operation names of ops_version 1 added by F2.2 (Node Inspector).
const (
	OpSetConfig   = "set_config"
	OpUnsetConfig = "unset_config"
	OpAddPort     = "add_port"
	OpUpdatePort  = "update_port"
	OpRemovePort  = "remove_port"
	OpSetLoop     = "set_loop"
	OpClearLoop   = "clear_loop"
	OpSetUnits    = "set_units"
)

// Port directions of the port operations.
const (
	DirectionInput  = "input"
	DirectionOutput = "output"
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

	// F2.2 fields. A port operation names its port in Port because Name is
	// set_name's.
	Node        string          `json:"node,omitempty"`         // the node a config, port or loop operation edits
	Path        []string        `json:"path,omitempty"`         // config keys from the node's config mapping down
	Value       json.RawMessage `json:"value,omitempty"`        // set_config: a JSON scalar, list or object
	Direction   string          `json:"direction,omitempty"`    // input or output
	Port        string          `json:"port,omitempty"`         // the port a port operation edits
	NewPort     string          `json:"new_port,omitempty"`     // update_port: rename the port
	Required    *bool           `json:"required,omitempty"`     // add_port, update_port
	MaxAttempts *int            `json:"max_attempts,omitempty"` // set_loop; absent means the default
	Gate        *LoopGate       `json:"gate,omitempty"`         // set_loop
	Bindings    []LoopBinding   `json:"bindings,omitempty"`     // set_loop
	Units       []string        `json:"units,omitempty"`        // set_units; an empty list clears them
}

// LoopGate is the embedded Gate of set_loop.
type LoopGate struct {
	Inputs map[string]PortSpec `json:"inputs,omitempty"`
	Config json.RawMessage     `json:"config"`
}

// PortSpec declares a port of a loop's embedded Gate.
type PortSpec struct {
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

// LoopBinding feeds a loop's Gate input from the candidate's output or the
// node's input.
type LoopBinding struct {
	From string `json:"from"`
	To   string `json:"to"`
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
	case OpSetConfig:
		return firstErr(need(o.Node != "", "node"), need(validPath(o.Path), "a path of non-empty keys"), need(len(o.Value) > 0, "value"))
	case OpUnsetConfig:
		return firstErr(need(o.Node != "", "node"), need(validPath(o.Path), "a path of non-empty keys"))
	case OpAddPort:
		return firstErr(o.portCheck(need), need(o.Type != "", "type"))
	case OpUpdatePort:
		return firstErr(o.portCheck(need), need(o.NewPort != "" || o.Type != "" || o.Required != nil, "at least one of new_port, type or required"))
	case OpRemovePort:
		return o.portCheck(need)
	case OpSetLoop:
		return firstErr(need(o.Node != "", "node"), need(o.Gate != nil && len(o.Gate.Config) > 0, "gate with config"))
	case OpClearLoop:
		return need(o.Node != "", "node")
	case OpSetUnits:
		return need(o.Units != nil, "units (use an empty list to clear them)")
	default:
		return &Error{Code: CodeUnknownOp, Message: fmt.Sprintf("unknown operation %q", o.Op), OpIndex: -1, Suggestion: "operations of ops_version 1: " + strings.Join(opNames, ", ")}
	}
}

// opNames lists the operations of ops_version 1, for the unknown-operation hint.
var opNames = []string{OpAddNode, OpRemoveNode, OpRenameNode, OpSetName, OpSetLayout, OpAddEdge, OpRemoveEdge, OpCanConnect,
	OpSetConfig, OpUnsetConfig, OpAddPort, OpUpdatePort, OpRemovePort, OpSetLoop, OpClearLoop, OpSetUnits}

func (o Op) portCheck(need func(bool, string) *Error) *Error {
	return firstErr(need(o.Node != "", "node"), need(o.Direction == DirectionInput || o.Direction == DirectionOutput, "direction input or output"), need(o.Port != "", "port"))
}

// maxPathDepth bounds a config path; deeper config is edited as text.
const maxPathDepth = 16

func validPath(path []string) bool {
	if len(path) == 0 || len(path) > maxPathDepth {
		return false
	}
	for _, k := range path {
		if k == "" {
			return false
		}
	}
	return true
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
