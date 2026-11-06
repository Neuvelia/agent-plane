//go:build darwin || linux

package process

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const statusEnvName = "AGENTPLANE_SUPERVISOR_STATUS_FD"

func configureStatus(cmd *exec.Cmd, file *os.File, extra []*os.File) (func(), error) {
	cmd.ExtraFiles = append(append([]*os.File(nil), extra...), file)
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%d", statusEnvName, 3+len(extra)))
	return func() {}, nil
}

func openStatusFileFromEnvironment() *os.File {
	fd, err := strconv.Atoi(os.Getenv(statusEnvName))
	if err != nil || fd < 3 || fd > 16 {
		return nil
	}
	// The target never receives this private sidecar status capability.
	syscall.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), "supervisor-status")
}
