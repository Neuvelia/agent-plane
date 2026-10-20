//go:build !darwin && !linux

package desktop

import "os"

const documentSwapAvailable = false

// Windows replacement needs a backup-aware ReplaceFileW implementation and
// native verification before the desktop package can claim Windows saves.
func swapDocumentEntries(_ *os.Root, _, _ string) error { return ErrDocumentSaveUnavailable }
