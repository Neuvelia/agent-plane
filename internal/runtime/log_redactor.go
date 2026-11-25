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
		if r.eager && !final {
			undecidable := false
			for _, secret := range r.secrets {
				if len(data)-i < len(secret) && bytes.HasPrefix(secret, data[i:]) {
					undecidable = true
					break
				}
			}
			if undecidable {
				break
			}
		}
		matched := false
		for _, secret := range r.secrets {
			if bytes.HasPrefix(data[i:], secret) {
				output = append(output, r.marker...)
				i += len(secret)
				matched = true
				break
			}
		}
		if !matched {
			output = append(output, data[i])
			i++
		}
	}
	r.pending[stream] = append([]byte(nil), data[i:]...)
	return output
}
