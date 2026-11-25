//go:build !unix

package secretscan

import (
	"context"

	"agentplane/internal/domain/secrets"
)

// ScanFiles reports secrets.ErrUnobservable: without O_NOFOLLOW and Lstat
// semantics a scan of untrusted paths cannot be made safe, so the platform is
// unobserved by construction.
func (Scanner) ScanFiles(context.Context, string, []string, []secrets.ScanTarget, secrets.ScanLimits) (secrets.ScanReport, error) {
	return secrets.ScanReport{}, secrets.ErrUnobservable
}
