package secrets

import (
	"errors"
	"fmt"
)

const redactedText = "[secret]"

// errValueNotSerializable is returned by every marshaling entry point of Value.
var errValueNotSerializable = errors.New("a secret value cannot be serialized")

// Value is a resolved secret. It formats as "[secret]" under every fmt verb and
// refuses JSON and text marshaling, so a stray %+v, t.Logf or json.Marshal cannot
// leak it. Reveal is the only way to read the plaintext; only the code that hands
// the value to a child process or to a redaction set should call it.
type Value struct{ text string }

// NewValue wraps plaintext in a Value.
func NewValue(text string) Value { return Value{text: text} }

// Reveal returns the plaintext.
func (v Value) Reveal() string { return v.text }

// String implements fmt.Stringer and never reveals the value.
func (Value) String() string { return redactedText }

// GoString implements fmt.GoStringer and never reveals the value.
func (Value) GoString() string { return redactedText }

// Format implements fmt.Formatter so that no verb prints the plaintext.
func (Value) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedText)) }

// MarshalJSON always fails: a secret must never enter an event, store or API payload.
func (Value) MarshalJSON() ([]byte, error) { return nil, errValueNotSerializable }

// MarshalText always fails for the same reason as MarshalJSON.
func (Value) MarshalText() ([]byte, error) { return nil, errValueNotSerializable }
