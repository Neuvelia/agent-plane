package runtime

import (
	"bytes"
	"errors"
	"sort"

	"agentplane/internal/domain/events"
)

// logRedactor keeps the undecidable tail of each byte stream until a later
// write or final flush can rule out a secret spanning the boundary.
type logRedactor struct {
	secrets [][]byte
	maxLen  int
	eager   bool
	marker  []byte
	pending map[events.Stream][]byte
}

func newLogRedactor(values []string) (*logRedactor, error) {
	secrets := make([][]byte, 0, len(values))
	for _, value := range values {
		if value != "" {
			secrets = append(secrets, []byte(value))
		}
	}
	return newByteRedactor(secrets)
}

// newByteRedactor builds a redactor for non-empty byte strings. The marker is
// chosen from bytes absent from every one of them.
func newByteRedactor(values [][]byte) (*logRedactor, error) {
	r := &logRedactor{pending: make(map[events.Stream][]byte, 2)}
	var used [256]bool
	for _, secret := range values {
		r.secrets = append(r.secrets, secret)
		for _, b := range secret {
			used[b] = true
		}
		if len(secret) > r.maxLen {
			r.maxLen = len(secret)
		}
	}
	sort.Slice(r.secrets, func(i, j int) bool {
		if len(r.secrets[i]) != len(r.secrets[j]) {
			return len(r.secrets[i]) > len(r.secrets[j])
		}
		return bytes.Compare(r.secrets[i], r.secrets[j]) < 0
	})
	if len(r.secrets) > 0 {
		marker, ok := safeMarker(&used)
		if !ok {
			return nil, errors.New("log redactions cover every byte; no safe replacement marker is available")
		}
		r.marker = marker
	}
	return r, nil
}

// safeMarker picks a byte absent from every secret, so a replacement can
// neither contain a secret nor join surrounding bytes into one. It prefers
// punctuation, then any printable byte, then any byte.
func safeMarker(used *[256]bool) ([]byte, bool) {
	for _, b := range []byte("*#~^|!?@%+=:;") {
		if !used[b] {
			return []byte{b, b, b}, true
		}
	}
	for b := byte('!'); b <= '~'; b++ {
		if !used[b] {
			return []byte{b, b, b}, true
		}
	}
	for i := 0; i < len(used); i++ {
		if !used[i] {
			return []byte{byte(i), byte(i), byte(i)}, true
		}
	}
	return nil, false
}

func (r *logRedactor) clone() *logRedactor {
	return &logRedactor{secrets: r.secrets, maxLen: r.maxLen, marker: r.marker, pending: make(map[events.Stream][]byte, 2)}
}

// Write returns only bytes that can no longer be part of a future secret.
// The caller serializes concurrent stream callbacks.
func (r *logRedactor) Write(stream events.Stream, data []byte) []byte {
	if len(r.secrets) == 0 {
		return data
	}
	r.pending[stream] = append(r.pending[stream], data...)
	return r.resolve(stream, false)
}

func (r *logRedactor) Flush(stream events.Stream) []byte {
	if len(r.secrets) == 0 {
		return nil
	}
	return r.resolve(stream, true)
}

func (r *logRedactor) resolve(stream events.Stream, final bool) []byte {
	data := r.pending[stream]
	var output []byte
	i := 0
	for i < len(data) && (final || r.eager || len(data)-i >= r.maxLen) {
		if r.eager && !final && r.startsPartialSecret(data[i:]) {
			break
		}
		if n := r.secretAt(data[i:]); n > 0 {
			output = append(output, r.marker...)
			i += n
			continue
		}
		output = append(output, data[i])
		i++
	}
	r.pending[stream] = append([]byte(nil), data[i:]...)
	return output
}

// FlushCut is Flush for a stream whose capture was cut (ADR-0039): the bytes
// after the cut were never seen, so a tail that could begin a secret is
// replaced by the marker instead of being released as harmless text.
func (r *logRedactor) FlushCut(stream events.Stream) []byte {
	if len(r.secrets) == 0 {
		return nil
	}
	data := r.pending[stream]
	r.pending[stream] = nil
	var output []byte
	for i := 0; i < len(data); {
		// A longer secret may continue past the cut even where a shorter one
		// matches here, so the partial check comes first.
		if r.startsPartialSecret(data[i:]) {
			return append(output, r.marker...)
		}
		if n := r.secretAt(data[i:]); n > 0 {
			output = append(output, r.marker...)
			i += n
			continue
		}
		output = append(output, data[i])
		i++
	}
	return output
}

// secretAt returns the length of the longest secret at the start of data, or 0.
// Secrets are sorted longest first.
func (r *logRedactor) secretAt(data []byte) int {
	for _, secret := range r.secrets {
		if bytes.HasPrefix(data, secret) {
			return len(secret)
		}
	}
	return 0
}

// startsPartialSecret reports whether data is a proper prefix of a secret, so
// only bytes after it decide whether a secret is present.
func (r *logRedactor) startsPartialSecret(data []byte) bool {
	for _, secret := range r.secrets {
		if len(data) < len(secret) && bytes.HasPrefix(secret, data) {
			return true
		}
	}
	return false
}
