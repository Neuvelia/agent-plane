// Package envsecrets is the env secret provider (F3.8, ADR-0029): it reads a
// secret from the runner's own environment. Only the composition root imports
// it (internal/archtest enforces this), and it reports facts only.
package envsecrets

import (
	"context"
	"fmt"

	"agentplane/internal/domain/secrets"
)

// Provider resolves env/<name> references. Lookup is injected by the
// composition root (os.LookupEnv) so the package never touches the process
// environment itself.
type Provider struct {
	Lookup func(name string) (string, bool)
}

// Resolve returns the value of the variable named by ref. An unset variable is
// secrets.ErrNotFound; an empty one is returned as is, because judging it is the
// runtime's decision. Errors never contain the value.
func (p Provider) Resolve(ctx context.Context, ref secrets.Ref) (secrets.Value, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Value{}, err
	}
	if ref.Provider != secrets.ProviderEnv || p.Lookup == nil {
		return secrets.Value{}, fmt.Errorf("provider %q: %w", ref.Provider, secrets.ErrProviderUnavailable)
	}
	text, found := p.Lookup(ref.Name)
	if !found {
		return secrets.Value{}, fmt.Errorf("environment variable is not set: %w", secrets.ErrNotFound)
	}
	return secrets.NewValue(text), nil
}
