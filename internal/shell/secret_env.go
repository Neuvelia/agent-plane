package shell

import (
	"fmt"
	"strings"

	"agentplane/internal/domain/secrets"
)

// parseSecretEnv reads secret_env: environment variable name -> NAME of a
// declared secret. Only names are held here, never values. The rules mirror
// the graph validator (E_SECRET_ENV_*); whether a name is declared in the
// graph's secrets section is checked by the validator, which sees the graph.
// Messages never quote a name that failed the grammar: a pasted literal such as
// "GH_TOKEN=ghp_xxx" must not reach diagnostics, so such entries are named by
// their index in the sorted keys, as the graph validator does.
func parseSecretEnv(value any, cfg config) (map[string]string, error) {
	entries, err := secretEnvEntries(value)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(entries))
	for index, variable := range sortedMapKeys(entries) {
		if err := checkSecretEnvEntry(index, variable, entries[variable], cfg); err != nil {
			return nil, err
		}
		folded := strings.ToUpper(variable)
		if seen[folded] {
			return nil, fmt.Errorf("%s repeats an earlier environment name; names are compared case-insensitively", entryText("secret_env", index, variable))
		}
		seen[folded] = true
	}
	return entries, nil
}

// secretEnvEntries is stringMap for secret_env: it never quotes a variable name
// that is outside the grammar (graph validator: entry N has an invalid variable
// name), and leaves the shared stringMap unchanged for other fields.
func secretEnvEntries(value any) (map[string]string, error) {
	raw, keys, err := sortedEntries(value, "secret_env")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(keys))
	for index, key := range keys {
		if !secrets.ValidEnvName(key) {
			return nil, fmt.Errorf("secret_env entry %d has an invalid environment name; use a non-empty name without '=' that is not AGENTPLANE_ATTEMPT", index)
		}
		item, ok := raw[key].(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", entryText("secret_env", index, key))
		}
		if strings.ContainsRune(item, 0) {
			return nil, fmt.Errorf("%s contains a NUL byte", entryText("secret_env", index, key))
		}
		result[key] = item
	}
	return result, nil
}

func checkSecretEnvEntry(index int, variable, secretName string, cfg config) error {
	if !secrets.ValidName(secretName) {
		return fmt.Errorf("%s must name a declared secret: a lowercase name of 1-64 characters", entryText("secret_env", index, variable))
	}
	if conflict := secretEnvConflict(variable, cfg); conflict != "" {
		return fmt.Errorf("%s conflicts with %s", entryText("secret_env", index, variable), conflict)
	}
	return nil
}

// secretEnvConflict names the channel that already sets variable, ignoring
// letter case, or returns "".
func secretEnvConflict(variable string, cfg config) string {
	for explicit := range cfg.env {
		if strings.EqualFold(explicit, variable) {
			return "env"
		}
	}
	for _, allowed := range cfg.envAllowlist {
		if strings.EqualFold(allowed, variable) {
			return "env_allowlist"
		}
	}
	for _, target := range cfg.inputEnv {
		if strings.EqualFold(target, variable) {
			return "input_env"
		}
	}
	return ""
}
