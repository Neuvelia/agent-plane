//go:build windows

package process

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

const statusEnvName = "AGENTPLANE_SUPERVISOR_STATUS_HANDLE"

func configureStatus(cmd *exec.Cmd, file *os.File, _ []*os.File) (func(), error) {
	var inherited windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &inherited, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(inherited)}}
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%d", statusEnvName, inherited))
	return func() { _ = windows.CloseHandle(inherited) }, nil
}

func openStatusFileFromEnvironment() *os.File {
	handle, err := strconv.ParseUint(os.Getenv(statusEnvName), 10, 64)
	if err != nil || handle == 0 {
		return nil
	}
	return os.NewFile(uintptr(handle), "supervisor-status")
}
