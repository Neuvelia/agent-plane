package runtime

import (
	"context"
	"errors"
	"fmt"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
)

// Preflight diagnostic codes of the secret stage. Static graph errors keep the
// validator's E_SECRET_* codes, the redaction bound its own (redaction_set.go).
const (
	codeSecretUnresolved  = "secret_unresolved"
	codeSecretEmpty       = "secret_empty"
	codeSecretUnavailable = "secret_provider_unavailable"
	codeSecretInvalid     = "secret_invalid_value"
	codeSecretDelivery    = "secret_delivery_unsupported"
)

// secretResolution is the product of the secret stage. The zero value means the
// graph assigns no secrets. Resolved values live only here and in the
// redaction template; they never enter Options, a Result, an event or an error.
type secretResolution struct {
	template *logRedactor
	grants   map[string]secrets.Env
}

// preflightSecrets resolves the secrets the graph assigns to nodes, judges the
// values and builds the run's redaction template and per-node grants. A graph
// that assigns nothing returns at once: no resolver call, no environment read.
// It runs after graph validation (which already enforces the ambient-bypass
// rule) and before unitScopes and run_started, so a failure leaves no trace.
func preflightSecrets(ctx context.Context, definition *graph.Definition, executor Executor, options Options, redactions []string) (secretResolution, error) {
	plan := graph.PlanSecrets(definition)
	if plan.Empty() {
		return secretResolution{}, nil
	}
	if faults := checkSecretStatics(definition, plan, executor, options.SecretResolver); len(faults) > 0 {
		return secretResolution{}, secretFailure(definition.ID, faults)
	}
	// F3.9 narrows plan to the grants its policy admits at this point, before
	// anything is read.
	values, faults, err := resolveSecrets(ctx, options.SecretResolver, plan, secretValueCheck(executor, options))
	if err != nil {
		return secretResolution{}, err
	}
	if len(faults) > 0 {
		return secretResolution{}, secretFailure(definition.ID, faults)
	}
	template, err := buildSecretTemplate(redactions, plan, values)
	if err != nil {
		return secretResolution{}, err
	}
	return secretResolution{template: template, grants: buildGrants(plan, values)}, nil
}

// secretFault is one preflight finding about a secret. It never carries a value
// or text from the provider, and names the secret and node, not the variable
// behind the reference.
type secretFault struct {
	code, node, path, message, suggestion string
}

func (f secretFault) diagnostic(graphID string) graph.Diagnostic {
	return graph.Diagnostic{
		Severity: graph.SeverityError, Code: f.code, GraphID: graphID, Message: f.message, Suggestion: f.suggestion,
		Location: graph.DiagnosticLocation{NodeID: f.node, Path: f.path},
	}
}

func secretFailure(graphID string, faults []secretFault) error {
	diagnostics := make([]graph.Diagnostic, len(faults))
	for i, f := range faults {
		diagnostics[i] = f.diagnostic(graphID)
	}
	return &run.PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: diagnostics}}
}

// checkSecretStatics finds what can be decided without reading any secret: an
// executor that cannot deliver, and a runner without a provider.
func checkSecretStatics(definition *graph.Definition, plan graph.SecretPlan, executor Executor, resolver SecretResolver) []secretFault {
	var faults []secretFault
	receiver, _ := executor.(SecretReceiver)
	for _, id := range plan.NodeIDs() {
		if receiver != nil && receiver.ReceivesSecrets(definition.Nodes[id].Type) {
			continue
		}
		for _, grant := range plan.Grants[id] {
			faults = append(faults, secretFault{
				code: codeSecretDelivery, node: id, path: secretEnvPath(id),
				message:    fmt.Sprintf("node %q is assigned secret %q but this runner cannot deliver secrets to a %s node", id, grant.Secret, definition.Nodes[id].Type),
				suggestion: "remove secret_env from this node, or run it with an executor that delivers secrets",
			})
		}
	}
	if resolver == nil {
		for _, name := range plan.Assigned() {
			faults = append(faults, secretResolveFault(plan, name, codeSecretUnavailable, "cannot be resolved: this runner has no secret provider",
				"run with a secret provider configured"))
		}
	}
	return faults
}

// resolveSecrets reads each assigned secret once per reference and judges the
// value with check. It reports every failing secret in name order; only a
// cancelled context is returned as a plain error.
func resolveSecrets(ctx context.Context, resolver SecretResolver, plan graph.SecretPlan, check func(secrets.Value) error) (map[string]secrets.Value, []secretFault, error) {
	reader := refReader{resolver: resolver, cache: map[secrets.Ref]refOutcome{}}
	values := map[string]secrets.Value{}
	var faults []secretFault
	for _, name := range plan.Assigned() {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("resolving secrets: %w", err)
		}
		outcome := reader.read(ctx, plan.Refs[name])
		err := outcome.err
		if err == nil {
			err = check(outcome.value)
		}
		if err != nil {
			faults = append(faults, secretValueFault(plan, name, err))
			continue
		}
		values[name] = outcome.value
	}
	// A provider that failed because the run was cancelled is not a finding.
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("resolving secrets: %w", err)
	}
	return values, faults, nil
}

