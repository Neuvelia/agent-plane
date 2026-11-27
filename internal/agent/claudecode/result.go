package claudecode

import (
	"encoding/json"
	"errors"
	"strings"

	"agentplane/internal/agent"
	"agentplane/internal/agent/cliproc"
)

// parseOutput validates the `structured_output` value of the final result
// record: exactly `summary` (nonempty string) and `artifacts` (array of objects
// with a nonempty `ref` and an optional string `name`). Usage and raw metadata
// stay absent (plan D9).
func parseOutput(raw json.RawMessage) (agent.AgentExecutionOutput, error) {
	var out agent.AgentExecutionOutput
	if len(raw) == 0 || len(raw) > maxRecord || cliproc.UniqueJSON(raw) != nil {
		return out, fail("result", "final structured output is missing or not unambiguous JSON", nil)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["summary"] == nil || fields["artifacts"] == nil {
		return out, fail("result", "final structured output requires exact summary and artifacts fields", nil)
	}
	summary, err := parseString(fields["summary"])
	if err != nil || strings.TrimSpace(summary) == "" {
		return out, fail("result", "final summary must be a nonempty string", nil)
	}
	artifacts, err := parseArtifacts(fields["artifacts"])
	if err != nil {
		return out, err
	}
	return agent.AgentExecutionOutput{Summary: summary, Artifacts: artifacts}, nil
}

func parseArtifacts(raw json.RawMessage) ([]agent.ArtifactReference, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, fail("result", "final artifacts must be an array", nil)
	}
	artifacts := make([]agent.ArtifactReference, 0, len(items))
	for _, item := range items {
		artifact, err := parseArtifact(item)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func parseArtifact(raw json.RawMessage) (agent.ArtifactReference, error) {
	var record map[string]json.RawMessage
	if json.Unmarshal(raw, &record) != nil || len(record) < 1 || len(record) > 2 || record["ref"] == nil {
		return agent.ArtifactReference{}, fail("result", "artifact requires ref and optional name", nil)
	}
	for key := range record {
		if key != "ref" && key != "name" {
			return agent.ArtifactReference{}, fail("result", "unknown artifact field", nil)
		}
	}
	ref, err := parseString(record["ref"])
	if err != nil || strings.TrimSpace(ref) == "" {
		return agent.ArtifactReference{}, fail("result", "artifact reference must be nonempty", nil)
	}
	name := ""
	if rawName, ok := record["name"]; ok {
		if name, err = parseString(rawName); err != nil {
			return agent.ArtifactReference{}, fail("result", "artifact name must be a string", nil)
		}
	}
	return agent.ArtifactReference{Ref: ref, Name: name}, nil
}

// parseString accepts only a JSON string; null and other types are rejected.
func parseString(raw json.RawMessage) (string, error) {
	var text string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &text) != nil {
		return "", errors.New("expected string")
	}
	return text, nil
}
