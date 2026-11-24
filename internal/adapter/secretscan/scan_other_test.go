//go:build !unix

package secretscan

import (
	"context"
	"errors"
	"testing"

	"agentplane/internal/domain/secrets"
)

func TestScanReportsOtherOSAsUnobservable(t *testing.T) {
	_, err := Scanner{}.ScanFiles(context.Background(), t.TempDir(), []string{"a"}, nil, secrets.DefaultScanLimits())
	if !errors.Is(err, secrets.ErrUnobservable) {
		t.Errorf("err = %v, want ErrUnobservable", err)
	}
}
