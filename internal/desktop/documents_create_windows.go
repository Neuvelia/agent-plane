//go:build windows

package desktop

import (
	"errors"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const documentCreateAvailable = true

// Link a pinned, complete regular file under a pinned parent directory. NT's
// FileLinkInformation with ReplaceIfExists=0 fails on a target collision. This
// avoids a path-based hard-link call that could follow a replaced parent.
func linkDocumentExclusive(parent *os.Root, candidate *os.File, from, to string) error {
	opened, err := parent.OpenFile(from, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer opened.Close()
	openedInfo, err := opened.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() {
		return ErrInvalidDocumentPath
	}
	candidateInfo, err := candidate.Stat()
	if err != nil || !os.SameFile(openedInfo, candidateInfo) {
		return ErrDocumentConflict
	}
	entryInfo, err := parent.Lstat(from)
	if err != nil || entryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, entryInfo) {
		return ErrDocumentConflict
	}
	var handleInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(opened.Fd()), &handleInfo); err != nil || handleInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrInvalidDocumentPath
	}
	dir, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	name, err := syscall.UTF16FromString(to)
	if err != nil || len(name) > syscall.MAX_PATH {
		return ErrInvalidDocumentPath
	}
	info := fileLinkInformation{RootDirectory: windows.Handle(dir.Fd()), FileNameLength: uint32((len(name) - 1) * 2)}
	copy(info.FileName[:], name)
	err = windows.NtSetInformationFile(windows.Handle(opened.Fd()), &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), 11)
	if status, ok := err.(windows.NTStatus); ok {
		err = status.Errno()
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return os.ErrExist
	}
	return err
}

// The layout matches the native FILE_LINK_INFORMATION used by NtSetInformationFile.
// The fixed UTF-16 buffer is sufficient for one Windows directory component;
// longer names fail closed above.
type fileLinkInformation struct {
	ReplaceIfExists byte
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [syscall.MAX_PATH]uint16
}
