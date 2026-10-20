//go:build windows

package desktop

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func identityOfFile(file *os.File) (ProjectIdentity, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return "", err
	}
	return ProjectIdentity(fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow)), nil
}
