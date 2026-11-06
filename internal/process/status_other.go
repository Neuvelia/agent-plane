//go:build !darwin && !linux && !windows

package process

import (
	"errors"
	"os"
	"os/exec"
)

func configureStatus(*exec.Cmd, *os.File, []*os.File) (func(), error) {
	return nil, errors.New("process supervisor is unsupported on this platform")
}

func openStatusFileFromEnvironment() *os.File { return nil }
