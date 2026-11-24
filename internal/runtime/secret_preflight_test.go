package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
)

func TestPreflightSecretUnresolved(t *testing.T) {
	exec := newReceiver()
	diagnostics := requireSecretPreflightFailure(t, ghDefinition(), exec, &fakeResolver{}, "secret_unresolved")
	d := diagnostics[0]
	if !strings.Contains(d.Message, `"gh_token"`) || d.Location.NodeID != "a" || d.Suggestion == "" {
		t.Errorf("diagnostic must name the secret and the node and suggest a fix: %+v", d)
	}
	if strings.Contains(d.Message+d.Suggestion+d.Location.Path, "GITHUB_TOKEN") {
		t.Errorf("diagnostic must not cite the variable behind the reference: %+v", d)
	}
}

func TestPreflightSecretEmpty(t *testing.T) {
	resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": ""}}
	requireSecretPreflightFailure(t, ghDefinition(), newReceiver(), resolver, "secret_empty")
}

func TestPreflightSecretProviderUnavailable(t *testing.T) {
	for name, cause := range map[string]error{
		"sentinel": fmt.Errorf("fake: %w", secrets.ErrProviderUnavailable),
		"other":    errors.New("lookup exploded near " + secretCanary),
	} {
		t.Run(name, func(t *testing.T) {
			resolver := &fakeResolver{errs: map[string]error{"env/GITHUB_TOKEN": cause}}
			requireSecretPreflightFailure(t, ghDefinition(), newReceiver(), resolver, "secret_provider_unavailable")
		})
	}
}

func TestPreflightSecretWithoutResolverIsProviderUnavailable(t *testing.T) {
	requireSecretPreflightFailure(t, ghDefinition(), newReceiver(), nil, "secret_provider_unavailable")
}

func TestPreflightSecretInvalidValueNUL(t *testing.T) {
	resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": "ab\x00" + secretCanary}}
	requireSecretPreflightFailure(t, ghDefinition(), newReceiver(), resolver, "secret_invalid_value")
}

func TestPreflightSecretNotUTF8WithObservedExecutor(t *testing.T) {
	resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": secretCanary + "\xff"}}
	requireSecretPreflightFailure(t, ghDefinition(), newObservedReceiver(), resolver, "secret_invalid_value")
}

func TestPreflightSecretNotUTF8WithRecordValues(t *testing.T) {
	resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": secretCanary + "\xff"}}
	sink := &persisted{}
	options := sink.options(resolver)
	options.RecordValues = true
	_, err := Run(context.Background(), ghDefinition(), nil, newReceiver(), options)
	if got := diagnosticCodes(preflightDiagnostics(t, err)); !reflect.DeepEqual(got, []string{"secret_invalid_value"}) {
		t.Errorf("codes = %v", got)
	}
}

func TestPreflightSecretNotUTF8IsAcceptedWithoutObservationOrRecording(t *testing.T) {
	resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": secretCanary + "\xff"}}
	sink := &persisted{}
	if _, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), sink.options(resolver)); err != nil {
		t.Errorf("raw bytes are redactable without observation: %v", err)
	}
}

func TestPreflightSecretAmbientBypass(t *testing.T) {
	def := ghDefinition()
	def.Nodes["sibling"] = graph.NodeDefinition{Type: "shell", Config: map[string]any{"command": "/bin/true", "env_allowlist": []any{"GITHUB_TOKEN"}}}
	resolver := ghResolver()
	requireSecretPreflightFailure(t, def, newReceiver(), resolver, "E_SECRET_AMBIENT_BYPASS")
	if resolver.callCount() != 0 {
		t.Error("a statically invalid graph must not read any secret")
	}
}

func TestPreflightSecretDeliveryUnsupported(t *testing.T) {
	cases := map[string]Executor{
		"executor without the capability": noopExecutor(),
		"capability false for shell":      &receiverExecutor{nodeType: "agent"},
	}
	for name, exec := range cases {
		t.Run(name, func(t *testing.T) {
			resolver := ghResolver()
			diagnostics := requireSecretPreflightFailure(t, ghDefinition(), exec, resolver, "secret_delivery_unsupported")
			if diagnostics[0].Location.NodeID != "a" || !strings.Contains(diagnostics[0].Message, `"gh_token"`) {
				t.Errorf("diagnostic must name the node and the secret: %+v", diagnostics[0])
			}
			if resolver.callCount() != 0 {
				t.Error("static failures happen before any secret is read")
			}
		})
	}
}

func TestPreflightUnassignedSecretIsNotResolved(t *testing.T) {
	def := ghDefinition()
	def.Secrets["spare"] = graph.SecretDefinition{SecretRef: "env/SPARE"}
	resolver := ghResolver()
	sink := &persisted{}
	if _, err := preflightRun(context.Background(), def, nil, newReceiver(), sink.options(resolver)); err != nil {
		t.Fatal(err)
	}
	if want := []secrets.Ref{{Provider: "env", Name: "GITHUB_TOKEN"}}; !reflect.DeepEqual(resolver.calls, want) {
		t.Errorf("resolved %v, want only the assigned reference %v", resolver.calls, want)
	}
}

