package runtime

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/events"
)

func mustLogRedactor(t *testing.T, secrets []string) *logRedactor {
	t.Helper()
	redactor, err := newLogRedactor(secrets)
	if err != nil {
		t.Fatal(err)
	}
	return redactor
}

func TestLogRedactorHidesSecretAcrossWritesOnSeparateStreams(t *testing.T) {
	redactor := mustLogRedactor(t, []string{"secret"})
	var stdout, stderr bytes.Buffer
	stdout.Write(redactor.Write(events.Stdout, []byte("before sec")))
	stderr.Write(redactor.Write(events.Stderr, []byte("error sec")))
	stdout.Write(redactor.Write(events.Stdout, []byte("ret after")))
	stderr.Write(redactor.Write(events.Stderr, []byte("ret done")))
	stdout.Write(redactor.Flush(events.Stdout))
	stderr.Write(redactor.Flush(events.Stderr))
	if got := stdout.String(); got != "before *** after" {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); got != "error *** done" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestLogRedactorHidesSecretAtEveryWriteBoundary(t *testing.T) {
	input := []byte("prefix secret suffix")
	for cut := 1; cut < len(input); cut++ {
		redactor := mustLogRedactor(t, []string{"secret"})
		var output bytes.Buffer
		output.Write(redactor.Write(events.Stdout, input[:cut]))
		output.Write(redactor.Write(events.Stdout, input[cut:]))
		output.Write(redactor.Flush(events.Stdout))
		if got := output.String(); got != "prefix *** suffix" {
			t.Fatalf("split at %d: output = %q", cut, got)
		}
	}
}

func TestLogRedactorChoosesLongestMatchAndPreservesOtherBytes(t *testing.T) {
	redactor := mustLogRedactor(t, []string{"ab", "abc", ""})
	var output bytes.Buffer
	output.Write(redactor.Write(events.Stdout, []byte("zab")))
	output.Write(redactor.Write(events.Stdout, []byte("cq")))
	output.Write(redactor.Flush(events.Stdout))
	if got := output.String(); got != "z***q" {
		t.Fatalf("output = %q", got)
	}
}

func TestLogRedactorWithoutSecretsPreservesBinaryBytes(t *testing.T) {
	redactor := mustLogRedactor(t, nil)
	input := []byte{0, 255, 'x'}
	got := append(redactor.Write(events.Stdout, input), redactor.Flush(events.Stdout)...)
	if !bytes.Equal(got, input) {
		t.Fatalf("output = %v, want %v", got, input)
	}
}

func TestLogRedactorMarkerCannotRevealConfiguredSecret(t *testing.T) {
	for _, test := range []struct {
		name    string
		secrets []string
		input   string
	}{
		{name: "secret inside marker", secrets: []string{"REDACTED"}, input: "REDACTED"},
		{name: "secret crossing marker", secrets: []string{"a[", "b"}, input: "ab"},
	} {
		t.Run(test.name, func(t *testing.T) {
			redactor := mustLogRedactor(t, test.secrets)
			output := string(redactor.Write(events.Stdout, []byte(test.input))) + string(redactor.Flush(events.Stdout))
			for _, secret := range test.secrets {
				if strings.Contains(output, secret) {
					t.Fatalf("redacted output %q still contains %q", output, secret)
				}
			}
		})
	}
}

func TestLogRedactorRejectsValuesCoveringEveryByte(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	if _, err := newLogRedactor([]string{string(allBytes)}); err == nil {
		t.Fatal("redactor accepted values with no safe marker byte")
	}
}

func TestCaptureRedactorWholeFieldAndDetachedValues(t *testing.T) {
	values := []string{"secret"}
	redactor, err := NewCaptureRedactor(values)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = "changed"
	input := []byte("prefix secret suffix")
	output := redactor.Redact(input)
	if string(output) != "prefix *** suffix" {
		t.Fatalf("whole field output = %q", output)
	}
	input[0] = 'X'
	output[0] = 'Y'
	if got := string(redactor.Redact([]byte("prefix secret suffix"))); got != "prefix *** suffix" {
		t.Fatalf("shared state or alias changed output = %q", got)
	}
	withoutSecrets, err := NewCaptureRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("plain")
	detached := withoutSecrets.Redact(plain)
	plain[0] = 'X'
	if string(detached) != "plain" {
		t.Fatalf("no-secret result aliases input: %q", detached)
	}
}

func TestCaptureRedactorBinaryEvidenceAndMarkerCollision(t *testing.T) {
	redactor, err := NewCaptureRedactor([]string{"SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte{0, 255, 'S', 'E', 'C', 'R', 'E', 'T', 0}
	if got := redactor.Redact(input); !bytes.Equal(got, []byte{0, 255, '*', '*', '*', 0}) {
		t.Fatalf("binary evidence = %v", got)
	}
	if !bytes.Equal(input, []byte{0, 255, 'S', 'E', 'C', 'R', 'E', 'T', 0}) {
		t.Fatal("input bytes changed")
	}
	collision, err := NewCaptureRedactor([]string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	if got := collision.Redact([]byte("a*b")); bytes.Contains(got, []byte{'*'}) {
		t.Fatalf("marker reintroduced forbidden byte: %q", got)
	}
	if _, err := NewCaptureRedactor([]string{string([]byte{0xff})}); err == nil {
		t.Fatal("invalid UTF-8 redaction value accepted")
	}
}

func TestCaptureRedactorConcurrentCallsAreIndependent(t *testing.T) {
	redactor, err := NewCaptureRedactor([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				if got := string(redactor.Redact([]byte("before private after"))); got != "before *** after" {
					t.Errorf("concurrent output = %q", got)
					return
				}
			}
		}()
	}
	workers.Wait()
}
