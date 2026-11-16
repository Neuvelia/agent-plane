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

// RequireExecutable skips t unless every path is an existing regular file.
// Use it for absolute Unix tool paths (/usr/bin/git, /bin/sh, ...) that the test
// or the code under test starts directly.
func RequireExecutable(t testing.TB, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
			t.Skipf("%s is not available on this platform", p)
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
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not report Unix permission bits")
	}
}