func TestPreflightResolvesEachRefOnce(t *testing.T) {
	def := secretDefinition(
		map[string]string{"one": "env/SHARED", "two": "env/SHARED"},
		map[string]map[string]string{"a": {"ONE": "one"}, "b": {"TWO": "two", "ONE": "one"}},
	)
	resolver := &fakeResolver{values: map[string]string{"env/SHARED": secretCanary}}
	sink := &persisted{}
	if _, err := preflightRun(context.Background(), def, nil, newReceiver(), sink.options(resolver)); err != nil {
		t.Fatal(err)
	}
	if resolver.callCount() != 1 {
		t.Errorf("resolver called %d times, want once per reference", resolver.callCount())
	}
}

func TestPreflightReportsEveryFailingSecretInOrder(t *testing.T) {
	def := secretDefinition(
		map[string]string{"d_down": "env/REF_D_DOWN_CANARY", "c_ok": "env/REF_C_OK_CANARY", "b_empty": "env/REF_B_EMPTY_CANARY", "a_missing": "env/REF_A_MISSING_CANARY"},
		map[string]map[string]string{"n": {"A": "a_missing", "B": "b_empty", "C": "c_ok", "D": "d_down"}},
	)
	resolver := &fakeResolver{
		values: map[string]string{"env/REF_B_EMPTY_CANARY": "", "env/REF_C_OK_CANARY": "fine"},
		errs:   map[string]error{"env/REF_D_DOWN_CANARY": fmt.Errorf("fake: %w", secrets.ErrProviderUnavailable)},
	}
	diagnostics := requireSecretPreflightFailure(t, def, newReceiver(), resolver, "secret_unresolved", "secret_empty", "secret_provider_unavailable")
	for i, name := range []string{"a_missing", "b_empty", "d_down"} {
		if !strings.Contains(diagnostics[i].Message, `"`+name+`"`) {
			t.Errorf("diagnostic %d = %q, want secret %s", i, diagnostics[i].Message, name)
		}
	}
}

func TestPreflightStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := ghResolver()
	sink := &persisted{}
	_, err := Run(ctx, ghDefinition(), nil, newReceiver(), sink.options(resolver))
	var preflight *run.PreflightError
	if !errors.Is(err, context.Canceled) || errors.As(err, &preflight) {
		t.Errorf("err = %v, want a plain context.Canceled error, not a diagnostic", err)
	}
	if resolver.callCount() != 0 || sink.n.Load() != 0 {
		t.Errorf("a cancelled run resolved %d secrets and emitted %d events", resolver.callCount(), sink.n.Load())
	}
}

func TestPreflightNoSecretsDoesNotTouchResolverOrTemplate(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"n": {Type: "shell"}})
	resolver := ghResolver()
	sink := &persisted{}
	options := sink.options(resolver)
	options.LogRedactions = []string{"env-redacted"}
	prepared, err := preflightRun(context.Background(), def, nil, noopExecutor(), options)
	if err != nil {
		t.Fatalf("a graph without secrets must not need a receiver or a resolver: %v", err)
	}
	if resolver.callCount() != 0 || prepared.grants != nil {
		t.Errorf("calls = %d, grants = %v; want none", resolver.callCount(), prepared.grants)
	}
	want := mustLogRedactor(t, []string{"env-redacted"})
	if got := redactAll(prepared.redactionTemplate, []byte("x env-redacted y"), 0); string(got) != string(redactAll(want, []byte("x env-redacted y"), 0)) {
		t.Errorf("template changed for a graph without secrets: %q", got)
	}
}

// idleDiffer is a hand-written AttemptDiffer whose sessions capture nothing; it
// only switches attempt-diff capture on for preflight.
type idleDiffer struct{}

func (idleDiffer) Open(context.Context) (DiffSession, error) { return idleDiffSession{}, nil }

type idleDiffSession struct{}

func (idleDiffSession) Snapshot(context.Context, string) (DiffSnapshot, error) { return nil, nil }

func (idleDiffSession) Diff(context.Context, DiffSnapshot, DiffSnapshot, DiffRedactor) (AttemptCapture, error) {
	return AttemptCapture{}, nil
}

func (idleDiffSession) Close() error { return nil }

func TestPreflightSecretNotUTF8WithAttemptDiffs(t *testing.T) {
	resolver := &fakeResolver{values: map[string]string{"env/GITHUB_TOKEN": secretCanary + "\xff"}}
	sink := &persisted{}
	options := sink.options(resolver)
	options.AttemptDiffs = idleDiffer{}
	_, err := preflightRun(context.Background(), ghDefinition(), nil, newReceiver(), options)
	if got := diagnosticCodes(preflightDiagnostics(t, err)); !reflect.DeepEqual(got, []string{"secret_invalid_value"}) {
		t.Errorf("codes = %v, want secret_invalid_value while attempt diffs rewrite redacted text", got)
	}
}
