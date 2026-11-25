package runtime

import (
	"fmt"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
)

// resolvedSecret pairs a declared secret name with its resolved value for the
// redaction set only. The value is never formatted, stored or put in an error.
type resolvedSecret struct {
	name  string
	value string
}

const redactionBoundCode = "secret_redaction_bound_exceeded"

// newRedactionSet builds the run's redaction template: the --redact-env values
// exactly as before, plus the raw bytes and encoded forms of every resolved
// secret. A set beyond the bounds fails with a structured preflight diagnostic
// that names the secret and the bound, never the value.
func newRedactionSet(logValues []string, resolved []resolvedSecret) (*logRedactor, error) {
	if err := checkRedactionBounds(resolved); err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return newLogRedactor(logValues)
	}
	set := make([][]byte, 0, len(logValues)+len(resolved)*secrets.MaxForms)
	seen := map[string]bool{}
	add := func(b []byte) {
		if len(b) > 0 && !seen[string(b)] {
			seen[string(b)] = true
			set = append(set, b)
		}
	}
	for _, value := range logValues {
		add([]byte(value))
	}
	for _, s := range resolved {
		for _, form := range secrets.Forms(s.value) {
			add(form)
		}
	}
	return newByteRedactor(set)
}

func checkRedactionBounds(resolved []resolvedSecret) error {
	if len(resolved) > secrets.MaxSecrets {
		return redactionBoundError("secrets", fmt.Sprintf("%d secrets were resolved but at most %d can be redacted", len(resolved), secrets.MaxSecrets),
			"assign fewer secrets to this run, or split the graph")
	}
	for _, s := range resolved {
		if len(s.value) > secrets.MaxSecretBytes {
			return redactionBoundError("secrets."+s.name, fmt.Sprintf("secret %q is larger than the %d-byte redaction bound", s.name, secrets.MaxSecretBytes),
				"use a smaller secret; values above the bound cannot be reliably redacted")
		}
	}
	return nil
}

func redactionBoundError(path, message, suggestion string) error {
	return &run.PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{
		Severity:   graph.SeverityError,
		Code:       redactionBoundCode,
		Message:    message,
		Location:   graph.DiagnosticLocation{Path: path},
		Suggestion: suggestion,
	}}}}
}
