package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/gitstate"
	"agentplane/internal/testenv"
)

func gitScopeRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	gitScope(t, d, "init", "-q")
	for _, p := range []string{"src/a.go", "src/sub/b.go", "src2/c.go", "old.go"} {
		full := filepath.Join(d, p)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("base"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitScope(t, d, "add", ".")
	gitScope(t, d, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	return d
}
func gitScope(t *testing.T, dir string, args ...string) {
	t.Helper()
	testenv.RequireExecutable(t, "/usr/bin/git")
	c := exec.Command("/usr/bin/git", args...)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, b)
	}
}
func captureScope(t *testing.T, dir string) gitstate.Baseline {
	t.Helper()
	b, err := gitstate.Capture(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func checkScope(t *testing.T, b gitstate.Baseline, scope []string, mode string) events.CheckResult {
	t.Helper()
	p, err := CheckChangedFiles(context.Background(), b, scope, mode, "scope")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return p
}
func pathsOf(p events.CheckResult) []string {
	var paths []string
	for _, e := range p.Evidence {
		if e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	return paths
}
func TestChangedFilesScopeGitClassesAndMatching(t *testing.T) {
	dir := gitScopeRepo(t)
	if p := checkScope(t, captureScope(t, dir), []string{"src/**"}, "deny"); p.Verdict != events.CheckGreen || len(pathsOf(p)) != 0 {
		t.Fatalf("empty: %+v", p)
	}
	b := captureScope(t, dir)
	for _, p := range []string{"src/a.go", "src/sub/b.go"} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("edited"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if p := checkScope(t, b, []string{"./src/**", "src/", "src/a.go"}, "deny"); p.Verdict != events.CheckGreen {
		t.Fatalf("allowed: %+v", p)
	}
	if err := os.WriteFile(filepath.Join(dir, "src2/c.go"), []byte("forbidden"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z-untracked"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "old.go")); err != nil {
		t.Fatal(err)
	}
	gitScope(t, dir, "add", "src2/c.go")
	p := checkScope(t, b, []string{"src/**"}, "deny")
	if p.Verdict != events.CheckRed || p.Reason != "changed_files_outside_scope" || !reflect.DeepEqual(pathsOf(p), []string{"old.go", "src2/c.go", "z-untracked"}) {
		t.Fatalf("mixed: %+v", p)
	}
	if p.ChangedFiles.Mode != "deny" || !reflect.DeepEqual(p.ChangedFiles.AllowedPaths, []string{"src/"}) || p.ChangedFiles.ObservedCount != 5 {
		t.Fatalf("details: %+v", p.ChangedFiles)
	}
	warn := checkScope(t, b, []string{"src/**"}, "warn")
	if warn.Verdict != events.CheckGreen || warn.Reason != p.Reason || !reflect.DeepEqual(pathsOf(warn), pathsOf(p)) {
		t.Fatalf("warn: %+v", warn)
	}
}
func TestChangedFilesScopeRenameChecksBothPaths(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		want           []string
	}{
		{"allowed to allowed", "src/a.go", "src/new.go", nil},
		{"allowed to forbidden", "src/a.go", "moved.go", []string{"moved.go"}},
		{"forbidden to allowed", "old.go", "src/new.go", []string{"old.go"}},
		{"forbidden to forbidden", "old.go", "moved.go", []string{"moved.go", "old.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := gitScopeRepo(t)
			b := captureScope(t, dir)
			if err := os.Rename(filepath.Join(dir, tc.from), filepath.Join(dir, tc.to)); err != nil {
				t.Fatal(err)
			}
			p := checkScope(t, b, []string{"src/**"}, "deny")
			if !reflect.DeepEqual(pathsOf(p), tc.want) {
				t.Fatalf("got %v want %v", pathsOf(p), tc.want)
			}
		})
	}
}
func TestChangedFilesScopeErrorsAndPreexistingDirt(t *testing.T) {
	dir := gitScopeRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "old.go"), []byte("preexisting"), 0600); err != nil {
		t.Fatal(err)
	}
	b := captureScope(t, dir)
	if p := checkScope(t, b, []string{}, "deny"); p.Verdict != events.CheckGreen {
		t.Fatalf("preexisting dirt: %+v", p)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.go"), []byte("changed further"), 0600); err != nil {
		t.Fatal(err)
	}
	if p := checkScope(t, b, []string{}, "deny"); !reflect.DeepEqual(pathsOf(p), []string{"old.go"}) {
		t.Fatalf("further edit: %+v", p)
	}
	for _, bad := range []string{"../src", "/src", "src/../other", "C:/src", "src\\file", "src/*.go", "src/**/more"} {
		if _, err := CheckChangedFiles(context.Background(), b, []string{bad}, "deny", "scope"); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := gitstate.Capture(context.Background(), t.TempDir()); err == nil {
		t.Fatal("non-Git accepted")
	}
	if _, err := CheckChangedFiles(context.Background(), gitstate.Baseline{}, []string{}, "deny", "scope"); err == nil {
		t.Fatal("invalid baseline passed deny")
	}
	if p := checkScope(t, gitstate.Baseline{}, []string{}, "warn"); p.Reason != "changed_files_scope_indeterminate" || p.Verdict != events.CheckGreen {
		t.Fatalf("warn failure: %+v", p)
	}
}

func TestChangedFilesNewlyIgnoredPathCannotPassDeny(t *testing.T) {
	dir := gitScopeRepo(t)
	b := captureScope(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("hidden.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hidden.txt"), []byte("hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	if p := checkScope(t, b, []string{".gitignore"}, "deny"); p.Verdict != events.CheckRed || !reflect.DeepEqual(pathsOf(p), []string{"hidden.txt"}) {
		t.Fatalf("newly hidden file escaped deny: %+v", p)
	}
	if p := checkScope(t, b, []string{".gitignore"}, "warn"); p.Verdict != events.CheckGreen || !reflect.DeepEqual(pathsOf(p), []string{"hidden.txt"}) {
		t.Fatalf("warn=%+v", p)
	}
}

func TestChangedFilesPreviouslyIgnoredPathIsChecked(t *testing.T) {
	dir := gitScopeRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "ignored"), 0700); err != nil {
		t.Fatal(err)
	}
	preexisting := filepath.Join(dir, "ignored", "existing.txt")
	if err := os.WriteFile(preexisting, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	b := captureScope(t, dir)
	if p := checkScope(t, b, []string{"src/**"}, "deny"); p.Verdict != events.CheckGreen {
		t.Fatalf("unchanged ignored file attributed to agent: %+v", p)
	}
	if err := os.WriteFile(preexisting, []byte("edited"), 0600); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, "ignored", "created.txt")
	if err := os.WriteFile(created, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	p := checkScope(t, b, []string{"src/**"}, "deny")
	if p.Verdict != events.CheckRed || !reflect.DeepEqual(pathsOf(p), []string{"ignored/created.txt", "ignored/existing.txt"}) {
		t.Fatalf("ignored changes escaped scope: %+v", p)
	}
}

func TestChangedFilesLongPathKeepsWarnNonblocking(t *testing.T) {
	dir := gitScopeRepo(t)
	b := captureScope(t, dir)
	long := filepath.Join(strings.Repeat("a", 200), strings.Repeat("b", 200), strings.Repeat("c", 200))
	full := filepath.Join(dir, long)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"warn", "deny"} {
		p := checkScope(t, b, []string{}, mode)
		if p.Reason != "changed_files_outside_scope" || !reflect.DeepEqual(pathsOf(p), []string{filepath.ToSlash(long)}) {
			t.Fatalf("%s: %+v", mode, p)
		}
		if mode == "warn" && p.Verdict != events.CheckGreen || mode == "deny" && p.Verdict != events.CheckRed {
			t.Fatalf("%s: %+v", mode, p)
		}
	}
}
