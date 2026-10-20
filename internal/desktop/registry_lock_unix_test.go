//go:build darwin || linux

package desktop

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func privateRegistryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRegistryLockContendsAcrossStateReplacement(t *testing.T) {
	dir := privateRegistryDir(t)
	first, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	current := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(current, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "registry.tmp")
	if err := os.WriteFile(replacement, []byte(`{"version":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, current); err != nil {
		t.Fatal(err)
	}
	if second, err := acquireRegistryLock(dir); !errors.Is(err, ErrRegistryBusy) || second != nil {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second lock after JSON replacement = %v, %v", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryLockRejectsSymlinkAndInsecureMode(t *testing.T) {
	dir := privateRegistryDir(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, registryLockName)
	if err := os.Symlink(outside, lockPath); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireRegistryLock(dir); !errors.Is(err, ErrRegistryLockUnsafe) || lock != nil {
		t.Fatalf("symlink lock = %v, %v", lock, err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireRegistryLock(dir); !errors.Is(err, ErrRegistryLockUnsafe) || lock != nil {
		t.Fatalf("insecure lock = %v, %v", lock, err)
	}
}

func TestRegistryLockRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := privateRegistryDir(t)
	if err := unix.Mkfifo(filepath.Join(dir, registryLockName), 0600); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireRegistryLock(dir); !errors.Is(err, ErrRegistryLockUnsafe) || lock != nil {
		t.Fatalf("FIFO lock = %v, %v", lock, err)
	}
}

func TestRegistryLockDetectsRecreatedPath(t *testing.T) {
	dir := privateRegistryDir(t)
	first, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, registryLockName)); err != nil {
		t.Fatal(err)
	}
	second, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatalf("replacement inode lock: %v", err)
	}
	defer second.Close()
	if err := first.Verify(); !errors.Is(err, ErrRegistryLockUnsafe) {
		t.Fatalf("original lock verification = %v", err)
	}
	if err := second.Verify(); err != nil {
		t.Fatalf("replacement lock verification = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Verify(); err != nil {
		t.Fatalf("replacement after old close = %v", err)
	}
}

func TestRegistryLockRejectsInsecureOrLinkedStateDirectory(t *testing.T) {
	dir := privateRegistryDir(t)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireRegistryLock(dir); !errors.Is(err, ErrRegistryLockUnsafe) || lock != nil {
		t.Fatalf("insecure directory = %v, %v", lock, err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "linked-state")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireRegistryLock(alias); !errors.Is(err, ErrRegistryLockUnsafe) || lock != nil {
		t.Fatalf("linked directory = %v, %v", lock, err)
	}
}

func TestRegistryLockReleasesAfterProcessDeath(t *testing.T) {
	dir := privateRegistryDir(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestRegistryLockCrashChild$")
	cmd.Env = append(os.Environ(), "AGENTPLANE_LOCK_CHILD_DIR="+dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "locked" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("child readiness = %q, %v", line, err)
	}
	if lock, err := acquireRegistryLock(dir); !errors.Is(err, ErrRegistryBusy) || lock != nil {
		if lock != nil {
			lock.Close()
		}
		t.Fatalf("lock while child alive = %v, %v", lock, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	lock, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatalf("lock after child death: %v", err)
	}
	_ = lock.Close()
}

func TestRegistryLockCrashChild(t *testing.T) {
	dir := os.Getenv("AGENTPLANE_LOCK_CHILD_DIR")
	if dir == "" {
		return
	}
	lock, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := fmt.Fprintln(os.Stdout, "locked"); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}
