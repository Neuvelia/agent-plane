//go:build !darwin && !linux

package capture

import "os"

// Native no-follow identity verification is not yet proven on this platform.
func directoryIdentity(os.FileInfo) (string, error) { return "", ErrWorkspaceChanged }
