//go:build windows

package gate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The native handle walk uses one verified cwd handle for both operands. A
// handle-relative component cannot traverse out through a renamed pathname.
type windowsComparisonRoot struct {
	path     string
	handle   windows.Handle
	identity windowsFileID
}

type windowsAttributeTag struct {
	Attributes uint32
	ReparseTag uint32
}

// FILE_ID_INFO uses the volume serial plus a 128-bit identifier (also on ReFS).
type windowsFileID struct {
	VolumeSerial uint64
	ID           [16]byte
}

const comparisonShare = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

func openComparisonRoot(cwd string) (comparisonRoot, error) {
	if err := validLocalCwd(cwd); err != nil {
		return nil, err
	}
	handle, err := walkWindowsCwd(cwd)
	if err != nil {
		return nil, err
	}
	id, err := windowsHandleID(handle)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("identify comparison cwd: %w", err)
	}
	root := &windowsComparisonRoot{path: cwd, handle: handle, identity: id}
	if err := root.Verify(); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func (r *windowsComparisonRoot) Close() error { return windows.CloseHandle(r.handle) }

func (r *windowsComparisonRoot) Verify() error {
	current, err := walkWindowsCwd(r.path)
	if err != nil {
		return fmt.Errorf("comparison cwd changed or became unsafe: %w", err)
	}
	defer windows.CloseHandle(current)
	id, err := windowsHandleID(current)
	if err != nil {
		return fmt.Errorf("identify comparison cwd: %w", err)
	}
	if id != r.identity {
		return errors.New("comparison cwd changed during open")
	}
	return nil
}

func (r *windowsComparisonRoot) Open(relative string) (*os.File, error) {
	q, err := normalize(relative)
	if err != nil {
		return nil, err
	}
	if err := r.Verify(); err != nil {
		return nil, err
	}
	parts := strings.Split(q, "/")
	for _, part := range parts {
		if err := validWindowsComponent(part); err != nil {
			return nil, err
		}
	}
	parent := r.handle
	for _, part := range parts[:len(parts)-1] {
		child, err := openWindowsChild(parent, part, true)
		if parent != r.handle {
			windows.CloseHandle(parent)
		}
		if err != nil {
			return nil, fmt.Errorf("open comparison path: %w", err)
		}
		parent = child
	}
	if parent != r.handle {
		defer windows.CloseHandle(parent)
	}
	handle, err := openWindowsChild(parent, parts[len(parts)-1], false)
	if err != nil {
		return nil, fmt.Errorf("open comparison input: %w", err)
	}
	file := os.NewFile(uintptr(handle), "comparison input")
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("comparison input must be a regular file")
	}
	if err := r.Verify(); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func validLocalCwd(cwd string) error {
	if len(cwd) < 3 || ((cwd[0] < 'A' || cwd[0] > 'Z') && (cwd[0] < 'a' || cwd[0] > 'z')) || cwd[1] != ':' || cwd[2] != '\\' || filepath.Clean(cwd) != cwd {
		return errors.New("comparison cwd must be a canonical drive-absolute path; UNC and device paths are unsupported")
	}
	for _, part := range strings.Split(strings.TrimPrefix(cwd[3:], "\\"), "\\") {
		if part == "" {
			if len(cwd) == 3 {
				continue
			}
			return errors.New("comparison cwd has an empty path component")
		}
		if err := validWindowsComponent(part); err != nil {
			return err
		}
	}
	return nil
}

// Reject NT stream syntax and Win32 aliases before any native open. Every
// native ObjectName below contains exactly one ordinary filename component.
func validWindowsComponent(part string) error {
	if part == "" || part == "." || part == ".." || strings.ContainsAny(part, ":\\/\x00\"<>|?*") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
		return errors.New("invalid Windows comparison path component")
	}
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(part, ".", 2)[0], " "))
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return errors.New("reserved Windows comparison path component")
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		suffix := base[3:]
		if (len(suffix) == 1 && suffix[0] >= '0' && suffix[0] <= '9') || suffix == "¹" || suffix == "²" || suffix == "³" {
			return errors.New("reserved Windows comparison path component")
		}
	}
	return nil
}

func walkWindowsCwd(cwd string) (windows.Handle, error) {
	if err := validLocalCwd(cwd); err != nil {
		return 0, err
	}
	rootName, err := windows.UTF16PtrFromString(`\\?\` + cwd[:3])
	if err != nil {
		return 0, err
	}
	root, err := windows.CreateFile(rootName, windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE|windows.SYNCHRONIZE, comparisonShare, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, fmt.Errorf("open comparison volume root: %w", err)
	}
	if err := verifyWindowsHandle(root, true); err != nil {
		windows.CloseHandle(root)
		return 0, err
	}
	current := root
	if len(cwd) > 3 {
		for _, part := range strings.Split(cwd[3:], "\\") {
			next, err := openWindowsChild(current, part, true)
			windows.CloseHandle(current)
			if err != nil {
				return 0, fmt.Errorf("open comparison cwd: %w", err)
			}
			current = next
		}
	}
	return current, nil
}

func openWindowsChild(parent windows.Handle, name string, directory bool) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	access := uint32(windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE)
	if directory {
		access |= windows.FILE_TRAVERSE
	} else {
		access |= windows.FILE_READ_DATA
	}
	var handle windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	// A type-neutral native open permits FILE_OPEN_REPARSE_POINT. Type and tag
	// are verified on the returned handle before it is used as a parent.
	err = windows.NtCreateFile(&handle, access, &attributes, &iosb, nil, 0, comparisonShare, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return 0, err
	}
	if err := verifyWindowsHandle(handle, directory); err != nil {
		windows.CloseHandle(handle)
		return 0, err
	}
	return handle, nil
}

func verifyWindowsHandle(handle windows.Handle, directory bool) error {
	var info windowsAttributeTag
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("inspect comparison path: %w", err)
	}
	if info.Attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("comparison path must not contain reparse points")
	}
	if directory {
		if info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return errors.New("comparison path component must be a directory")
		}
	} else if info.Attributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_DEVICE) != 0 {
		return errors.New("comparison input must be a regular file")
	}
	return nil
}

func windowsHandleID(handle windows.Handle) (windowsFileID, error) {
	var id windowsFileID
	err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id)))
	return id, err
}
