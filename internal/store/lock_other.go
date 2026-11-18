//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package store

import "os"

func tryLock(*os.File, bool) (bool, error) { return false, ErrLockUnsupported }

func unlock(*os.File) error { return ErrLockUnsupported }
