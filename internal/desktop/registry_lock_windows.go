//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The registry lock is a separate stable file: replacing registry.json must
// never release cross-process exclusion. Both the directory and lock are
// checked by handle and by path before the lock is accepted or used.
func acquireRegistryLock(stateDir string) (registryLock, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, ErrRegistryLockUnsafe
	}
	directoryInfo, err := os.Lstat(stateDir)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return nil, ErrRegistryLockUnsafe
	}
	root, err := os.OpenRoot(stateDir)
	if err != nil {
		return nil, fmt.Errorf("open registry state directory: %w", err)
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("pin registry state directory: %w", err)
	}
	defer directory.Close()
	openedDirectory, err := directory.Stat()
	if err != nil || !os.SameFile(directoryInfo, openedDirectory) || privateWindowsFile(directory, true) != nil {
		return nil, ErrRegistryLockUnsafe
	}
	file, err := root.OpenFile(registryLockName, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open registry lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || privateWindowsFile(file, false) != nil {
		file.Close()
		return nil, ErrRegistryLockUnsafe
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrRegistryBusy
		}
		return nil, fmt.Errorf("lock local registry: %w", err)
	}
	current, err := root.Lstat(registryLockName)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		file.Close()
		return nil, ErrRegistryLockUnsafe
	}
	return &windowsRegistryLock{file: file, stateDir: stateDir, directoryIdentity: directoryInfo, fileIdentity: info}, nil
}

type windowsRegistryLock struct {
	mu                sync.Mutex
	file              *os.File
	stateDir          string
	directoryIdentity os.FileInfo
	fileIdentity      os.FileInfo
}

func (l *windowsRegistryLock) Verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return ErrRegistryLockUnsafe
	}
	directoryInfo, err := os.Lstat(l.stateDir)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(directoryInfo, l.directoryIdentity) {
		return ErrRegistryLockUnsafe
	}
	directory, err := os.Open(l.stateDir)
	if err != nil {
		return ErrRegistryLockUnsafe
	}
	defer directory.Close()
	openedDirectory, err := directory.Stat()
	if err != nil || !os.SameFile(openedDirectory, l.directoryIdentity) || privateWindowsFile(directory, true) != nil {
		return ErrRegistryLockUnsafe
	}
	openedLock, err := l.file.Stat()
	if err != nil || !os.SameFile(openedLock, l.fileIdentity) || privateWindowsFile(l.file, false) != nil {
		return ErrRegistryLockUnsafe
	}
	pathInfo, err := os.Lstat(filepath.Join(l.stateDir, registryLockName))
	if err != nil || !pathInfo.Mode().IsRegular() || !os.SameFile(pathInfo, l.fileIdentity) {
		return ErrRegistryLockUnsafe
	}
	return nil
}

func (l *windowsRegistryLock) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close() // A process death also releases LockFileEx.
	l.file = nil
	return err
}

// A private store allows the current user, LocalSystem and Administrators.
// Unknown ACE formats fail closed instead of assuming mode bits enforce an ACL.
func privateWindowsFile(file *os.File, directory bool) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		(directory && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0) ||
		(!directory && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) {
		return ErrRegistryLockUnsafe
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || owner == nil || !windows.EqualSid(owner, user.User.Sid) {
		return ErrRegistryLockUnsafe
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return ErrRegistryLockUnsafe
	}
	system, err := windows.StringToSid("S-1-5-18")
	if err != nil {
		return err
	}
	admins, err := windows.StringToSid("S-1-5-32-544")
	if err != nil {
		return err
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil || ace == nil {
			return ErrRegistryLockUnsafe
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return ErrRegistryLockUnsafe
		}
		if ace.Header.AceSize < uint16(unsafe.Offsetof(ace.SidStart))+8 {
			return ErrRegistryLockUnsafe
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if 8+4*uint32(sid.SubAuthorityCount()) > uint32(ace.Header.AceSize)-uint32(unsafe.Offsetof(ace.SidStart)) || !sid.IsValid() {
			return ErrRegistryLockUnsafe
		}
		if !windows.EqualSid(sid, user.User.Sid) && !windows.EqualSid(sid, system) && !windows.EqualSid(sid, admins) {
			return ErrRegistryLockUnsafe
		}
	}
	return nil
}
