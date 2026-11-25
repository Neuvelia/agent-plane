package secrets

import "errors"

// Sentinel facts a provider reports when it cannot produce a value. Callers
// branch on them with errors.Is; judging a returned value (empty, NUL, UTF-8)
// is the runtime's decision, not the provider's.
var (
	// ErrNotFound means the provider is reachable but holds no secret of that name.
	ErrNotFound = errors.New("secret not found")
	// ErrProviderUnavailable means the provider cannot resolve references at all
	// (unknown, reserved or not configured).
	ErrProviderUnavailable = errors.New("secret provider unavailable")
)
