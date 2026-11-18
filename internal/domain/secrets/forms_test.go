package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"math/rand"
	"net/url"
	"strings"
	"testing"
)

func has(forms [][]byte, want string) bool {
	for _, f := range forms {
		if string(f) == want {
			return true
		}
	}
	return false
}

// The oracle shares no code with Forms: it embeds the secret in random buffers
// at every byte offset modulo 3, encodes the whole buffer with all four
// standard encodings and requires that some form still appears in the output.
func TestFormsMatchIndependentOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	encodings := map[string]*base64.Encoding{
		"std": base64.StdEncoding, "url": base64.URLEncoding, "raw-std": base64.RawStdEncoding, "raw-url": base64.RawURLEncoding,
	}
	for round := 0; round < 4000; round++ {
		secret := make([]byte, 8+rng.Intn(40))
		for i := range secret {
			secret[i] = byte(0x21 + rng.Intn(0x5e))
		}
		forms := Forms(string(secret))
		prefix := make([]byte, rng.Intn(9))
		suffix := make([]byte, rng.Intn(6))
		rng.Read(prefix)
		rng.Read(suffix)
		buffer := append(append(append([]byte{}, prefix...), secret...), suffix...)
		for name, enc := range encodings {
			out := []byte(enc.EncodeToString(buffer))
			found := false
			for _, f := range forms {
				if bytes.Contains(out, f) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("round %d %s: no form of %q occurs in %q (prefix %d bytes, suffix %d bytes)", round, name, secret, out, len(prefix), len(suffix))
			}
		}
	}
}

func TestFormsShortValueRawOnly(t *testing.T) {
	if got := Forms(""); len(got) != 0 {
		t.Fatalf("empty value has no forms: %q", got)
	}
	got := Forms("s3cr3t!")
	if len(got) != 1 || string(got[0]) != "s3cr3t!" {
		t.Fatalf("a value under 8 bytes yields raw only, got %q", got)
	}
}

func TestFormsHexAndPercentCaseVariants(t *testing.T) {
	value := "p@ss word/+9=Z"
	forms := Forms(value)
	for _, want := range []string{
		value,
		hex.EncodeToString([]byte(value)),
		strings.ToUpper(hex.EncodeToString([]byte(value))),
		url.QueryEscape(value),
		url.PathEscape(value),
		base64.StdEncoding.EncodeToString([]byte(value))[:18],
		base64.URLEncoding.EncodeToString([]byte(value))[:18],
	} {
		if !has(forms, want) {
			t.Errorf("missing form %q in %q", want, forms)
		}
	}
	plain := Forms("abcdefgh")
	count := 0
	for _, f := range plain {
		if string(f) == "abcdefgh" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("percent forms equal to raw must not repeat it: %q", plain)
	}
}

func TestFormsAreUniqueAndBounded(t *testing.T) {
	for _, value := range []string{"abcdefgh", "p@ss word/+9=Z", strings.Repeat("é€", 40), strings.Repeat("a", MaxSecretBytes)} {
		forms := Forms(value)
		if len(forms) > MaxForms {
			t.Fatalf("%d forms exceed the bound %d", len(forms), MaxForms)
		}
		seen := map[string]bool{}
		for _, f := range forms {
			if len(f) == 0 || seen[string(f)] {
				t.Fatalf("empty or duplicate form %q", f)
			}
			seen[string(f)] = true
		}
	}
}
