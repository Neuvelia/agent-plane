package secrets

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRefGrammar(t *testing.T) {
	good := map[string]Ref{
		"env/GITHUB_TOKEN":                  {Provider: "env", Name: "GITHUB_TOKEN"},
		"local/my.token-1":                  {Provider: "local", Name: "my.token-1"},
		"a/b":                               {Provider: "a", Name: "b"},
		"p-1/" + strings.Repeat("x", 128):   {Provider: "p-1", Name: strings.Repeat("x", 128)},
		strings.Repeat("a", 32) + "/n":      {Provider: strings.Repeat("a", 32), Name: "n"},
		"env/~!@#$%^&*()_+=[]{}|;:'\",.<>?": {Provider: "env", Name: "~!@#$%^&*()_+=[]{}|;:'\",.<>?"},
	}
	for in, want := range good {
		got, err := ParseRef(in)
		if err != nil || got != want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", in, got, err, want)
		}
		if got.String() != in {
			t.Errorf("String() = %q, want %q", got.String(), in)
		}
	}
	bad := []string{
		"", "env", "env/", "/NAME", "Env/NAME", "1env/NAME", "en_v/NAME", "env/a/b", "env/has space", "env/tab\t",
		"env/new\nline", "env/nul\x00", "env/é", "env/" + strings.Repeat("x", 129), strings.Repeat("a", 33) + "/n",
		"env/\x7f", "env /NAME",
	}
	for _, in := range bad {
		if _, err := ParseRef(in); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("ParseRef(%q) error = %v, want ErrInvalidRef", in, err)
		}
	}
}

func TestParseRefErrorNeverEchoesTheInput(t *testing.T) {
	_, err := ParseRef("ghp_notARealTokenButLooksLikeOne123")
	if err == nil || strings.Contains(err.Error(), "ghp_") {
		t.Fatalf("error must not echo the text: %v", err)
	}
}

func TestProviderStatus(t *testing.T) {
	for provider, want := range map[string]ProviderStatus{"env": ProviderAvailable, "local": ProviderReserved, "vault": ProviderUnknown, "": ProviderUnknown} {
		if got := StatusOf(provider); got != want {
			t.Errorf("StatusOf(%q) = %v, want %v", provider, got, want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "gh_token", "a1_b2", strings.Repeat("a", 64)} {
		if !ValidName(ok) {
			t.Errorf("ValidName(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "1a", "_a", "A", "gh-token", "gh token", "a.b", strings.Repeat("a", 65), "é"} {
		if ValidName(bad) {
			t.Errorf("ValidName(%q) = true", bad)
		}
	}
}

func TestValidEnvName(t *testing.T) {
	for _, ok := range []string{"GH_TOKEN", "lower", "1A", "A-B"} {
		if !ValidEnvName(ok) {
			t.Errorf("ValidEnvName(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "A=B", "A\x00B", "AGENTPLANE_ATTEMPT", "agentplane_attempt"} {
		if ValidEnvName(bad) {
			t.Errorf("ValidEnvName(%q) = true", bad)
		}
	}
}

func TestValidEnvIdentifierGrammar(t *testing.T) {
	for s, want := range map[string]bool{
		"GH_TOKEN": true, "_x1": true, "a": true, "": false, "1A": false,
		"ghp_Pasted-Token.9": false, "A=B": false, strings.Repeat("A", 128): true, strings.Repeat("A", 129): false,
	} {
		if got := ValidEnvIdentifier(s); got != want {
			t.Errorf("ValidEnvIdentifier(%q) = %v, want %v", s, got, want)
		}
	}
}
