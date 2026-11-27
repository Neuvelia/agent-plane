package claudecode

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// arguments is the exact argv of one invocation; the task never appears here.
// Bash is deliberately absent from --tools (plan D4, fallback a): the CLI has
// no OS sandbox by default, so only workspace file tools are enabled until the
// spike confirms a sandbox. No bypass, dangerous-skip or background flag may
// ever be added.
// ASSUMED(spike): every flag, its value and the semantics of `--bare`
// (no hooks, CLAUDE.md, plugins, keychain; auth only through ANTHROPIC_API_KEY)
// and `--permission-prompts none` combined with `acceptEdits` (whatever would
// prompt is denied).
func arguments() []string {
	return []string{
		"-p",
		"--output-format", "stream-json", "--verbose", "--input-format", "text",
		"--json-schema", resultSchema,
		"--no-session-persistence",
		"--permission-mode", "acceptEdits", "--permission-prompts", "none",
		"--tools", "Read,Edit,Write,Glob,Grep",
		"--strict-mcp-config", "--disable-slash-commands",
		"--bare",
	}
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnvironment names are owned by the adapter; environment_refs may not
// override them.
var reservedEnvironment = map[string]bool{
	"HOME": true, "CLAUDE_CONFIG_DIR": true,
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": true, "DISABLE_AUTOUPDATER": true,
}

// environment resolves environment_refs, the only caller-controlled source of
// child variables.
func environment(refs []string) ([]string, error) {
	env := make([]string, 0, len(refs)+4)
	seen := map[string]bool{}
	for _, name := range refs {
		if !envName.MatchString(name) || reservedEnvironment[name] {
			return nil, fail("environment", "environment_refs must contain valid, non-reserved variable names", nil)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		value, ok := os.LookupEnv(name)
		if !ok || strings.ContainsRune(value, 0) {
			return nil, fail("environment", "referenced environment variable is missing or invalid", nil)
		}
		env = append(env, name+"="+value)
	}
	return env, nil
}

// adapterEnvironment is the fixed adapter-owned part of the environment: a
// private HOME and config directory (plan D5) and telemetry/updater switches.
// ASSUMED(spike): these variable names are honored by the CLI, and a private
// HOME keeps every config write out of the workspace.
func adapterEnvironment(home string) []string {
	return []string{
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_AUTOUPDATER=1",
		"HOME=" + home,
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, "config"),
	}
}

// newPrivateHome creates a private temporary HOME outside the workspace. The
// caller removes it after the run. An empty workspace skips the containment
// check (capability probes have no workspace).
func newPrivateHome(workspace string) (string, error) {
	dir, err := os.MkdirTemp("", "agentplane-claude-")
	if err != nil {
		return "", fail("start", "cannot create private home directory", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || (workspace != "" && within(workspace, resolved)) {
		_ = os.RemoveAll(dir)
		return "", fail("environment", "private home directory must be outside the workspace", err)
	}
	if err = os.Mkdir(filepath.Join(resolved, "config"), 0700); err != nil {
		_ = os.RemoveAll(dir)
		return "", fail("start", "cannot create private config directory", err)
	}
	return resolved, nil
}

// removePrivate deletes a private home created by newPrivateHome. The message
// never carries the path or the underlying error text.
func removePrivate(home string) error {
	if err := os.RemoveAll(home); err != nil {
		return fail("environment", "cannot remove private home directory", err)
	}
	return nil
}

// within reports whether path equals root or lies below it, lexically.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
