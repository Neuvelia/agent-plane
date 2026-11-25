package envsecrets

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentplane/internal/domain/secrets"
	"agentplane/internal/runtime"
)

var _ runtime.SecretResolver = Provider{}

const canary = "s3cr3t-canary-value"

func envRef(name string) secrets.Ref { return secrets.Ref{Provider: secrets.ProviderEnv, Name: name} }

func lookupFrom(values map[string]string, asked *[]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if asked != nil {
			*asked = append(*asked, name)
		}
		v, ok := values[name]
		return v, ok
	}
}

func TestProviderResolvesSetVariable(t *testing.T) {
	p := Provider{Lookup: lookupFrom(map[string]string{"api_token": canary}, nil)}
	got, err := p.Resolve(context.Background(), envRef("api_token"))
	if err != nil || got.Reveal() != canary {
		t.Fatalf("Resolve = %v, %v; want the value", got, err)
	}
}

func TestProviderUnsetIsErrNotFound(t *testing.T) {
	p := Provider{Lookup: lookupFrom(nil, nil)}
	_, err := p.Resolve(context.Background(), envRef("missing"))
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestProviderReturnsEmptyValueAsFact(t *testing.T) {
	p := Provider{Lookup: lookupFrom(map[string]string{"empty": ""}, nil)}
	got, err := p.Resolve(context.Background(), envRef("empty"))
	if err != nil || got.Reveal() != "" {
		t.Fatalf("Resolve = %v, %v; want empty value and no error", got, err)
	}
}

func TestProviderRejectsOtherProviders(t *testing.T) {
	p := Provider{Lookup: lookupFrom(map[string]string{"x": canary}, nil)}
	for _, provider := range []string{secrets.ProviderLocal, "vault"} {
		_, err := p.Resolve(context.Background(), secrets.Ref{Provider: provider, Name: "x"})
		if !errors.Is(err, secrets.ErrProviderUnavailable) {
			t.Errorf("provider %q: err = %v, want ErrProviderUnavailable", provider, err)
		}
	}
}

func TestProviderNilLookupIsUnavailable(t *testing.T) {
	_, err := Provider{}.Resolve(context.Background(), envRef("x"))
	if !errors.Is(err, secrets.ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
}

func TestProviderLooksUpNameExactlyAsGiven(t *testing.T) {
	var asked []string
	p := Provider{Lookup: lookupFrom(nil, &asked)}
	_, _ = p.Resolve(context.Background(), envRef("mixed_case_1"))
	if len(asked) != 1 || asked[0] != "mixed_case_1" {
		t.Fatalf("lookups = %q, want exactly [mixed_case_1]", asked)
	}
}

func TestProviderStopsOnCancelledContext(t *testing.T) {
	var asked []string
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Provider{Lookup: lookupFrom(nil, &asked)}.Resolve(ctx, envRef("x"))
	if !errors.Is(err, context.Canceled) || len(asked) != 0 {
		t.Fatalf("err = %v, lookups = %q; want context.Canceled and no lookup", err, asked)
	}
}

func TestProviderErrorsNeverContainValue(t *testing.T) {
	p := Provider{Lookup: lookupFrom(map[string]string{"set": canary}, nil)}
	_, errOther := p.Resolve(context.Background(), secrets.Ref{Provider: "vault", Name: "set"})
	_, errMissing := Provider{Lookup: lookupFrom(nil, nil)}.Resolve(context.Background(), envRef("set"))
	for _, err := range []error{errOther, errMissing} {
		if err == nil || strings.Contains(err.Error(), canary) {
			t.Errorf("error %v must exist and not contain the value", err)
		}
	}
}
