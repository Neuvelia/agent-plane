package runtime

import (
	"bytes"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
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

func TestLogRedactorFlushCutMasksATailThatCouldBeginASecret(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		secrets           []string
	}{
		{name: "partial secret at the cut", secrets: []string{"secret"}, input: "log secr", want: "log ***"},
		{name: "complete secret at the cut", secrets: []string{"secret"}, input: "log secret", want: "log ***"},
		{name: "tail that begins no secret", secrets: []string{"secret"}, input: "log sex", want: "log sex"},
		{name: "longer secret continues past a shorter match", secrets: []string{"abc", "abcdef"}, input: "x abcd", want: "x ***"},
		{name: "partial secret split across writes", secrets: []string{"secret"}, input: "log se|cr", want: "log ***"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redactor := mustLogRedactor(t, tc.secrets)
			var got []byte
			for _, part := range strings.Split(tc.input, "|") {
				got = append(got, redactor.Write(events.Stdout, []byte(part))...)
			}
			got = append(got, redactor.FlushCut(events.Stdout)...)
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLogRedactorFlushKeepsATailThatOnlyBeginsASecret(t *testing.T) {
	redactor := mustLogRedactor(t, []string{"secret"})
	got := append(redactor.Write(events.Stdout, []byte("log secr")), redactor.Flush(events.Stdout)...)
	if string(got) != "log secr" {
		t.Fatalf("a complete stream has no hidden continuation; got %q", got)
	}
}
