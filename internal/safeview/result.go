package safeview

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"agentplane/internal/events"
	"agentplane/internal/history"
	"agentplane/internal/runtime"
)

const (
	maxResultProjectionBytes = 3 << 20
	maxResultPorts           = 512
	maxResultDiagnosticCodes = 128
)

// ProjectedResult is the detached, allowlisted input to completed-run history.
// Result is a versioned positional snapshot. Nodes are independently bounded
// indexes; their Status and identities live in typed history columns.
type ProjectedResult struct {
	Result   history.SafeJSON
	Nodes    []history.NodeOutcome
	Complete bool
}

// ProjectAttemptSummary produces an indexed attempt outcome from the typed
// runtime summary. Unsafe optional fact references are omitted and reported
// through the completeness bit; indexed identities and status fail closed.
func (p *Projector) ProjectAttemptSummary(summary runtime.AttemptSummary) (history.AttemptOutcome, bool, error) {
	if p == nil || p.redactor == nil || !p.requiredResultIdentity(string(summary.AttemptID)) || !p.requiredResultIdentity(summary.NodeID) || !p.requiredResultIdentity(string(summary.NodeRunID)) || !validAttemptOutcome(summary.Outcome) || p.identity(string(summary.Outcome)) {
		return history.AttemptOutcome{}, false, errors.New("required attempt outcome identity is unsafe")
	}
	complete := true
	var check any
	if ref := summary.Check; ref != nil {
		kind, kindOK := checkKindCode(ref.Kind)
		verdict, verdictOK := checkVerdictCode(ref.Verdict)
		if !ref.IdentityRedacted && p.requiredResultIdentity(ref.CheckID) && ref.Sequence != 0 && kindOK && verdictOK {
			check = []any{ref.CheckID, kind, verdict, ref.Sequence}
		} else {
			complete = false
		}
	}
	var envelope any
	if ref := summary.Envelope; ref != nil {
		verdict, verdictOK := checkVerdictCode(ref.Verdict)
		if !ref.IdentityRedacted && p.requiredResultIdentity(ref.UnitID) && p.requiredResultIdentity(ref.CheckID) && ref.Sequence != 0 && verdictOK {
			envelope = []any{ref.UnitID, ref.CheckID, verdict, ref.Sequence}
		} else {
			complete = false
		}
	}
	body := []any{1, summary.NodeID, check, envelope, complete}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxResultProjectionBytes {
		return history.AttemptOutcome{}, false, errors.New("safe attempt outcome exceeds indexed limit")
	}
	projection, err := history.ParseSafeJSON(encoded, p.forbidden)
	if err != nil {
		return history.AttemptOutcome{}, false, err
	}
	return history.AttemptOutcome{AttemptID: string(summary.AttemptID), NodeRunID: string(summary.NodeRunID), Status: string(summary.Outcome), Projection: projection}, complete, nil
}

func validAttemptOutcome(outcome runtime.AttemptOutcome) bool {
	switch outcome {
	case runtime.AttemptAccepted, runtime.AttemptRejected, runtime.AttemptNeedsIntervention, runtime.AttemptCancelled, runtime.AttemptExecutionError:
		return true
	default:
		return false
	}
}

func checkKindCode(kind events.CheckKind) (int, bool) {
	switch kind {
	case events.CheckCommand:
		return 1, true
	case events.CheckJSONPredicate:
		return 2, true
	case events.CheckReportedFileScope:
		return 3, true
	case events.CheckChangedFilesScope:
		return 4, true
	default:
		return 0, false
	}
}

func checkVerdictCode(verdict events.CheckVerdict) (int, bool) {
	switch verdict {
	case events.CheckGreen:
		return 1, true
	case events.CheckRed:
		return 2, true
	default:
		return 0, false
	}
}

