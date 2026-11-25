package runtime

import (
	"fmt"
	"slices"
	"strings"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
)

// StoredMaterial is what a run may copy or record besides its events: the graph
// bytes and definition the store snapshots and the supplied graph inputs. The
// composition root owns it; the runtime owns the resolved values. Preflight
// hands holds to Locations instead of handing values out, so a value never
// leaves the runtime (F3.8 requirement 12, ADR-0029).
type StoredMaterial interface {
	// Locations returns the places at which holds is true for some text, in a
	// stable order. A location is "graph", "inputs" or "inputs.<key>"; it never
	// contains text that held, so "inputs" stands for an input name that did.
	Locations(holds func(text string) bool) []string
}

// Preflight codes of the stored-material guard.
const (
	codeSecretValueInGraph = "secret_value_in_graph"
	codeSecretValueInInput = "secret_value_in_input"
)

// guardStoredMaterial refuses a run whose graph, inputs or recorded inputs hold
// the value of a secret that was just resolved. Findings come in secret-name
// order, then location order, and name the secret and the place, never the value.
func guardStoredMaterial(definition *graph.Definition, material StoredMaterial, plan graph.SecretPlan, values map[string]secrets.Value) error {
	if material == nil {
		return nil
	}
	var faults []secretFault
	for _, name := range plan.Assigned() {
		revealed := values[name].Reveal()
		locations := material.Locations(func(text string) bool { return strings.Contains(text, revealed) })
		slices.Sort(locations)
		for _, location := range slices.Compact(locations) {
			faults = append(faults, materialFault(name, location))
		}
	}
	if len(faults) == 0 {
		return nil
	}
	return secretFailure(definition.ID, faults)
}

func materialFault(name, location string) secretFault {
	if location == "graph" {
		return secretFault{
			code: codeSecretValueInGraph, path: location,
			message:    fmt.Sprintf("the value of secret %q appears in the graph definition", name),
			suggestion: "remove the value from the graph and reference the secret through secrets and secret_env instead",
		}
	}
	return secretFault{
		code: codeSecretValueInInput, path: location,
		message:    fmt.Sprintf("the value of secret %q appears in %s", name, inputWhere(location)),
		suggestion: "do not pass a secret as --input; assign it to the node that needs it through secret_env",
	}
}

func inputWhere(location string) string {
	if key, found := strings.CutPrefix(location, "inputs."); found {
		return fmt.Sprintf("graph input %q", key)
	}
	return "the name of a supplied graph input"
}
