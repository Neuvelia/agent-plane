package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
)

func mustRedactionSet(t *testing.T, logValues []string, resolved ...resolvedSecret) *logRedactor {
	t.Helper()
	redactor, err := newRedactionSet(logValues, resolved)
	if err != nil {
		t.Fatal(err)
	}
	return redactor
}

// redactAll feeds data through the redactor in chunks cut at cut (or whole when
// cut is 0) and returns everything it emitted.
func redactAll(r *logRedactor, data []byte, cut int) []byte {
	var out bytes.Buffer
	if cut <= 0 || cut >= len(data) {
		out.Write(r.Write(events.Stdout, data))
	} else {
		out.Write(r.Write(events.Stdout, data[:cut]))
		out.Write(r.Write(events.Stdout, data[cut:]))
	}
	out.Write(r.Flush(events.Stdout))
	return out.Bytes()
}

func TestResolvedSecretSplitAcrossWrites(t *testing.T) {
	const value = "ghp_Example0Token9Value"
	input := []byte("before " + value + " after")
	for cut := 1; cut < len(input); cut++ {
		redactor := mustRedactionSet(t, nil, resolvedSecret{name: "gh_token", value: value})
		got := string(redactAll(redactor, input, cut))
		if strings.Contains(got, "ghp_") || strings.Contains(got, "Token9") || !strings.HasPrefix(got, "before ") || !strings.HasSuffix(got, " after") {
			t.Fatalf("split at %d: %q", cut, got)
		}
	}
}

// For each of the three alignments of the secret inside a larger Base64 blob
// the stable characters must be replaced, however the stream is chunked.
func TestRedactionBase64AllAlignments(t *testing.T) {
	const value = "ghp_Example0Token9Value"
	rng := rand.New(rand.NewSource(11))
	encodings := map[string]*base64.Encoding{"std": base64.StdEncoding, "url": base64.URLEncoding, "raw-std": base64.RawStdEncoding, "raw-url": base64.RawURLEncoding}
	for prefixLen := 0; prefixLen < 3; prefixLen++ {
		prefix := make([]byte, prefixLen)
		suffix := make([]byte, 4)
		rng.Read(prefix)
		rng.Read(suffix)
		blob := append(append(append([]byte{}, prefix...), value...), suffix...)
		for name, enc := range encodings {
			encoded := []byte(enc.EncodeToString(blob))
			// Characters that depend only on secret bytes, derived from the bit layout.
			start, end := (8*prefixLen+5)/6, 8*(prefixLen+len(value))/6
			stable := string(encoded[start:end])
			for cut := 0; cut < len(encoded); cut += 3 {
				redactor := mustRedactionSet(t, nil, resolvedSecret{name: "gh_token", value: value})
				got := redactAll(redactor, encoded, cut)
				if bytes.Contains(got, []byte(stable)) || len(got) >= len(encoded) {
					t.Fatalf("alignment %d %s cut %d: stable part %q survived in %q", prefixLen, name, cut, stable, got)
				}
			}
		}
	}
}

func TestRedactionSetCoversHexAndPercentForms(t *testing.T) {
	const value = "p@ss word/+9=Zq"
	for name, form := range map[string]string{
		"hex":   hex.EncodeToString([]byte(value)),
		"HEX":   strings.ToUpper(hex.EncodeToString([]byte(value))),
		"query": url.QueryEscape(value),
		"path":  url.PathEscape(value),
	} {
		redactor := mustRedactionSet(t, nil, resolvedSecret{name: "pw", value: value})
		got := string(redactAll(redactor, []byte("x "+form+" y"), 0))
		if strings.Contains(got, form) || !strings.HasPrefix(got, "x ") || !strings.HasSuffix(got, " y") || len(got) >= len(form)+4 {
			t.Errorf("%s form %q survived: %q", name, form, got)
		}
	}
}

func TestRedactorMarkerAvoidsEncodedBytes(t *testing.T) {
	// Raw value uses the first marker candidates, so the marker must move; the
	// forms add '%', '+', '-', '_' and '/' which it must also avoid.
	redactor := mustRedactionSet(t, nil, resolvedSecret{name: "s", value: "*#~^|!?@%+=:;abcdefgh"})
	for _, form := range secrets.Forms("*#~^|!?@%+=:;abcdefgh") {
		for _, b := range redactor.marker {
			if bytes.IndexByte(form, b) >= 0 {
				t.Fatalf("marker %q uses byte %q found in form %q", redactor.marker, b, form)
			}
		}
	}
}

func TestRedactionSetKeepsLogRedactionsByteIdentical(t *testing.T) {
	values := []string{"alpha-secret", "beta"}
	old := mustLogRedactor(t, values)
	set := mustRedactionSet(t, values)
	input := []byte("alpha-secret and beta, hex " + hex.EncodeToString([]byte("alpha-secret")))
	if !bytes.Equal(redactAll(old, input, 5), redactAll(set, input, 5)) {
		t.Fatal("log redactions alone must behave exactly as before; forms apply to resolved secrets only")
	}
}

