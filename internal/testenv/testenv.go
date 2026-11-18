// Package testenv holds test-only guards for platform preconditions. It must
// only be imported from _test.go files.
//
// Native Windows support is not a product decision yet: production code starts
// /usr/bin/git, and the Codex adapter and the command Gate accept only Darwin
// and Linux. Tests that depend on those paths skip with a reason instead of
// failing, so a Windows run reports what still needs a decision.
package testenv

import (
	"os"
	"runtime"
	"testing"
)

// RequireExecutable requires every path to be an existing regular file. Use it
// for absolute Unix tool paths (/usr/bin/git, /bin/sh, ...) that the test or the
// code under test starts directly. A missing tool fails t on Linux and macOS,
// where it is a broken runner rather than a platform gap, so CI cannot stay
// green by skipping; on other platforms t is skipped.
func RequireExecutable(t testing.TB, paths ...string) {
	t.Helper()
	requireExecutable(t, runtime.GOOS, paths...)
}

func requireExecutable(t testing.TB, goos string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
			if goos == "linux" || goos == "darwin" {
				t.Fatalf("%s is not available on this platform", p)
			} else {
				t.Skipf("%s is not available on this platform", p)
			}
			return
		}
	}
}

// RequireDarwinOrLinux skips t on platforms the code under test rejects by
// design; what names the component (for example "the Codex adapter").
func RequireDarwinOrLinux(t testing.TB, what string) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("%s supports only Darwin and Linux, not %s", what, runtime.GOOS)
	}
}

// RequireUnixPermissions skips t where the file system does not report Unix
// permission bits (Windows reports 0666/0444 for files and 0777 for dirs).
func RequireUnixPermissions(t testing.TB) {
	t.Helper()
	if !HasUnixPermissions() {
		t.Skip("Windows does not report Unix permission bits")
	}
}

// HasUnixPermissions reports whether Stat returns meaningful Unix permission
// bits; use it to guard a single assertion instead of skipping the whole test.
func HasUnixPermissions() bool { return runtime.GOOS != "windows" }

// SkipOnWindows skips t with reason on Windows, for fixtures the platform
// cannot create (file names or argv bytes its APIs reject or rewrite).
func SkipOnWindows(t testing.TB, reason string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(reason)
	}
}
