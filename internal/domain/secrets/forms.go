package secrets

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// Redaction-set bounds (F3.8 decision 8). They keep the per-byte scan cost
// predictable; exceeding them is a preflight error, never a silent skip.
const (
	MaxSecrets     = 64
	MaxSecretBytes = 16 * 1024
	MaxForms       = 16

	// MinFormLength is the shortest value that gets encoded forms; shorter
	// values would corrupt ordinary output, so they are redacted raw only.
	MinFormLength = 8
)

// Forms returns the byte strings a redactor must replace for one secret value:
// the raw bytes and, from MinFormLength bytes up, lower and upper hex, the query
// and path percent-encodings (when they differ from raw), and standard and
// URL-safe Base64 for each of the three alignments the secret can take inside a
// larger blob. Hashes, truncation, MIME wrapping and other encodings are not
// covered. The forms are unique; ordering is the caller's concern.
func Forms(value string) [][]byte {
	if value == "" {
		return nil
	}
	forms := newFormSet()
	forms.add(value)
	if len(value) < MinFormLength {
		return forms.list
	}
	raw := []byte(value)
	lower := hex.EncodeToString(raw)
	forms.add(lower)
	forms.add(strings.ToUpper(lower))
	forms.add(percentEncode(value, true))
	forms.add(percentEncode(value, false))
	for k := 0; k < 3; k++ {
		forms.add(stableBase64(base64.RawStdEncoding, raw, k))
		forms.add(stableBase64(base64.RawURLEncoding, raw, k))
	}
	return forms.list
}

// percentEncode matches net/url's QueryEscape (query true: space becomes '+')
// and PathEscape (query false: "$&+=:@" stay literal). It is written out here
// because a domain package that net/url would pull into the dependency graph
// breaks the capability package's no-network-dependency guarantee.
func percentEncode(s string, query bool) string {
	const upperHex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case unreserved(c):
			b.WriteByte(c)
		case query && c == ' ':
			b.WriteByte('+')
		case !query && strings.IndexByte("$&+=:@", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&15])
		}
	}
	return b.String()
}

func unreserved(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~", c) >= 0
}

// stableBase64 returns the characters of the unpadded encoding that depend only
// on the secret's bytes when k unknown bytes precede it: character i covers
// bits [6i, 6i+6) and the secret occupies bits [8k, 8(k+n)).
func stableBase64(enc *base64.Encoding, secret []byte, k int) string {
	padded := make([]byte, k, k+len(secret))
	padded = append(padded, secret...)
	encoded := enc.EncodeToString(padded)
	start, end := (8*k+5)/6, 8*(k+len(secret))/6
	if start >= end {
		return ""
	}
	return encoded[start:end]
}

type formSet struct {
	seen map[string]bool
	list [][]byte
}

func newFormSet() *formSet { return &formSet{seen: map[string]bool{}} }

func (f *formSet) add(s string) {
	if s == "" || f.seen[s] {
		return
	}
	f.seen[s] = true
	f.list = append(f.list, []byte(s))
}
