//go:build !darwin && !linux && !windows

package desktop

import "os"

// Non-Unix platforms get traversal confinement from os.Root. Native no-follow
// and reparse-point verification are required before Windows packaging.
func openRegularDocument(root *os.Root, relative string) (*os.File, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, ErrInvalidDocumentPath
	}
	return file, nil
}
