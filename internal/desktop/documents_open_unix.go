//go:build darwin || linux

package desktop

import (
	"os"
	"path"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Opening the parent through os.Root pins a directory inside the registered
// project. O_NONBLOCK prevents a swapped FIFO from hanging the service, and
// O_NOFOLLOW refuses a replacement symlink at the final component.
func openRegularDocument(root *os.Root, relative string) (*os.File, error) {
	parent, err := root.OpenRoot(filepath.FromSlash(path.Dir(relative)))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	file, err := parent.OpenFile(path.Base(relative), os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
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