// ProjectResult inspects typed runtime facts without serializing RunResult.
// Freeform errors, diagnostic messages, skip reasons, transitions, and raw
// events are never part of the projection.
func (p *Projector) ProjectResult(result runtime.RunResult) (ProjectedResult, error) {
	if p == nil || p.redactor == nil || !p.requiredResultIdentity(result.GraphID) || !p.requiredResultIdentity(result.GraphVersion) || !p.requiredResultIdentity(string(result.RunID)) {
		return ProjectedResult{}, errors.New("required result identity is unsafe")
	}
	state, ok := runStateCode(result.State)
	if !ok {
		return ProjectedResult{}, errors.New("invalid terminal run state")
	}
	outputs, outputComplete := p.resultPorts(result.Outputs)
	failure, failureComplete := p.resultFailure(result.Failure)
	root := []any{1, result.GraphID, result.GraphVersion, string(result.RunID), state, outputs, failure, outputComplete && failureComplete}
	safeResult, finalComplete, err := p.boundedResult(root, outputs, outputComplete && failureComplete)
	if err != nil {
		return ProjectedResult{}, err
	}
	nodes := make([]history.NodeOutcome, 0, len(result.Nodes))
	seen := make(map[string]bool, len(result.Nodes))
	for _, node := range result.Nodes {
		if !p.requiredResultIdentity(node.NodeID) || !p.requiredResultIdentity(string(node.NodeRunID)) || seen[string(node.NodeRunID)] {
			return ProjectedResult{}, errors.New("required node outcome identity is unsafe or repeated")
		}
		seen[string(node.NodeRunID)] = true
		if !terminalNodeState(node.State) || p.identity(string(node.State)) {
			return ProjectedResult{}, errors.New("invalid terminal node state")
		}
		accepted := string(node.AcceptedAttemptID)
		if accepted != "" && !p.requiredResultIdentity(accepted) {
			return ProjectedResult{}, errors.New("accepted attempt identity is unsafe")
		}
		nodeOutputs, nodeComplete := p.resultPorts(node.Outputs)
		nodeFailure, nodeFailureComplete := p.resultFailure(node.Failure)
		nodeComplete = nodeComplete && nodeFailureComplete
		body := []any{1, accepted, nodeOutputs, nodeFailure, nodeComplete}
		projection, complete, err := p.boundedResult(body, nodeOutputs, nodeComplete)
		if err != nil {
			return ProjectedResult{}, fmt.Errorf("node outcome projection unavailable: %w", err)
		}
		finalComplete = finalComplete && complete
		nodes = append(nodes, history.NodeOutcome{NodeRunID: string(node.NodeRunID), NodeID: node.NodeID, Status: string(node.State), Projection: projection})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeRunID < nodes[j].NodeRunID })
	return ProjectedResult{Result: safeResult, Nodes: nodes, Complete: finalComplete}, nil
}

func (p *Projector) requiredResultIdentity(value string) bool {
	return value != "" && len(value) <= 256 && !p.identity(value)
}

func runStateCode(state runtime.RunState) (int, bool) {
	switch state {
	case runtime.RunSucceeded:
		return 1, true
	case runtime.RunFailed:
		return 2, true
	case runtime.RunCancelled:
		return 3, true
	default:
		return 0, false
	}
}

func terminalNodeState(state runtime.NodeState) bool {
	return state == runtime.NodeSucceeded || state == runtime.NodeFailed || state == runtime.NodeSkipped || state == runtime.NodeCancelled
}

