package secrets

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Judgements about a resolved value. A provider reports facts; the runtime
// preflight applies these (F3.8, ADR-0029). None of the errors carries the value.
var (
	// ErrEmptyValue means the secret resolved to the empty string, which a
	// redactor could not replace.
	ErrEmptyValue = errors.New("secret value is empty")
	// ErrNULValue means the secret contains a NUL byte, which no environment
	// variable can hold.
	ErrNULValue = errors.New("secret value contains a NUL byte")
	// ErrNotUTF8 means the secret is not valid UTF-8, which observed output and
	// recorded values cannot redact safely.
	ErrNotUTF8 = errors.New("secret value is not valid UTF-8")
)

// CheckValue rejects a value that cannot be delivered or redacted at all.
func CheckValue(v Value) error {
	text := v.Reveal()
	if text == "" {
		return ErrEmptyValue
	}
	if strings.ContainsRune(text, 0) {
		return ErrNULValue
	}
	return nil
}

// CheckUTF8 rejects a value that is not valid UTF-8. It is a separate check
// because only features that rewrite redacted text as JSON need it.
func CheckUTF8(v Value) error {
	if !utf8.ValidString(v.Reveal()) {
		return ErrNotUTF8
	}
	return nil
}
