//go:build !darwin && !linux && !windows

package desktop

import "os"

const documentCreateAvailable = false

func linkDocumentExclusive(_ *os.Root, _ *os.File, _, _ string) error {
	return ErrDocumentCreateUnavailable
}
