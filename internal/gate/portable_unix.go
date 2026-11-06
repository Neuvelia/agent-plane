//go:build darwin || linux

package gate

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type unixComparisonRoot struct {
	path     string
	fd       int
	identity unix.Stat_t
}

// openComparisonRoot walks from / with no-follow directory opens and retains
// one directory handle for both inputs.
func openComparisonRoot(cwd string) (comparisonRoot, error) {
	start, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", pathCause(err))
	}
	current := start
	for _, part := range strings.Split(strings.TrimPrefix(cwd, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if current != start {
			unix.Close(current)
		}
		if err != nil {
			unix.Close(start)
			return nil, fmt.Errorf("open comparison cwd: %w", pathCause(err))
		}
		current = next
	}
	if current != start {
		unix.Close(start)
	}
	var identity unix.Stat_t
	if err := unix.Fstat(current, &identity); err != nil {
		unix.Close(current)
		return nil, fmt.Errorf("stat comparison cwd: %w", err)
	}
	root := &unixComparisonRoot{path: cwd, fd: current, identity: identity}
	if err := root.Verify(); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func (r *unixComparisonRoot) Close() error { return unix.Close(r.fd) }

func (r *unixComparisonRoot) Verify() error {
	if !sameDirectoryPath(r.path, r.identity) {
		return errors.New("comparison cwd changed during open")
	}
	return nil
}

func (r *unixComparisonRoot) Open(relative string) (*os.File, error) {
	q, err := normalize(relative)
	if err != nil {
		return nil, err
	}
	if err := r.Verify(); err != nil {
		return nil, err
	}
	parent := r.fd
	parts := strings.Split(q, "/")
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(parent, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if parent != r.fd {
			unix.Close(parent)
		}
		if err != nil {
			return nil, fmt.Errorf("open comparison path: %w", pathCause(err))
		}
		parent = next
	}
	if parent != r.fd {
		defer unix.Close(parent)
	}
	fd, err := unix.Openat(parent, parts[len(parts)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open comparison input: %w", pathCause(err))
	}
	file := os.NewFile(uintptr(fd), "comparison input")
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

func sameDirectoryPath(path string, opened unix.Stat_t) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	current, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(current.Dev) == uint64(opened.Dev) && uint64(current.Ino) == uint64(opened.Ino)
}
