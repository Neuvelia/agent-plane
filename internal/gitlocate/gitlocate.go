// Package gitlocate finds the git executable the engine may run.
package gitlocate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrGitNotFound means no usable git executable could be resolved from PATH.
var ErrGitNotFound = errors.New("git executable not found")

// ErrDeveloperToolsMissing means PATH resolved to the macOS xcrun shim while no
// Command Line Tools or Xcode Git exists; executing the shim would open the
// "install developer tools" dialog.
var ErrDeveloperToolsMissing = errors.New("git is the macOS developer-tools shim and no developer tools are installed")

// macOS locations used to decide, without executing anything, whether the
// /usr/bin/git shim fronts a real Git.
const (
	DarwinGitShim      = "/usr/bin/git"
	DarwinCLTGit       = "/Library/Developer/CommandLineTools/usr/bin/git"
	DarwinSelectLink   = "/var/db/xcode_select_link"
	darwinXcodeDevel   = "/Applications/Xcode.app/Contents/Developer"
	darwinDevelGitPath = "usr/bin/git"
)

// Locator resolves git. Zero values select the running platform and the real
// filesystem; tests inject the rest.
type Locator struct {
	GOOS     string
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)
	Readlink func(string) (string, error)
	// Shim is the path treated as the macOS xcrun shim; default DarwinGitShim.
	Shim string
}

// ResolveGit locates git on the process PATH with the default Locator.
func ResolveGit() (string, error) { return Locator{}.Resolve() }

// Resolve returns the absolute path of the git executable found on PATH. It
// refuses a relative or current-directory result and, on macOS, the shim
// without developer tools. Nothing is executed.
func (l Locator) Resolve() (string, error) {
	lookPath := l.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath("git")
	if errors.Is(err, exec.ErrDot) {
		return "", fmt.Errorf("%w: a relative or current-directory git (%q) was refused; put an absolute Git directory on PATH", ErrGitNotFound, path)
	}
	if err != nil {
		return "", fmt.Errorf("%w: exec.LookPath(\"git\") searched PATH and found nothing; install Git 2.38 or newer and add it to PATH: %w", ErrGitNotFound, err)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: PATH resolved git to the non-absolute %q, which was refused; put an absolute Git directory on PATH", ErrGitNotFound, path)
	}
	if err := l.Check(path); err != nil {
		return "", err
	}
	return path, nil
}

// Check applies the macOS shim guard to path: on Darwin, the shim may only be
// used when developer tools exist.
func (l Locator) Check(path string) error {
	goos := l.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	shim := l.Shim
	if shim == "" {
		shim = DarwinGitShim
	}
	if goos == "darwin" && path == shim && !l.darwinDeveloperGit() {
		return ErrDeveloperToolsMissing
	}
	return nil
}

func (l Locator) darwinDeveloperGit() bool {
	stat, readlink := l.Stat, l.Readlink
	if stat == nil {
		stat = os.Stat
	}
	if readlink == nil {
		readlink = os.Readlink
	}
	candidates := []string{DarwinCLTGit, darwinXcodeDevel + "/" + darwinDevelGitPath}
	if target, err := readlink(DarwinSelectLink); err == nil && strings.HasPrefix(target, "/") {
		candidates = append(candidates, strings.TrimSuffix(target, "/")+"/"+darwinDevelGitPath)
	}
	for _, c := range candidates {
		if info, err := stat(c); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}
