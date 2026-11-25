package shell

import (
	"errors"
	"fmt"
	"strings"

	"agentplane/internal/domain/secrets"
)

// errSecretDeliveryUnavailable is returned when a node configures secret_env
// but no secret values were provided to the executor (F3.8, ADR-0029). The
// process is never started, so nothing is silently skipped and nothing leaks.
// It names neither the variable nor the secret.
var errSecretDeliveryUnavailable = errors.New("secret_env is configured but this build does not deliver secret values yet; the command was not started")

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
	for _, variable := range sortedMapKeys(entries) {
		if err := checkSecretEnvEntry(variable, entries[variable], cfg); err != nil {
			return nil, err
		}
		folded := strings.ToUpper(variable)
		if seen[folded] {
			return nil, fmt.Errorf("secret_env has duplicate environment name %q", variable)
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
			return nil, fmt.Errorf("secret_env[%q] must be a string", key)
		}
		if strings.ContainsRune(item, 0) {
			return nil, fmt.Errorf("secret_env[%q] contains a NUL byte", key)
		}
		result[key] = item
	}
	return result, nil
}

func checkSecretEnvEntry(variable, secretName string, cfg config) error {
	if !secrets.ValidName(secretName) {
		return fmt.Errorf("secret_env[%q] must name a declared secret: a lowercase name of 1-64 characters", variable)
	}
	if conflict := secretEnvConflict(variable, cfg); conflict != "" {
		return fmt.Errorf("secret_env environment name %q conflicts with %s", variable, conflict)
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

// requireSecretDelivery fails closed: until secret values are delivered, a
// node that configures secret_env must not start.
func requireSecretDelivery(cfg config) error {
	if len(cfg.secretEnv) > 0 {
		return errSecretDeliveryUnavailable
	}
	return nil
}
