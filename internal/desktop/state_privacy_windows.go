//go:build windows

package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Create the final state component with a protected, inheritable ACL. Files
// created inside inherit only the current user, SYSTEM and Administrators.
// The parent must already exist and must not contain a static reparse point.
func createStateDirectory(dir string) error {
	volume := filepath.VolumeName(dir)
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || len(volume) != 2 || volume[1] != ':' {
		return ErrDocumentSaveUnavailable
	}
	for parent := filepath.Dir(dir); ; parent = filepath.Dir(parent) {
		name, err := windows.UTF16PtrFromString(parent)
		if err != nil {
			return ErrDocumentSaveUnavailable
		}
		attributes, err := windows.GetFileAttributes(name)
		if err != nil || attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return ErrDocumentSaveUnavailable
		}
		if parent == volume+string(filepath.Separator) {
			break
		}
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return ErrDocumentSaveUnavailable
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;GA;;;SY)(A;OICI;GA;;;BA)(A;OICI;GA;;;" + sid + ")")
	if err != nil {
		return ErrDocumentSaveUnavailable
	}
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return ErrDocumentSaveUnavailable
	}
	if err := windows.CreateDirectory(name, &attrs); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return ErrDocumentSaveUnavailable
	}
	return nil
}

func privateStateDirectory(file *os.File) error { return privateWindowsFile(file, true) }
func privateStateRegular(file *os.File) error   { return privateWindowsFile(file, false) }
