package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

// recorder captures how a guard ends a test instead of ending this one.
type recorder struct {
	testing.TB
	fatal, skip int
}

func (r *recorder) Helper()               {}
func (r *recorder) Fatalf(string, ...any) { r.fatal++ }
func (r *recorder) Skipf(string, ...any)  { r.skip++ }

func TestRequireExecutableFailsOnLinuxAndMacOSAndSkipsElsewhere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent-tool")
	cases := []struct {
		goos        string
		fatal, skip int
	}{{"linux", 1, 0}, {"darwin", 1, 0}, {"windows", 0, 1}, {"freebsd", 0, 1}}
	for _, c := range cases {
		r := &recorder{TB: t}
		requireExecutable(r, c.goos, missing, missing)
		if r.fatal != c.fatal || r.skip != c.skip {
			t.Errorf("%s: fatal %d skip %d, want fatal %d skip %d", c.goos, r.fatal, r.skip, c.fatal, c.skip)
		}
	}
	// An existing regular file passes everywhere.
	present := filepath.Join(t.TempDir(), "tool")
	writeFile(t, present)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		r := &recorder{TB: t}
		requireExecutable(r, goos, present)
		if r.fatal != 0 || r.skip != 0 {
			t.Errorf("%s: present file rejected", goos)
		}
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestOnSharedRunnerFollowsTheCIVariable(t *testing.T) {
	for value, want := range map[string]bool{"": false, "true": true} {
		t.Setenv("CI", value)
		if got := OnSharedRunner(); got != want {
			t.Errorf("CI=%q: OnSharedRunner() = %v, want %v", value, got, want)
		}
	}
}
