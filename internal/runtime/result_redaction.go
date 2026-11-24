package runtime

import (
	"errors"
	"unicode/utf8"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

// resultRedactor presents the run Result with the same redaction set as the
// event log (F3.8, ADR-0029). It works on copies only: accepted outputs stay
// raw for downstream nodes. It is immutable after construction, so snapshots
// may run on any goroutine.
type resultRedactor struct{ template *logRedactor }

// newResultRedactor returns nil when the set is empty, so a run without
// redactions keeps byte-identical results. Every method accepts a nil receiver.
func newResultRedactor(template *logRedactor) *resultRedactor {
	if template == nil || len(template.secrets) == 0 {
		return nil
	}
	return &resultRedactor{template: template}
}

// apply replaces the result's values and error text with redacted copies.
// result.Nodes must already be a private slice, as snapshot builds it.
func (r *resultRedactor) apply(result *run.Result) {
	if r == nil {
		return
	}
	result.Outputs = r.ports(result.Outputs)
	result.Failure = r.failure(result.Failure)
	for i := range result.Nodes {
		result.Nodes[i].Outputs = r.ports(result.Nodes[i].Outputs)
		result.Nodes[i].Failure = r.failure(result.Nodes[i].Failure)
	}
}

func (r *resultRedactor) ports(in []run.PortValue) []run.PortValue {
	if r == nil || in == nil {
		return in
	}
	out := make([]run.PortValue, len(in))
	for i, port := range in {
		out[i] = r.port(port)
	}
	return out
}

func (r *resultRedactor) port(in run.PortValue) run.PortValue {
	value, outcome := r.value(in.Value)
	switch outcome {
	case valueMasked:
		in.Value, in.Redacted = value, true
	case valueWithheld:
		in.Withheld, in.Value, in.Redacted = in.Value.Type(), graph.Value{}, true
	}
	return in
}

type valueOutcome int

const (
	valueUnchanged valueOutcome = iota
	valueMasked
	valueWithheld
)

// value masks text-like values and withholds the rest. A number, boolean or
// JSON document is never rewritten: it is withheld when masking would change
// its text, and JSON also when a decoded string spells a secret with escapes,
// the same rule as value_recorded (ADR-0023).
func (r *resultRedactor) value(v graph.Value) (graph.Value, valueOutcome) {
	raw := valueText(v)
	masked := r.text(raw)
	if masked == raw && !r.jsonSpellsSecret(v, raw) {
		return v, valueUnchanged
	}
	if masked == raw || !utf8.ValidString(masked) {
		return graph.Value{}, valueWithheld
	}
	switch v.Type() {
	case graph.ValueTypeString:
		return graph.NewStringValue(masked), valueMasked
	case graph.ValueTypeArtifactRef:
		ref, err := graph.NewArtifactRefValue(masked)
		if err != nil {
			return graph.Value{}, valueWithheld
		}
		return ref, valueMasked
	}
	return graph.Value{}, valueWithheld
}

func (r *resultRedactor) jsonSpellsSecret(v graph.Value, raw string) bool {
	return v.Type() == graph.ValueTypeJSON && jsonStringsContainSecret([]byte(raw), r.template.secrets)
}

// text redacts the whole string in one pass, so a secret is never split by a
// boundary, and returns the input itself when nothing matched.
func (r *resultRedactor) text(s string) string {
	if r == nil {
		return s
	}
	c := r.template.clone()
	masked := append([]byte{}, c.Write(events.Stdout, []byte(s))...)
	return string(append(masked, c.Flush(events.Stdout)...))
}

// failure returns a copy whose error text is redacted, or in itself when it
// has no error to present. Diagnostics describe
// graph structure and carry no values, so they are shared unchanged.
func (r *resultRedactor) failure(in *run.Failure) *run.Failure {
	if r == nil || in == nil {
		return in
	}
	if in.Err == nil {
		return in
	}
	out := *in
	out.Err = r.presentedError(in.Err)
	return &out
}

// redactableError is implemented by typed errors that carry raw process or
// provider text. Redacted returns a copy whose text fields went through mask.
type redactableError interface {
	Redacted(mask func(string) string) error
}

// presentedError returns cause itself when it holds no secret. A typed error
// that carries raw text is always replaced: its fields, not only Error(), may
// spell a secret.
func (r *resultRedactor) presentedError(cause error) error {
	message := cause.Error()
	masked := r.text(message)
	var typed redactableError
	hasTyped := errors.As(cause, &typed)
	if masked == message && !hasTyped {
		return cause
	}
	presented := &redactedError{message: masked, cause: cause}
	if hasTyped {
		presented.typed = typed.Redacted(r.text)
	}
	return presented
}

// redactedError carries masked text. errors.Is answers from the original
// cause, which is a boolean and exposes nothing. errors.As reaches only the
// masked copy of the typed error (never the raw cause), and there is no
// Unwrap, so the raw chain cannot be walked (ADR-0029).
type redactedError struct {
	message string
	cause   error
	typed   error
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Is(target error) bool { return errors.Is(e.cause, target) }

func (e *redactedError) As(target any) bool {
	return e.typed != nil && errors.As(e.typed, target)
}
