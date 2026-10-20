//go:build darwin || linux

package desktop

import (
	"fmt"
	"os"
	"syscall"
)

func identityOfFile(file *os.File) (ProjectIdentity, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrInvalidDocumentPath
	}
	return ProjectIdentity(fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)), nil
}
