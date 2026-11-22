package gitstate

import (
	"os"
	"path/filepath"
	"runtime"
)

// Environment is the controlled environment for a Git child process. Nothing
// is inherited. On Windows Git also needs SystemRoot, TEMP/TMP and its own
// directory on PATH; "/dev/null" config paths rely on Git for Windows mapping
// them to NUL, which is not yet verified on a Windows runner.
func Environment(gitPath string) []string {
	path := "PATH=/usr/bin:/bin"
	env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	if runtime.GOOS == "windows" {
		path = "PATH=" + filepath.Dir(gitPath)
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"), "TEMP="+os.Getenv("TEMP"), "TMP="+os.Getenv("TMP"))
	}
	return append([]string{path}, env...)
}
