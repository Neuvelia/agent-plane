//go:build linux

package desktop

import (
	"os"

	"golang.org/x/sys/unix"
)

const documentSwapAvailable = true

func swapDocumentEntries(parent *os.Root, from, to string) error {
	dir, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.Renameat2(int(dir.Fd()), from, int(dir.Fd()), to, unix.RENAME_EXCHANGE)
}
