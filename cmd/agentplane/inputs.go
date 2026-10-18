package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"agentplane/internal/graph"
)

type inputFlags []string

func (f *inputFlags) String() string         { return strings.Join(*f, ",") }
func (f *inputFlags) Set(value string) error { *f = append(*f, value); return nil }

func validateInputSyntax(flags inputFlags) error {
	seen := make(map[string]bool, len(flags))
	for _, item := range flags {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			return fmt.Errorf("--input must have the form key=value")
		}
		if seen[key] {
			return fmt.Errorf("duplicate --input key %q", key)
		}
		seen[key] = true
	}
	return nil
}

func redactionsFromEnv(names inputFlags) ([]string, error) {
	seen := make(map[string]bool, len(names))
	var values []string
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, "=\x00") {
			return nil, fmt.Errorf("invalid --redact-env name %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate --redact-env name %q", name)
		}
		seen[name] = true
		value, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf("--redact-env variable %q is not set", name)
		}
		if value != "" {
			values = append(values, value)
		}
	}
	return values, nil
}

func parseInputs(def *graph.Definition, flags inputFlags) (map[string]graph.Value, graph.ValidationResult, error) {
	if err := validateInputSyntax(flags); err != nil {
		return nil, graph.ValidationResult{}, err
	}
	values := make(map[string]graph.Value, len(flags))
	seen := make(map[string]bool, len(flags))
	var diagnostics []graph.Diagnostic
	for _, item := range flags {
		key, raw, _ := strings.Cut(item, "=") // Syntax was checked above.
		seen[key] = true
		declared, exists := def.Inputs[key]
		if !exists {
			continue
		}
		var value graph.Value
		var err error
		switch declared.Type {
		case graph.ValueTypeString:
			value = graph.NewStringValue(raw)
		case graph.ValueTypeNumber:
			value, err = graph.NewNumberValue(json.Number(raw))
		case graph.ValueTypeBoolean:
			var parsed bool
			if raw != "true" && raw != "false" {
				err = fmt.Errorf("expected true or false")
			} else {
				parsed, _ = strconv.ParseBool(raw)
				value = graph.NewBooleanValue(parsed)
			}
		case graph.ValueTypeJSON:
			value, err = graph.NewJSONValue(json.RawMessage(raw))
		case graph.ValueTypeArtifactRef:
			value, err = graph.NewArtifactRefValue(raw)
		}
		if err != nil {
			diagnostics = append(diagnostics, graph.Diagnostic{Severity: graph.SeverityError, Code: "E_CLI_INPUT_VALUE_INVALID", Message: fmt.Sprintf("graph input %q requires a %s value: %v", key, declared.Type, err), GraphID: def.ID, Location: graph.DiagnosticLocation{Path: "inputs." + key}, Suggestion: "supply a value matching the declared graph input type"})
			continue
		}
		values[key] = value
	}
	for key := range seen {
		if _, exists := def.Inputs[key]; !exists {
			values[key] = graph.NewStringValue("")
		}
	}
	return values, graph.ValidationResult{Diagnostics: diagnostics}, nil
}
