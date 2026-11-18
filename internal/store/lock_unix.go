//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package store

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes a non-blocking flock; held is false when another open file
// description owns a conflicting lock. flock locks belong to the open file
// description, so a second open in the same process conflicts as well.
func tryLock(f *os.File, exclusive bool) (held bool, err error) {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}
	for {
		err = unix.Flock(int(f.Fd()), how|unix.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EWOULDBLOCK):
			return false, nil
		default:
			return false, err
		}
	}
}

func unlock(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
