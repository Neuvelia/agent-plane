//go:build darwin || linux

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// acquireRegistryLock holds a lock on a stable, separate inode. The registry
// JSON file may be atomically replaced while this handle stays open.
func acquireRegistryLock(stateDir string) (registryLock, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, ErrRegistryLockUnsafe
	}
	stateInfo, err := os.Lstat(stateDir)
	if err != nil {
		return nil, fmt.Errorf("inspect registry state directory: %w", err)
	}
	if !stateInfo.IsDir() || stateInfo.Mode().Perm()&0077 != 0 {
		return nil, ErrRegistryLockUnsafe
	}
	root, err := os.OpenRoot(stateDir)
	if err != nil {
		return nil, fmt.Errorf("open registry state directory: %w", err)
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("pin registry state directory: %w", err)
	}
	defer dir.Close()
	openedDir, err := dir.Stat()
	if err != nil || !os.SameFile(stateInfo, openedDir) {
		return nil, ErrRegistryLockUnsafe
	}
	fd, err := unix.Openat(int(dir.Fd()), registryLockName, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EISDIR) {
			return nil, ErrRegistryLockUnsafe
		}
		return nil, fmt.Errorf("open registry lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(stateDir, registryLockName))
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		file.Close()
		return nil, ErrRegistryLockUnsafe
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrRegistryBusy
		}
		return nil, fmt.Errorf("lock local registry: %w", err)
	}
	current, err := root.Lstat(registryLockName)
	if err != nil || !os.SameFile(info, current) {
		file.Close()
		return nil, ErrRegistryLockUnsafe
	}
	return &unixRegistryLock{file: file, stateDir: stateDir, directoryIdentity: stateInfo, fileIdentity: info}, nil
}

type unixRegistryLock struct {
	mu                sync.Mutex
	file              *os.File
	stateDir          string
	directoryIdentity os.FileInfo
	fileIdentity      os.FileInfo
}

func (l *unixRegistryLock) Verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return ErrRegistryLockUnsafe
	}
	dirInfo, err := os.Lstat(l.stateDir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode().Perm()&0077 != 0 || !os.SameFile(dirInfo, l.directoryIdentity) {
		return ErrRegistryLockUnsafe
	}
	openInfo, err := l.file.Stat()
	if err != nil || !os.SameFile(openInfo, l.fileIdentity) {
		return ErrRegistryLockUnsafe
	}
	pathInfo, err := os.Lstat(filepath.Join(l.stateDir, registryLockName))
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Mode().Perm() != 0600 || !os.SameFile(pathInfo, l.fileIdentity) {
		return ErrRegistryLockUnsafe
	}
	return nil
}

func (l *unixRegistryLock) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