// Each port is [name, retained, typed value or null, provenance or null].
// A false retained flag is an explicit omission, never an empty output value.
func (p *Projector) resultPorts(ports []runtime.PortValue) ([]any, bool) {
	items := make([]any, 0, min(len(ports), maxResultPorts))
	complete := len(ports) <= maxResultPorts
	for _, port := range ports[:min(len(ports), maxResultPorts)] {
		if port.Name == "" || len(port.Name) > 256 || p.unsafeLaunchKey(port.Name) {
			complete = false
			continue
		}
		var value any
		retained := false
		projected, kept, itemComplete, err := p.projectTypedValue(port.Value, &launchBudget{})
		if err == nil && kept && !p.unsafeResultNumber(projected) {
			encoded, encodeErr := json.Marshal(projected)
			if encodeErr == nil {
				_, encodeErr = history.ParseSafeJSON(encoded, p.forbidden)
			}
			if encodeErr == nil {
				value, retained = projected, true
			} else {
				itemComplete = false
			}
		}
		complete = complete && err == nil && kept && retained && itemComplete
		var provenance any
		if port.Provenance != nil {
			pr := port.Provenance
			if p.requiredResultIdentity(string(pr.RunID)) && p.requiredResultIdentity(string(pr.NodeRunID)) && p.requiredResultIdentity(string(pr.AttemptID)) && p.requiredResultIdentity(pr.Port) && !CredentialShapedKey(pr.Port) {
				provenance = []any{string(pr.RunID), string(pr.NodeRunID), string(pr.AttemptID), pr.Port}
			} else {
				complete = false
			}
		}
		items = append(items, []any{port.Name, retained, value, provenance})
	}
	return items, complete
}

func (p *Projector) unsafeResultNumber(value any) bool {
	switch item := value.(type) {
	case json.Number:
		return p.identity(string(item))
	case []any:
		for _, nested := range item {
			if p.unsafeResultNumber(nested) {
				return true
			}
		}
	case map[string]any:
		for _, nested := range item {
			if p.unsafeResultNumber(nested) {
				return true
			}
		}
	}
	return false
}

// Only bounded machine codes are retained. Diagnostic prose and errors may
// include arbitrary secrets or repository content and are never examined.
func (p *Projector) resultFailure(failure *runtime.Failure) (any, bool) {
	if failure == nil {
		return nil, true
	}
	code, ok := failureKindCode(failure.Kind)
	complete := ok && len(failure.Diagnostics) <= maxResultDiagnosticCodes
	if !ok {
		code = 0
	}
	diagnostics := make([]any, 0, min(len(failure.Diagnostics), maxResultDiagnosticCodes))
	for _, item := range failure.Diagnostics[:min(len(failure.Diagnostics), maxResultDiagnosticCodes)] {
		if item.Code == "" || len(item.Code) > 256 || p.identity(item.Code) || len(diagnostics) >= maxResultDiagnosticCodes {
			complete = false
			continue
		}
		diagnostics = append(diagnostics, item.Code)
	}
	return []any{code, diagnostics}, complete
}

func failureKindCode(kind runtime.FailureKind) (int, bool) {
	switch kind {
	case runtime.FailureNeedsIntervention:
		return 1, true
	case runtime.FailureExecutor:
		return 2, true
	case runtime.FailureBinding:
		return 3, true
	case runtime.FailureOutputContract:
		return 4, true
	case runtime.FailureGraphOutput:
		return 5, true
	case runtime.FailureScheduler:
		return 6, true
	default:
		return 0, false
	}
}

func (p *Projector) boundedResult(root []any, ports []any, complete bool) (history.SafeJSON, bool, error) {
	for {
		root[len(root)-1] = complete
		encoded, err := json.Marshal(root)
		if err != nil {
			return history.SafeJSON{}, false, errors.New("safe result cannot be encoded")
		}
		if len(encoded) <= maxResultProjectionBytes {
			safe, err := history.ParseSafeJSON(encoded, p.forbidden)
			if err != nil {
				return history.SafeJSON{}, false, err
			}
			if len(safe.Bytes()) > maxResultProjectionBytes {
				return history.SafeJSON{}, false, errors.New("canonical safe result exceeds indexed projection limit")
			}
			return safe, complete, nil
		}
		// Drop a retained value explicitly. The index and provenance remain
		// available for inspection, while completeness signals the omission.
		dropped := false
		for i := len(ports) - 1; i >= 0; i-- {
			item := ports[i].([]any)
			if item[1] == true {
				item[1], item[2] = false, nil
				complete, dropped = false, true
				break
			}
		}
		if !dropped {
			return history.SafeJSON{}, false, errors.New("safe result metadata exceeds indexed projection limit")
		}
	}
}
