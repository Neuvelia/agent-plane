package runtime

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

// ApprovalRequest is what a DecisionProvider shows to a person. Prompt and
// Context are already redacted and bounded.
type ApprovalRequest struct {
	RunID     events.RunID
	NodeID    string
	NodeRunID events.NodeRunID
	Prompt    string
	Context   []events.ApprovalContext
}

// Decision is a human's answer. Comment is bounded by the event contract.
type Decision struct {
	Approve bool
	Comment string
}

// DecisionProvider blocks until a human decides or ctx ends. The scheduler
// calls it once per human node run, so a provider that serves several
// channels (CLI, API) owns first-decision-wins and idempotency (ADR-0017).
type DecisionProvider interface {
	Decide(context.Context, ApprovalRequest) (Decision, error)
}

type rejectedError struct{}

func (rejectedError) Error() string          { return "human approval rejected" }
func (rejectedError) DiagnosticCode() string { return "human_rejected" }

type decisionError struct{ msg string }

func (e decisionError) Error() string        { return e.msg }
func (decisionError) DiagnosticCode() string { return "human_decision_invalid" }

func hasHumanNode(d *graph.Definition) (string, bool) {
	for _, id := range sortedDefinitionIDs(d) {
		if d.Nodes[id].Type == "human" {
			return id, true
		}
	}
	return "", false
}

func preflightHuman(d *graph.Definition, p DecisionProvider) error {
	id, ok := hasHumanNode(d)
	if !ok || p != nil {
		return nil
	}
	return &PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{
		Severity: graph.SeverityError, Code: "E_HUMAN_NO_PROVIDER", GraphID: d.ID,
		Location:   graph.DiagnosticLocation{NodeID: id, Path: "nodes." + id},
		Message:    "graph contains a human node but no decision provider is configured",
		Suggestion: "run with --approve/--reject NODE, an interactive terminal, or the local API",
	}}}}
}

func redactText(r *logRedactor, s string) string {
	c := r.clone()
	b := append([]byte{}, c.Write(events.Stdout, []byte(strings.ToValidUTF8(s, "")))...)
	b = append(b, c.Flush(events.Stdout)...)
	return string(b)
}

func truncateUTF8(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit], true
}

// humanNode runs on a worker like any node. The node stays "running" for the
// scheduler; waiting is the interval between approval_requested and
// approval_decided. The batch barrier is unchanged, so nodes that share its
// batch keep running while this one waits.
func (m *runMachine) humanNode(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, r *logRedactor) completedNode {
	done := func(out map[string]graph.Value, err error) completedNode {
		return completedNode{id: id, outputs: out, err: err}
	}
	if err := ctx.Err(); err != nil {
		return done(nil, err)
	}
	if m.decisions == nil {
		return done(nil, decisionError{"no decision provider"})
	}
	prompt, _ := n.Config["prompt"].(string)
	prompt, _ = truncateUTF8(redactText(r, prompt), events.MaxApprovalPrompt)
	names, _ := stringItems(n.Config["context_inputs"])
	contextItems := make([]events.ApprovalContext, 0, len(names))
	for _, name := range names {
		value, ok := in[name].AsString()
		if !ok {
			continue
		}
		text, cut := truncateUTF8(redactText(r, value), events.MaxApprovalContextValue)
		contextItems = append(contextItems, events.ApprovalContext{Name: name, Value: text, Truncated: cut})
	}
	nodeRunID := m.nodes[id].NodeRunID
	m.appendEvent(events.Event{Type: events.ApprovalRequested, NodeID: id, NodeRunID: nodeRunID, Payload: events.ApprovalRequestedPayload{Prompt: prompt, Context: contextItems}})
	decision, err := m.decisions.Decide(ctx, ApprovalRequest{RunID: m.result.RunID, NodeID: id, NodeRunID: nodeRunID, Prompt: prompt, Context: contextItems})
	if err != nil {
		if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			return done(nil, err)
		}
		return done(nil, decisionError{"decision provider failed"})
	}
	// A decision a provider already accepted is a fact the person acted on, so it
	// is recorded even if cancellation arrived at the same moment; the node is
	// then cancelled rather than succeeded or failed.
	comment, _ := truncateUTF8(redactText(r, decision.Comment), events.MaxApprovalComment)
	verdict := events.ApprovalRejected
	if decision.Approve {
		verdict = events.ApprovalApproved
	}
	m.appendEvent(events.Event{Type: events.ApprovalDecided, NodeID: id, NodeRunID: nodeRunID, Payload: events.ApprovalDecidedPayload{Decision: verdict, Comment: comment}})
	if err := ctx.Err(); err != nil {
		return done(nil, err)
	}
	if !decision.Approve {
		return done(nil, rejectedError{})
	}
	return done(map[string]graph.Value{"decision": graph.NewStringValue(events.ApprovalApproved)}, nil)
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
