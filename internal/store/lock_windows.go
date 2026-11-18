//go:build windows

package store

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock locks byte range [0,1) without waiting; the range is a convention,
// the file is never read. ERROR_LOCK_VIOLATION means another handle holds it.
func tryLock(f *os.File, exclusive bool) (held bool, err error) {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, new(windows.Overlapped))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
