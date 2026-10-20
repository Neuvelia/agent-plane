//go:build windows

package desktop

import (
	"errors"
	"testing"
)

func TestRegistryLockFailsClosedUntilNativeACLVerification(t *testing.T) {
	lock, err := acquireRegistryLock(t.TempDir())
	if lock != nil || !errors.Is(err, ErrRegistryLockUnavailable) {
		t.Fatalf("Windows lock = %v, %v", lock, err)
	}
}
