package claudecode

import (
	"encoding/json"
	"errors"
	"slices"
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
	fields, err := strictObject(raw, "final structured output is missing or not unambiguous JSON")
	if err != nil {
		return out, err
	}
	if !onlyKeys(fields, "summary", "artifacts") || fields["summary"] == nil || fields["artifacts"] == nil {
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

// strictObject decodes one bounded, unambiguous JSON object (no duplicate keys,
// valid UTF-8, not null). A failure carries message as a claude_result error.
func strictObject(raw json.RawMessage, message string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxRecord || cliproc.UniqueJSON(raw) != nil {
		return nil, fail("result", message, nil)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, fail("result", message, nil)
	}
	return object, nil
}

// onlyKeys reports whether every key of object is one of allowed.
func onlyKeys(object map[string]json.RawMessage, allowed ...string) bool {
	for key := range object {
		if !slices.Contains(allowed, key) {
			return false
		}
	}
	return true
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
	record, err := strictObject(raw, "artifact requires ref and optional name")
	if err != nil {
		return agent.ArtifactReference{}, err
	}
	if !onlyKeys(record, "ref", "name") || record["ref"] == nil {
		return agent.ArtifactReference{}, fail("result", "artifact requires ref and optional name", nil)
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
