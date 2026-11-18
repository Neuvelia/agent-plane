//go:build !windows

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// syncDir flushes a directory so a newly created entry survives power loss.
// Filesystems that cannot sync a directory report EINVAL or ENOTSUP; that is
// not a failure of the store.
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %s for sync: %w", path, err)
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}
