package main

import (
	"agentplane/internal/domain/run"
	"encoding/json"
	"fmt"
	"io"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/store"
)

type cliError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type cliNode struct {
	AcceptedAttemptID events.AttemptID `json:"accepted_attempt_id,omitempty"`
	NodeID            string           `json:"node_id"`
	NodeRunID         events.NodeRunID `json:"node_run_id"`
	Status            run.NodeState    `json:"status"`
	FailureKind       run.FailureKind  `json:"failure_kind,omitempty"`
}

type cliOutput struct {
	Name  string          `json:"name"`
	Type  graph.ValueType `json:"type"`
	Value any             `json:"value"`
}

type cliFailure struct {
	NodeID      string             `json:"node_id,omitempty"`
	Kind        run.FailureKind    `json:"kind"`
	Message     string             `json:"message,omitempty"`
	Diagnostics []graph.Diagnostic `json:"diagnostics"`
}

// cliIsolation is the optional F3.1 result object. It is absent, not empty,
// for a run in which no node used isolation, so existing documents are unchanged.
type cliIsolation struct {
	BaseSHA          string            `json:"base_sha"`
	IntegratedRef    string            `json:"integrated_ref,omitempty"`
	IntegratedCommit string            `json:"integrated_commit,omitempty"`
	Units            []cliIsolatedUnit `json:"units"`
	Retained         []cliRetained     `json:"retained"`
}

type cliIsolatedUnit struct {
	NodeID     string `json:"node_id"`
	Ref        string `json:"ref"`
	Commit     string `json:"commit"`
	Integrated bool   `json:"integrated"`
}

type cliRetained struct {
	NodeID string `json:"node_id"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type cliDocument struct {
	SchemaVersion int                `json:"cli_schema_version"`
	Command       string             `json:"command"`
	Status        string             `json:"status"`
	GraphID       string             `json:"graph_id,omitempty"`
	GraphVersion  string             `json:"graph_version,omitempty"`
	RunID         events.RunID       `json:"run_id,omitempty"`
	Diagnostics   []graph.Diagnostic `json:"diagnostics"`
	Nodes         *[]cliNode         `json:"nodes,omitempty"`
	Outputs       *[]cliOutput       `json:"outputs,omitempty"`
	Events        *[]events.Event    `json:"events,omitempty"`
	Isolation     *cliIsolation      `json:"workspace_isolation,omitempty"`
	Runs          *[]store.Summary   `json:"runs,omitempty"`
	Prune         *cliPrune          `json:"prune,omitempty"`
	Truncated     bool               `json:"truncated,omitempty"`
	Failure       *cliFailure        `json:"failure"`
	Error         *cliError          `json:"error"`
}

func nonNilDiagnostics(diagnostics []graph.Diagnostic) []graph.Diagnostic {
	if diagnostics == nil {
		return []graph.Diagnostic{}
	}
	return diagnostics
}

func writeJSON(w, stderr io.Writer, document cliDocument, code int) int {
	document.SchemaVersion = 1
	if err := json.NewEncoder(w).Encode(document); err != nil {
		fmt.Fprintf(stderr, "write JSON output: %v\n", err)
		return exitInternal
	}
	return code
}

func documentForRun(result run.Result, diagnostics []graph.Diagnostic) cliDocument {
	nodes := make([]cliNode, 0, len(result.Nodes))
	outputs := make([]cliOutput, 0, len(result.Outputs))
	events := result.Events
	doc := cliDocument{Command: "run", Status: string(result.State), GraphID: result.GraphID, GraphVersion: result.GraphVersion, RunID: result.RunID, Diagnostics: nonNilDiagnostics(diagnostics), Nodes: &nodes, Outputs: &outputs, Events: &events}
	for _, node := range result.Nodes {
		entry := cliNode{NodeID: node.NodeID, NodeRunID: node.NodeRunID, AcceptedAttemptID: node.AcceptedAttemptID, Status: node.State}
		if node.Failure != nil {
			entry.FailureKind = node.Failure.Kind
		}
		nodes = append(nodes, entry)
	}
	for _, output := range result.Outputs {
		entry := cliOutput{Name: output.Name, Type: output.Value.Type(), Value: valueForJSON(output.Value)}
		outputs = append(outputs, entry)
	}
	doc.Isolation = isolationForJSON(result.Isolation)
	if result.Failure != nil {
		failure := result.Failure
		doc.Failure = &cliFailure{NodeID: failure.NodeID, Kind: failure.Kind, Diagnostics: nonNilDiagnostics(failure.Diagnostics)}
		if failure.Err != nil {
			doc.Failure.Message = failure.Err.Error()
		}
	}
	return doc
}

func isolationForJSON(iso *run.IsolationResult) *cliIsolation {
	if iso == nil {
		return nil
	}
	out := &cliIsolation{BaseSHA: iso.BaseSHA, IntegratedRef: iso.IntegratedRef, IntegratedCommit: iso.IntegratedCommit, Units: []cliIsolatedUnit{}, Retained: []cliRetained{}}
	for _, u := range iso.Units {
		out.Units = append(out.Units, cliIsolatedUnit{NodeID: u.NodeID, Ref: u.Ref, Commit: u.Commit, Integrated: u.Integrated})
	}
	for _, r := range iso.Retained {
		out.Retained = append(out.Retained, cliRetained{NodeID: r.NodeID, Path: r.Path, Reason: r.Reason})
	}
	return out
}

func valueForJSON(value graph.Value) any {
	switch value.Type() {
	case graph.ValueTypeString:
		v, _ := value.AsString()
		return v
	case graph.ValueTypeNumber:
		v, _ := value.AsNumber()
		return v
	case graph.ValueTypeBoolean:
		v, _ := value.AsBoolean()
		return v
	case graph.ValueTypeJSON:
		v, _ := value.AsJSON()
		return json.RawMessage(v)
	case graph.ValueTypeArtifactRef:
		v, _ := value.AsArtifactRef()
		return v
	default:
		panic(fmt.Sprintf("unexpected graph output value type %q", value.Type()))
	}
}