func TestRedactorBoundsRejectOversizedSet(t *testing.T) {
	many := make([]resolvedSecret, secrets.MaxSecrets+1)
	for i := range many {
		many[i] = resolvedSecret{name: fmt.Sprintf("s%02d", i), value: fmt.Sprintf("value-number-%02d", i)}
	}
	if _, err := newRedactionSet(nil, many[:secrets.MaxSecrets]); err != nil {
		t.Fatalf("exactly %d secrets are within the bound: %v", secrets.MaxSecrets, err)
	}
	big := resolvedSecret{name: "huge", value: strings.Repeat("a", secrets.MaxSecretBytes)}
	if _, err := newRedactionSet(nil, []resolvedSecret{big}); err != nil {
		t.Fatalf("a %d-byte secret is within the bound: %v", secrets.MaxSecretBytes, err)
	}
	for name, set := range map[string][]resolvedSecret{
		"too many":  many,
		"too large": {{name: "huge", value: strings.Repeat("a", secrets.MaxSecretBytes+1)}},
	} {
		_, err := newRedactionSet(nil, set)
		var pre *run.PreflightError
		if !errors.As(err, &pre) {
			t.Fatalf("%s: want a structured preflight error, got %v", name, err)
		}
	}
}

func TestRedactionSetBoundDiagnosticNamesSecretAndBound(t *testing.T) {
	huge := strings.Repeat("Z", secrets.MaxSecretBytes+1)
	many := make([]resolvedSecret, secrets.MaxSecrets+1)
	for i := range many {
		many[i] = resolvedSecret{name: fmt.Sprintf("s%02d", i), value: fmt.Sprintf("value-number-%02d", i)}
	}
	for name, c := range map[string]struct {
		set      []resolvedSecret
		wantPath string
		wantText string
	}{
		"one secret above 16 KiB": {[]resolvedSecret{{name: "big_token", value: huge}}, "secrets.big_token", `"big_token"`},
		"more than 64 secrets":    {many, "secrets", fmt.Sprint(secrets.MaxSecrets)},
	} {
		_, err := newRedactionSet(nil, c.set)
		var pre *run.PreflightError
		if !errors.As(err, &pre) || len(pre.Diagnostics.Diagnostics) != 1 {
			t.Fatalf("%s: want one preflight diagnostic, got %v", name, err)
		}
		d := pre.Diagnostics.Diagnostics[0]
		if d.Code != "secret_redaction_bound_exceeded" || d.Location.Path != c.wantPath || !strings.Contains(d.Message, c.wantText) || d.Suggestion == "" {
			t.Fatalf("%s: diagnostic = %+v", name, d)
		}
		if strings.Contains(d.Message+d.Suggestion+err.Error(), "ZZZZ") || strings.Contains(d.Message+d.Suggestion, "value-number") {
			t.Fatalf("%s: the diagnostic must never carry a value: %+v", name, d)
		}
	}
}

func TestPreflightRedactionWithoutResolvedSecretsIsUnchanged(t *testing.T) {
	r, err := newRedactionSet([]string{"abc"}, nil)
	if err != nil || string(redactAll(r, []byte("xabcx"), 0)) != "x***x" {
		t.Fatalf("log redactions must work as before: %v", err)
	}
	if _, err := newRedactionSet(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkRedactorWithForms(b *testing.B) {
	resolved := make([]resolvedSecret, 16)
	for i := range resolved {
		resolved[i] = resolvedSecret{name: fmt.Sprintf("s%02d", i), value: strings.Repeat(fmt.Sprintf("tok%02d", i), 8)}
	}
	r, err := newRedactionSet([]string{"--redact-env-value"}, resolved)
	if err != nil {
		b.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("ordinary log output with no secrets in it at all\n"), 20)
	b.SetBytes(int64(len(chunk)))
	for i := 0; i < b.N; i++ {
		r.Write(events.Stdout, chunk)
		r.Flush(events.Stdout)
	}
}

func TestPreflightRedactionErrorWording(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, nil
	})
	_, err := preflightRun(context.Background(), definition(map[string]graph.NodeDefinition{"worker": {Type: "shell"}}), nil, exec, Options{LogRedactions: []string{string(allBytes)}})
	if err == nil {
		t.Fatal("redactions without a safe marker were accepted")
	}
	if !strings.Contains(err.Error(), "invalid redactions") || strings.Contains(err.Error(), "invalid log redactions") {
		t.Fatalf("error wording = %q", err)
	}
}

func TestRedactionPreflightErrorKeepsStructuredDiagnostic(t *testing.T) {
	structured := redactionBoundError("secrets.token", "too large", "use a smaller secret")
	err := redactionPreparationError(structured)
	var pre *run.PreflightError
	if !errors.As(err, &pre) || !errors.Is(err, structured) {
		t.Fatalf("structured diagnostic was wrapped: %#v", err)
	}
	if strings.Contains(err.Error(), "invalid") {
		t.Fatalf("structured error gained a prefix: %q", err)
	}
}
