package runtime

import (
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"agentplane/internal/domain/graph"
)

// runPreparation contains only values established before a run starts.
type runPreparation struct {
	plan              *graph.BindingPlan
	graphInputs       map[string]graph.Value
	redactionTemplate *logRedactor
	// grants holds the resolved secret environment of each assigned node. The
	// values stay in memory and are read only where a node process is started.
	grants        map[string]secrets.Env
	scopes        map[string][]string
	scopeRedacted map[string]bool
	isolation     isolationPlan
	diffs         *diffRecorder
	limit         int
}

// redactionPreparationError passes a structured preflight diagnostic through
// untouched so callers keep errors.As and its exact text, and labels any other
// failure as a redaction problem without implying it came only from --redact-env.
func redactionPreparationError(err error) error {
	var preflight *run.PreflightError
	if errors.As(err, &preflight) {
		return err
	}
	return fmt.Errorf("invalid redactions: %w", err)
}

func preflightRun(ctx context.Context, definition *graph.Definition, inputs map[string]graph.Value, executor Executor, options Options) (runPreparation, error) {
	if ctx == nil {
		return runPreparation{}, errors.New("run context is nil")
	}
	if executor == nil {
		return runPreparation{}, errors.New("node executor is nil")
	}
	if options.MaxConcurrency < 0 {
		return runPreparation{}, fmt.Errorf("max concurrency must not be negative: %d", options.MaxConcurrency)
	}
	limit := options.MaxConcurrency
	if limit == 0 {
		limit = 1
	}
	redactions := append([]string(nil), options.LogRedactions...)
	redactionTemplate, err := preflightRedactions(executor, options, redactions)
	if err != nil {
		return runPreparation{}, err
	}
	plan, graphInputs, err := compileRunBindings(definition, inputs)
	if err != nil {
		return runPreparation{}, err
	}

	resolution, err := preflightSecrets(ctx, definition, executor, options, redactions)
	if err != nil {
		return runPreparation{}, err
	}
	if resolution.template != nil {
		redactionTemplate = resolution.template
	}

	scopes, scopeRedacted, err := unitScopes(definition, redactionTemplate)
	if err != nil {
		return runPreparation{}, err
	}
	if err := preflightLoops(ctx, definition, executor); err != nil {
		return runPreparation{}, err
	}
	isolation, err := preflightIsolation(ctx, definition, options)
	if err != nil {
		return runPreparation{}, err
	}
	if err := preflightHuman(definition, options.Decisions); err != nil {
		return runPreparation{}, err
	}
	diffs, err := openAttemptDiffs(ctx, definition, options, redactions, redactionTemplate)
	if err != nil {
		return runPreparation{}, err
	}
	return runPreparation{
		diffs:             diffs,
		plan:              plan,
		graphInputs:       graphInputs,
		redactionTemplate: redactionTemplate,
		grants:            resolution.grants,
		scopes:            scopes,
		scopeRedacted:     scopeRedacted,
		isolation:         isolation,
		limit:             limit,
	}, nil
}

// preflightRedactions builds the --redact-env redaction template and applies
// the UTF-8 rules of the features that rewrite redacted text.
func preflightRedactions(executor Executor, options Options, redactions []string) (*logRedactor, error) {
	redactionTemplate, err := newRedactionSet(redactions, nil)
	if err != nil {
		return nil, redactionPreparationError(err)
	}

	// Byte logs serialize as base64, but normalized/evidence string observation
	// must not let JSON manufacture replacement runes after secret redaction.
	if _, observed := executor.(ObservedExecutor); observed {
		for _, value := range redactions {
			if !utf8.ValidString(value) {
				return nil, errors.New("invalid observed redactions: values must be valid UTF-8")
			}
		}
		if !utf8.Valid(redactionTemplate.marker) {
			return nil, errors.New("invalid observed redactions: no UTF-8-safe secret-absent marker is available")
		}
	}

	if err := preflightValueRecording(options.RecordValues, redactions, redactionTemplate); err != nil {
		return nil, err
	}
	return redactionTemplate, nil
}

// compileRunBindings validates the graph bindings and the supplied inputs and
// returns the binding plan with a stable snapshot of the inputs.
func compileRunBindings(definition *graph.Definition, inputs map[string]graph.Value) (*graph.BindingPlan, map[string]graph.Value, error) {
	plan, validation := graph.CompileBindings(definition)
	if !validation.Valid() {
		return nil, nil, &run.PreflightError{Diagnostics: validation}
	}
	inputValidation := plan.ValidateGraphInputs(inputs)
	if !inputValidation.Valid() {
		return nil, nil, &run.PreflightError{Diagnostics: inputValidation}
	}
	// Run inputs are a stable snapshot even if the caller reuses its map.
	graphInputs := make(map[string]graph.Value, len(inputs))
	for name, value := range inputs {
		graphInputs[name] = value
	}
	return plan, graphInputs, nil
}

// initializeRun starts the event stream after preflight has accepted all
// inputs and capabilities. Its setup order matches the former Run body.
func initializeRun(ctx context.Context, definition *graph.Definition, options Options, prepared runPreparation) (*runMachine, map[string][]string, error) {
	machine, err := newRunMachine(ctx, definition, options)
	if err != nil {
		return nil, nil, err
	}
	machine.definition = definition
	machine.decisions = options.Decisions
	// Graph inputs are recorded directly after run_started, before any other fact.
	machine.values = newValueRecorder(options.RecordValues, prepared.redactionTemplate)
	machine.diffs = prepared.diffs
	machine.grants = prepared.grants
	machine.recordGraphInputs(prepared.graphInputs)
	for _, id := range definition.Units {
		if err := machine.units.register(id, machine.result.RunID, machine.nodes[id].NodeRunID, machine.result.GraphVersion, prepared.scopes[id], prepared.scopeRedacted[id]); err != nil {
			return nil, nil, err
		}
	}
	if definition.Units != nil {
		machine.workspaces = inspectWorkspaces(ctx, definition)
		machine.isolation = prepared.isolation
		for id := range prepared.isolation.nodes {
			machine.workspaces[id] = isolatedIdentity(machine.nodes[id].NodeRunID)
		}
		machine.reportScopeConflicts(definition)
	}
	dependencies := make(map[string][]string, len(machine.ids))
	for _, id := range machine.ids {
		dependencies[id], _ = prepared.plan.Dependencies(id) // CompileBindings validated every node.
	}
	return machine, dependencies, nil
}
