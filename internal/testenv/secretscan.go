package testenv

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SecretForms lists the byte strings under which value could appear in an
// artifact: raw, JSON-escaped, hex, percent-encoded and Base64 at all three
// alignments (standard and URL alphabets). It is deliberately independent of
// the production form set (internal/domain/secrets), so a defect there cannot
// hide a leak from the scan that checks it.
func SecretForms(value string) []string {
	forms := map[string]bool{value: true}
	escaped, _ := json.Marshal(value)
	forms[strings.Trim(string(escaped), `"`)] = true
	raw := []byte(value)
	forms[hex.EncodeToString(raw)] = true
	forms[strings.ToUpper(hex.EncodeToString(raw))] = true
	for _, text := range percentForms(raw) {
		forms[text] = true
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		for k := 0; k < 3; k++ {
			if text := alignedBase64(encoding, raw, k); text != "" {
				forms[text] = true
			}
		}
	}
	result := make([]string, 0, len(forms))
	for text := range forms {
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func percentForms(raw []byte) []string {
	var keep, all strings.Builder
	for _, b := range raw {
		fmt.Fprintf(&all, "%%%02X", b)
		if b < 0x80 && (b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("-._~", b) >= 0) {
			keep.WriteByte(b)
			continue
		}
		fmt.Fprintf(&keep, "%%%02X", b)
	}
	return []string{keep.String(), strings.ToLower(keep.String()), all.String(), strings.ToLower(all.String())}
}

// alignedBase64 encodes value after k filler bytes and keeps only the
// characters that depend on value alone, which is how value appears when it
// starts k bytes into a larger Base64 stream.
func alignedBase64(encoding *base64.Encoding, value []byte, k int) string {
	encoded := encoding.EncodeToString(append(make([]byte, k), value...))
	start := [3]int{0, 2, 3}[k]
	end := (k + len(value)) * 8 / 6
	if end <= start {
		return ""
	}
	return encoded[start:end]
}

// AssertNoSecretBytes fails t when any file under root holds a form of any
// value. For JSON Lines files it also Base64-decodes every string field and
// scans the decoded text, because event log chunks carry their data as bytes.
func AssertNoSecretBytes(t testing.TB, root string, values ...string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if leak := findSecret(path, data, values); leak != "" {
			t.Errorf("%s", leak)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
}

// AssertNoSecretText is AssertNoSecretBytes for captured output or an API body.
func AssertNoSecretText(t testing.TB, label, text string, values ...string) {
	t.Helper()
	if leak := findSecret(label, []byte(text), values); leak != "" {
		t.Errorf("%s", leak)
	}
}

func findSecret(label string, data []byte, values []string) string {
	artifacts := [][]byte{data}
	for _, line := range bytes.Split(data, []byte("\n")) {
		artifacts = append(artifacts, decodedStrings(line)...)
	}
	for _, value := range values {
		for _, form := range SecretForms(value) {
			for _, artifact := range artifacts {
				if bytes.Contains(artifact, []byte(form)) {
					return fmt.Sprintf("%s holds a secret form of %d bytes (form length %d)", label, len(value), len(form))
				}
			}
		}
	}
	return ""
}

// decodedStrings returns the Base64 decoding of every JSON string in line that
// decodes, at any nesting depth. A line that is not JSON yields nothing.
func decodedStrings(line []byte) [][]byte {
	var document any
	if json.Unmarshal(line, &document) != nil {
		return nil
	}
	var decoded [][]byte
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case string:
			if raw, err := base64.StdEncoding.DecodeString(typed); err == nil && len(raw) > 0 {
				decoded = append(decoded, raw)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(document)
	return decoded
}
