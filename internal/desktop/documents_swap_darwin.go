//go:build darwin

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
	return unix.RenameatxNp(int(dir.Fd()), from, int(dir.Fd()), to, unix.RENAME_SWAP)
}