type refOutcome struct {
	value secrets.Value
	err   error
}

// refReader asks the provider once per reference, so two secrets that share a
// reference share one read and one value.
type refReader struct {
	resolver SecretResolver
	cache    map[secrets.Ref]refOutcome
}

func (r refReader) read(ctx context.Context, ref secrets.Ref) refOutcome {
	if outcome, seen := r.cache[ref]; seen {
		return outcome
	}
	value, err := r.resolver.Resolve(ctx, ref)
	outcome := refOutcome{value: value, err: err}
	r.cache[ref] = outcome
	return outcome
}

// secretValueCheck is the judgement applied to every resolved value: always
// non-empty and NUL-free, and valid UTF-8 where redacted text is rewritten as JSON.
func secretValueCheck(executor Executor, options Options) func(secrets.Value) error {
	return func(value secrets.Value) error {
		if err := secrets.CheckValue(value); err != nil {
			return err
		}
		if secretUTF8Required(executor, options) {
			return secrets.CheckUTF8(value)
		}
		return nil
	}
}

// secretUTF8Required matches the existing rule of every feature that rewrites
// redacted text: observed executors, recorded values and attempt diffs.
func secretUTF8Required(executor Executor, options Options) bool {
	_, observed := executor.(ObservedExecutor)
	return observed || options.RecordValues || options.AttemptDiffs != nil
}

// secretValueFault maps a provider error or a failed value check to a fault.
// Provider error text is never copied: it may contain what was looked up.
func secretValueFault(plan graph.SecretPlan, name string, err error) secretFault {
	switch {
	case errors.Is(err, secrets.ErrNotFound):
		return secretResolveFault(plan, name, codeSecretUnresolved, "was not found by its provider",
			"set the variable behind secret_ref in the runner's environment, or correct secret_ref")
	case errors.Is(err, secrets.ErrEmptyValue):
		return secretValueIssue(plan, name, codeSecretEmpty, "resolved to an empty value",
			"provide a non-empty value; an empty secret cannot be redacted")
	case errors.Is(err, secrets.ErrNULValue):
		return secretValueIssue(plan, name, codeSecretInvalid, "contains a NUL byte",
			"use a value without NUL bytes; an environment variable cannot hold one")
	case errors.Is(err, secrets.ErrNotUTF8):
		return secretValueIssue(plan, name, codeSecretInvalid, "is not valid UTF-8, which this run needs to redact it safely",
			"use a UTF-8 value, or remove observed agents, --record-values and attempt diffs from this run")
	}
	return secretResolveFault(plan, name, codeSecretUnavailable, "could not be read from its provider",
		"check that the provider is available; provider error text is not shown")
}

// secretResolveFault points at the secret declaration, secretValueIssue at the
// first node that assigns it.
func secretResolveFault(plan graph.SecretPlan, name, code, detail, suggestion string) secretFault {
	return secretFault{
		code: code, node: firstAssigningNode(plan, name), path: "secrets." + name + ".secret_ref",
		message: fmt.Sprintf("secret %q %s", name, detail), suggestion: suggestion,
	}
}

func secretValueIssue(plan graph.SecretPlan, name, code, detail, suggestion string) secretFault {
	node := firstAssigningNode(plan, name)
	return secretFault{
		code: code, node: node, path: secretEnvPath(node),
		message: fmt.Sprintf("secret %q %s", name, detail), suggestion: suggestion,
	}
}

func secretEnvPath(node string) string { return "nodes." + node + ".config.secret_env" }

func firstAssigningNode(plan graph.SecretPlan, name string) string {
	for _, id := range plan.NodeIDs() {
		for _, grant := range plan.Grants[id] {
			if grant.Secret == name {
				return id
			}
		}
	}
	return ""
}

// buildSecretTemplate adds the raw bytes and encoded forms of every resolved
// secret to the --redact-env values. Bound errors stay structured diagnostics.
// No UTF-8 check of the marker is needed: a value holding a NUL byte is
// rejected earlier, so a printable ASCII or NUL marker byte is always free.
func buildSecretTemplate(redactions []string, plan graph.SecretPlan, values map[string]secrets.Value) (*logRedactor, error) {
	resolved := make([]resolvedSecret, 0, len(values))
	for _, name := range plan.Assigned() {
		resolved = append(resolved, resolvedSecret{name: name, value: values[name].Reveal()})
	}
	template, err := newRedactionSet(redactions, resolved)
	if err != nil {
		return nil, redactionPreparationError(err)
	}
	return template, nil
}

// buildGrants turns the plan into the secret environment of each assigned node.
func buildGrants(plan graph.SecretPlan, values map[string]secrets.Value) map[string]secrets.Env {
	grants := make(map[string]secrets.Env, len(plan.Grants))
	for id, assigned := range plan.Grants {
		bindings := make([]secrets.Binding, len(assigned))
		for i, grant := range assigned {
			bindings[i] = secrets.Binding{Variable: grant.Variable, Value: values[grant.Secret]}
		}
		grants[id] = secrets.NewEnv(bindings)
	}
	return grants
}
