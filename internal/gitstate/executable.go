package gitstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type executableKey struct{}
type supervisorKey struct{}

// WithExecutable binds the selected absolute Git program to one inspection
// context. A desktop Start sets it once from its validated, fingerprinted
// setting; callers without a selection retain the Unix CLI default.
func WithExecutable(ctx context.Context, path string) (context.Context, error) {
	if ctx == nil || !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("Git executable must be an absolute path")
	}
	return context.WithValue(ctx, executableKey{}, filepath.Clean(path)), nil
}

// WithSupervisor binds the fixed bundled helper to Git inspections in one
// desktop run. The CLI omits it and retains its direct execution contract.
func WithSupervisor(ctx context.Context, path string) (context.Context, error) {
	if ctx == nil || !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("Git supervisor must be an absolute path")
	}
	return context.WithValue(ctx, supervisorKey{}, filepath.Clean(path)), nil
}

func Supervisor(ctx context.Context) string {
	if ctx != nil {
		if selected, ok := ctx.Value(supervisorKey{}).(string); ok {
			return selected
		}
	}
	return ""
}

// Executable returns an empty path on Windows unless a caller selected one.
// PATH lookup would make desktop preflight and execution disagree.
func Executable(ctx context.Context) string {
	if ctx != nil {
		if selected, ok := ctx.Value(executableKey{}).(string); ok {
			return selected
		}
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	return "/usr/bin/git"
}

// InspectionEnvironment prevents ambient Git configuration from changing
// read-only evidence. The selected executable is absolute, so PATH is needed
// only for existing Unix Git compatibility and is never used to select Git.
func InspectionEnvironment() []string {
	env := []string{"LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"}
	if runtime.GOOS != "windows" {
		env = append(env, "PATH=/usr/bin:/bin")
	}
	return env
}
