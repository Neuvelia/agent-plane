//go:build !windows

package desktop

import "golang.org/x/sys/unix"

func toolIsExecutable(path string) bool {
	return unix.Faccessat(unix.AT_FDCWD, path, unix.X_OK, unix.AT_EACCESS) == nil
}
