package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/agent/cliproc"
)

const probeTimeout = 10 * time.Second

// versionPattern matches `claude --version`.
// ASSUMED(spike): the output is exactly "<semver> (Claude Code)"; the plan
// recorded it against 2.1.332 only.
var versionPattern = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+\S*) \(Claude Code\)\s*$`)

// requiredFlags must all appear in `claude --help`. Detection checks flag
// names only, never record shapes, so protocol drift is caught at run time by
// the required structured result.
// ASSUMED(spike): every name below exists in `claude --help` of the supported CLI.
var requiredFlags = []string{
	"--print", "--output-format", "--verbose", "--input-format", "--json-schema",
	"--no-session-persistence", "--permission-mode", "--permission-prompts", "--tools",
	"--strict-mcp-config", "--disable-slash-commands", "--bare",
}

// Detect inspects the configured binary with bounded probes. A missing binary
// is reported as unavailable; an unusable one fails closed.
func (a Adapter) Detect(ctx context.Context) (agent.CapabilityReport, error) {
	supported := true
	r := agent.CapabilityReport{DisplayName: "Claude Code CLI", CorrectionFeedback: &supported}
	present, err := a.binaryState()
	if err != nil || !present {
		return r, err
	}
	// Probes run exactly like Execute: a private HOME outside any workspace is
	// both the working directory and HOME, so nothing a probe writes can reach
	// the caller's directory or the real user configuration.
	home, err := newPrivateHome("")
	if err != nil {
		return r, err
	}
	match, err := a.probeCapabilities(ctx, home)
	// The probe environment carries no credentials, so a failed removal leaves
	// nothing sensitive behind and must not make detection fail.
	_ = removePrivate(home)
	if err != nil {
		return r, err
	}
	yes, no := true, false
	r.Available = true
	r.Version = &match[1]
	r.Cancellation = &yes
	r.Streaming = &yes
	r.Workspace = &yes
	r.UsageReporting = &no
	return r, nil
}

// probeCapabilities runs the version and help probes in home and returns the
// version match.
func (a Adapter) probeCapabilities(ctx context.Context, home string) ([]string, error) {
	out, err := a.probe(ctx, home, "--version")
	if err != nil {
		return nil, err
	}
	match := versionPattern.FindStringSubmatch(out)
	if match == nil {
		return nil, fail("detection", "Claude Code version response is unsupported", nil)
	}
	help, err := a.probe(ctx, home, "--help")
	if err != nil {
		return nil, err
	}
	if !hasFlags(help, requiredFlags) {
		return nil, fail("unsupported_capability", "Claude Code lacks required flags", nil)
	}
	return match, nil
}

// binaryState reports whether the configured binary exists. It fails closed on
// an unsupported platform, a relative path or a non-executable file.
func (a Adapter) binaryState() (bool, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return false, fail("unsupported_platform", "process ownership requires Darwin or Linux", nil)
	}
	if !filepath.IsAbs(a.Binary) {
		return false, fail("detection", "configure an absolute --claude-bin path", nil)
	}
	info, err := os.Stat(a.Binary)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return false, fail("detection", "Claude Code binary must be an executable file", err)
	}
	return true, nil
}

// probe runs one bounded capability call in the private home with only the
// fixed adapter environment (no PATH, no ambient variables).
func (a Adapter) probe(ctx context.Context, home string, args ...string) (string, error) {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	b := cliproc.NewLimitedBuffer(maxRecord)
	err := runProcess(pctx, cliproc.Spec{Binary: a.Binary, Args: args, Env: adapterEnvironment(home), Dir: home, Stdout: b, Policy: a.Termination})
	if err != nil || b.Overflow() {
		return "", fail("detection", "Claude Code capability probe failed or exceeded bounds; script-based installs (for example npm shims using #!/usr/bin/env node) cannot run with the minimal adapter environment, so configure a standalone binary", err)
	}
	return b.String(), nil
}

func hasFlags(help string, flags []string) bool {
	present := map[string]bool{}
	for _, word := range strings.Fields(help) {
		present[strings.Trim(word, ",")] = true
	}
	for _, flag := range flags {
		if !present[flag] {
			return false
		}
	}
	return true
}
