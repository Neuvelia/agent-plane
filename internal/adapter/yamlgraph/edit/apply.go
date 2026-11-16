package edit

import (
	"agentplane/internal/adapter/yamlgraph"
	"bytes"
	"fmt"
	"strings"

	"agentplane/internal/domain/graph"

	"go.yaml.in/yaml/v3"
)

// QueryResult is the answer of a query operation such as can_connect.
type QueryResult struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// OpResult reports one operation of a batch.
type OpResult struct {
	Index int          `json:"index"`
	Op    string       `json:"op"`
	Query *QueryResult `json:"query,omitempty"`
}

// Result is the outcome of a successful batch. Text is the whole new file; the
// caller decides whether to save it.
type Result struct {
	Text    []byte
	Results []OpResult
}

// session is one batch in progress: the tree being patched and the typed
// definition predicting what the patch must mean.
type session struct {
	doc     *yaml.Node        // document node
	top     *yaml.Node        // top-level mapping
	want    *graph.Definition // typed prediction
	renames [][2]string       // old element path -> new, for blank-line restoration
}

// tamper lets a test corrupt the tree after the operations ran, to prove the
// safety net refuses a result that does not mean what was asked.
var tamper func(doc *yaml.Node)

// Inspect reports why src cannot be patched on the canvas. An empty result means
// it can: valid schema-v0 YAML without anchors, aliases or merge keys. The YAML
// text can still be edited by hand in either case.
func Inspect(src []byte) []string {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return []string{"parse_error"}
	}
	reasons := ReadOnlyReasons(&doc)
	if _, err := yamlgraph.Parse(src); err != nil {
		reasons = append(reasons, "parse_error")
	}
	return reasons
}

// Apply applies a batch to src atomically: either every operation applies and the
// result passes the safety net, or an *Error is returned and nothing is produced.
func Apply(src []byte, ops []Op) (*Result, error) {
	if len(ops) > MaxOps {
		return nil, fail(CodeTooManyOps, fmt.Sprintf("a batch may hold at most %d operations, got %d", MaxOps, len(ops)), "split the batch")
	}
	before, err := yamlgraph.Parse(src)
	if err != nil {
		e := fail(CodeParse, "the graph does not parse: "+err.Error(), "fix the YAML in the text view, then edit again")
		e.Reasons = []string{"parse_error"}
		return nil, e
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fail(CodeParse, "the graph is not valid YAML: "+err.Error(), "fix the YAML in the text view, then edit again")
	}
	if reasons := ReadOnlyReasons(&doc); len(reasons) > 0 {
		e := fail(CodeReadOnly, "the file uses "+strings.Join(reasons, ", ")+"; edits could reach several nodes through it, so the canvas is read-only", "edit the YAML text, or replace the shared parts with plain values")
		e.Reasons = reasons
		return nil, e
	}
	want, err := yamlgraph.Parse(src) // a second, independent copy for the typed prediction
	if err != nil {
		return nil, fail(CodeParse, err.Error(), "")
	}
	s := &session{doc: &doc, top: docMapping(&doc), want: want}
	if s.top.Kind != yaml.MappingNode {
		return nil, fail(CodeParse, "the graph is not a mapping", "")
	}

	results := make([]OpResult, 0, len(ops))
	presentation := true
	for i, op := range ops {
		res := OpResult{Index: i, Op: op.Op}
		q, err := s.apply(op)
		if err != nil {
			err.OpIndex = i
			return nil, err
		}
		res.Query = q
		results = append(results, res)
		if !op.presentation() && !op.query() {
			presentation = false
		}
	}
	if tamper != nil {
		tamper(&doc)
	}

	text, err := encode(src, &doc, s.renames)
	if err != nil {
		return nil, fail(CodeSafetyNet, err.Error(), "")
	}
	if err := safetyNet(text, before, want, presentation); err != nil {
		return nil, err
	}
	return &Result{Text: text, Results: results}, nil
}

// safetyNet proves the written text means what the typed edits predicted: it
// parses, the whole normalised definition equals the prediction, and a batch of
// presentation operations leaves the semantic version untouched.
func safetyNet(text []byte, before, want *graph.Definition, presentation bool) *Error {
	got, err := yamlgraph.Parse(text)
	if err != nil {
		return fail(CodeSafetyNet, "the edited file does not parse, nothing was changed: "+err.Error(), "report this as a bug; edit the YAML text instead")
	}
	gotYAML, err1 := yamlgraph.Marshal(got)
	wantYAML, err2 := yamlgraph.Marshal(want)
	if err1 != nil || err2 != nil || !bytes.Equal(gotYAML, wantYAML) {
		return fail(CodeSafetyNet, "the edited file does not mean what the operations asked for, nothing was changed", "an edit reached more than it targeted; edit the YAML text instead")
	}
	if presentation {
		v1, e1 := yamlgraph.SemanticVersion(before)
		v2, e2 := yamlgraph.SemanticVersion(got)
		if e1 != nil || e2 != nil || v1 != v2 {
			return fail(CodeSafetyNet, "a presentation edit changed the semantic version, nothing was changed", "report this as a bug")
		}
	}
	return nil
}

func (s *session) apply(op Op) (*QueryResult, *Error) {
	switch op.Op {
	case OpAddNode:
		return nil, s.addNode(op)
	case OpRemoveNode:
		return nil, s.removeNode(op)
	case OpRenameNode:
		return nil, s.renameNode(op)
	case OpSetName:
		return nil, s.setName(op)
	case OpSetLayout:
		return nil, s.setLayout(op)
	case OpAddEdge:
		return nil, s.addEdge(op)
	case OpRemoveEdge:
		return nil, s.removeEdge(op)
	case OpCanConnect:
		return s.canConnect(op.From, op.To), nil
	case OpSetConfig:
		return nil, s.setConfig(op)
	case OpUnsetConfig:
		return nil, s.unsetConfig(op)
	case OpAddPort:
		return nil, s.addPort(op)
	case OpUpdatePort:
		return nil, s.updatePort(op)
	case OpRemovePort:
		return nil, s.removePort(op)
	case OpSetLoop:
		return nil, s.setLoop(op)
	case OpClearLoop:
		return nil, s.clearLoop(op)
	case OpSetUnits:
		return nil, s.setUnits(op)
	case OpSetLoopAttempts:
		return nil, s.setLoopAttempts(op)
	case OpSetLoopConfig:
		return nil, s.setConfigAt(op, true)
	case OpUnsetLoopConfig:
		return nil, s.unsetConfigAt(op, true)
	case OpSetLoopGateInput:
		return nil, s.setLoopGateInput(op)
	case OpRemoveLoopGateInput:
		return nil, s.removeLoopGateInput(op)
	case OpSetLoopBindings:
		return nil, s.setLoopBindings(op)
	}
	return nil, &Error{Code: CodeUnknownOp, Message: fmt.Sprintf("unknown operation %q", op.Op), OpIndex: -1}
}
