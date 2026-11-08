//go:build darwin || linux

package desktop

import (
	"os"

	"golang.org/x/sys/unix"
)

const documentCreateAvailable = true

func linkDocumentExclusive(parent *os.Root, _ *os.File, from, to string) error {
	dir, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.Linkat(int(dir.Fd()), from, int(dir.Fd()), to, 0)
}
