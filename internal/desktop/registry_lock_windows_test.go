//go:build windows

package desktop

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateWindowsTestDir(t *testing.T) string {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;GA;;;SY)(A;OICI;GA;;;BA)(A;OICI;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "private")
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.CreateDirectory(ptr, &attrs); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestRegistryLockExclusiveAndDetectsReplacement(t *testing.T) {
	dir := privateWindowsTestDir(t)
	first, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := acquireRegistryLock(dir); second != nil || !errors.Is(err, ErrRegistryBusy) {
		t.Fatalf("second lock = %v, %v", second, err)
	}
	if err := first.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, registryLockName)); err != nil {
		t.Fatal(err)
	}
	if err := first.Verify(); !errors.Is(err, ErrRegistryLockUnsafe) {
		t.Fatalf("recreated lock path: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal(err)
	}
	if third, err := acquireRegistryLock(dir); third != nil || err == nil {
		t.Fatalf("moved directory accepted: %v, %v", third, err)
	}
}

func TestRegistryLockRejectsBroadACLAndReparsePoint(t *testing.T) {
	dir := privateWindowsTestDir(t)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, dir+"-link"); err == nil {
		if lock, err := acquireRegistryLock(dir + "-link"); lock != nil || !errors.Is(err, ErrRegistryLockUnsafe) {
			t.Fatalf("linked state directory accepted: %v, %v", lock, err)
		}
	} else if os.Getenv("AGENTPLANE_REQUIRE_REPARSE_TEST") == "1" {
		t.Fatalf("required directory reparse fixture could not be created: %v", err)
	} else {
		t.Logf("directory reparse assertion unavailable on this host: %v", err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;WD)(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireRegistryLock(dir); lock != nil || !errors.Is(err, ErrRegistryLockUnsafe) {
		t.Fatalf("broad ACL accepted: %v, %v", lock, err)
	}
}

func TestRegistryLockReleasesAfterWindowsProcessDeath(t *testing.T) {
	dir := privateWindowsTestDir(t)
	child := exec.Command(os.Args[0], "-test.run=^TestWindowsRegistryLockCrashChild$")
	child.Env = append(os.Environ(), "AGENTPLANE_REGISTRY_LOCK_CRASH_DIR="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("child lock barrier: %q %v", line, err)
	}
	if lock, err := acquireRegistryLock(dir); lock != nil || !errors.Is(err, ErrRegistryBusy) {
		t.Fatalf("lock did not contend with child: %v, %v", lock, err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	_ = stdin.Close()
	lock, err := acquireRegistryLock(dir)
	if err != nil {
		t.Fatalf("lock did not release after process death: %v", err)
	}
	lock.Close()
}

func TestWindowsRegistryLockCrashChild(t *testing.T) {
	dir := os.Getenv("AGENTPLANE_REGISTRY_LOCK_CRASH_DIR")
	if dir == "" {
		return
	}
	lock, err := acquireRegistryLock(dir)
	if err != nil {
		os.Exit(90)
	}
	defer lock.Close()
	_, _ = os.Stdout.WriteString("ready\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
}
