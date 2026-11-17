package gitstate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrGitNotFound means no git executable could be resolved from PATH.
var ErrGitNotFound = errors.New("git executable not found")

// ResolveGit returns the absolute path of the git executable found on PATH.
// It is a pure lookup: nothing is executed.
func ResolveGit() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("%w: exec.LookPath(\"git\") searched PATH and found nothing; install Git 2.38 or newer and add it to PATH: %w", ErrGitNotFound, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("making git path %q absolute: %w", path, err)
	}
	return abs, nil
}

// Environment is the controlled environment for a Git child process. Nothing
// is inherited; on Windows Git additionally needs SystemRoot and its own
// directory on PATH to start.
func Environment(gitPath string) []string {
	path := "PATH=/usr/bin:/bin"
	env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	if runtime.GOOS == "windows" {
		path = "PATH=" + filepath.Dir(gitPath)
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return append([]string{path}, env...)
}
