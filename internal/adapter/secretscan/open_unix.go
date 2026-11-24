//go:build unix

package secretscan

import (
	"os"
	"syscall"
)

// openNoFollow opens path read-only without following a final symlink and
// without blocking on a FIFO; the caller verifies the opened file is the
// regular file it saw with Lstat.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
