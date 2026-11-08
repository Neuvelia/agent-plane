//go:build darwin || linux

package capture

import (
	"fmt"
	"os"
	"syscall"
)

func directoryIdentity(info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrWorkspaceChanged
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
