//go:build windows

package desktop

import (
	"os"
	"path"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// os.Root confines traversal. Verify the opened handle and the final path
// identity before reading any bytes; Windows mode bits alone do not identify
// junctions or other reparse points.
func openRegularDocument(root *os.Root, relative string) (*os.File, error) {
	parent, err := root.OpenRoot(filepath.FromSlash(path.Dir(relative)))
	if err != nil {
		return nil, ErrInvalidDocumentPath
	}
	defer parent.Close()
	name := path.Base(relative)
	before, err := parent.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return nil, ErrInvalidDocumentPath
	}
	file, err := parent.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, ErrInvalidDocumentPath
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		file.Close()
		return nil, ErrInvalidDocumentPath
	}
	var handleInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &handleInfo); err != nil || handleInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		file.Close()
		return nil, ErrInvalidDocumentPath
	}
	after, err := parent.Lstat(name)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(info, after) {
		file.Close()
		return nil, ErrInvalidDocumentPath
	}
	return file, nil
}
