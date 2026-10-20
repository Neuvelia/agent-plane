package desktop

import (
	"errors"
	"io"
)

var ErrRegistryBusy = errors.New("local registry is open in another process")
var ErrRegistryLockUnsafe = errors.New("local registry lock path is not private and regular")
var ErrRegistryLockUnavailable = errors.New("local registry lock is unavailable on this platform")

const registryLockName = ".registry.lock"

// registryLock must be verified at each registry operation because a same-user
// process could unlink and recreate the lock path while the old inode is held.
type registryLock interface {
	io.Closer
	Verify() error
}
