package desktop

import (
	"errors"
	"unicode/utf8"

	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

const maxLaunchRedactionValues = 128
const maxLaunchRedactionBytes = 1 << 20

var errLaunchRedactions = errors.New("launch redaction limit exceeded")

// launchRedactions covers explicit environment values and supplied string
// graph inputs, including values that a Shell input_env binding may forward.
// Derived node outputs cannot be known until execution; they are not added.
func launchRedactions(environment LaunchEnvironment, inputs map[string]graph.Value) ([]string, error) {
	values := environment.Redactions()
	seen := make(map[string]bool, len(values)+len(inputs))
	redactions := make([]string, 0, len(values)+len(inputs))
	bytes := 0
	add := func(value string) error {
		if value == "" || seen[value] {
			return nil
		}
		if !utf8.ValidString(value) || len(redactions) >= maxLaunchRedactionValues || bytes+len(value) > maxLaunchRedactionBytes {
			return errLaunchRedactions
		}
		seen[value] = true
		redactions = append(redactions, value)
		bytes += len(value)
		return nil
	}
	for _, value := range values {
		if err := add(value); err != nil {
			return nil, err
		}
	}
	for _, value := range inputs {
		if text, ok := value.AsString(); ok {
			if err := add(text); err != nil {
				return nil, err
			}
		}
	}
	if err := runtime.ValidateLogRedactions(redactions, true); err != nil {
		return nil, errLaunchRedactions
	}
	return redactions, nil
}
