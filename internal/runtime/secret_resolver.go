package runtime

import (
	"context"

	"agentplane/internal/domain/secrets"
)

// SecretResolver resolves one secret reference to its value (F3.8, ADR-0029).
// It reports facts only: the value, or an error wrapping secrets.ErrNotFound or
// secrets.ErrProviderUnavailable. Judging the value (empty, NUL, UTF-8) belongs
// to the runtime preflight. The composition root supplies the implementation;
// nothing in the scheduler consumes it until the secret preflight lands (C4.2).
type SecretResolver interface {
	Resolve(ctx context.Context, ref secrets.Ref) (secrets.Value, error)
}
