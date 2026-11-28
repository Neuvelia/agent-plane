package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"agentplane/internal/capability"
	"agentplane/internal/testenv"
)

// fakeClaudeHelp lists the flags Claude Code detection requires. ASSUMED(spike):
// the real `claude --help` text differs; detection checks flag names only.
const fakeClaudeHelp = "--print --output-format --verbose --input-format --json-schema --no-session-persistence --permission-mode --permission-prompts --tools --strict-mcp-config --disable-slash-commands --bare"

// claudeFixture is a Git workspace, an Agent+Shell graph and a fake POSIX
// Claude Code CLI that edits actual.txt and reports a structured result.
func claudeFixture(t *testing.T) (graphPath, binary string) {
	t.Helper()
	testenv.RequireDarwinOrLinux(t, "the Claude Code adapter")
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		testenv.RequireExecutable(t, "/usr/bin/git")
		c := exec.Command("/usr/bin/git", args...)
		c.Dir = dir
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	binary = filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\ncase \"$*\" in\n *--version*) echo '2.1.332 (Claude Code)'; exit;;\n *--help*) echo '" + fakeClaudeHelp + "'; exit;;\nesac\ncat >/dev/null\nprintf edited >actual.txt\n" +
		`printf '%s\n' '{"type":"system","subtype":"init"}'` + "\n" +
		`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"structured_output":{"summary":"done","artifacts":[]}}'` + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	graphPath = filepath.Join(t.TempDir(), "graph.yaml")
	graphText := "schema_version: 0\nid: claude-cli-test\ninputs: {}\nnodes:\n  agent:\n    type: agent\n    config:\n      adapter: claude\n      workspace: " + dir + "\n      task: edit\n    outputs:\n      summary: {type: string, required: true}\n  shell:\n    type: shell\n    config:\n      command: /usr/bin/printf\n      args: [shell]\n    outputs:\n      stdout: {type: string}\nedges: []\noutputs: {}\n"
	if err := os.WriteFile(graphPath, []byte(graphText), 0o600); err != nil {
		t.Fatal(err)
	}
	return graphPath, binary
}

func assertClaudeRunSucceeded(t *testing.T, code int, stdout, stderr *bytes.Buffer) {
	t.Helper()
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"type":"node_evidence"`) || !strings.Contains(stdout.String(), `"path":"actual.txt"`) || !strings.Contains(stdout.String(), `"adapter_id":"claude"`) {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
}

// roadmap: F1.2b-AC1
func TestCLIExplicitClaudeAdapterAgentAndShell(t *testing.T) {
	path, binary := claudeFixture(t)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(), []string{"run", path, "--adapter", "claude", "--claude-bin", binary, "--json"}, &stdout, &stderr)
	assertClaudeRunSucceeded(t, code, &stdout, &stderr)
}

func TestCLIRepeatedAdapterRegistersBothAdapters(t *testing.T) {
	path, binary := claudeFixture(t)
	absentCodex := filepath.Join(t.TempDir(), "codex")
	var stdout, stderr bytes.Buffer
	args := []string{"run", path, "--adapter", "codex", "--codex-bin", absentCodex, "--adapter", "claude", "--claude-bin", binary, "--json"}
	code := runCommand(context.Background(), args, &stdout, &stderr)
	assertClaudeRunSucceeded(t, code, &stdout, &stderr)
}

func TestAdapterFlagsBuildOneAdapterPerChoice(t *testing.T) {
	var choice adapterFlags
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	choice.register(fs)
	if err := fs.Parse([]string{"--adapter", "codex", "--codex-bin", "/opt/codex", "--adapter", "claude", "--claude-bin", "/opt/claude"}); err != nil {
		t.Fatal(err)
	}
	if err := choice.validate(); err != nil {
		t.Fatal(err)
	}
	adapters := choice.adapters()
	if len(adapters) != 2 || adapters["codex"].ID() != "codex" || adapters["claude"].ID() != "claude" {
		t.Fatalf("adapters %v", adapters)
	}
	var none adapterFlags
	if none.adapters() != nil {
		t.Fatal("no --adapter must register no adapter")
	}
}

func TestCLIAdapterFlagValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--adapter", "other"},
		{"--adapter", "claude"},
		{"--adapter", "claude", "--claude-bin", "relative"},
		{"--claude-bin", "/absolute"},
		{"--adapter", "claude", "--claude-bin", "/absolute", "--adapter", "claude"},
		{"--adapter", "claude", "--claude-bin", "/absolute", "--codex-bin", "/absolute"},
		{"--adapter", "codex", "--adapter", "claude", "--codex-bin", "/absolute"},
	} {
		var out, errout bytes.Buffer
		full := append([]string{"run", "unused"}, args...)
		full = append(full, "--json")
		if code := runCommand(context.Background(), full, &out, &errout); code != exitUsage {
			t.Errorf("args=%v code=%d %s", args, code, &out)
		}
	}
}

func TestCLIMissingClaudeBinaryIsNodeScopedAndNamesTheBinary(t *testing.T) {
	testenv.RequireDarwinOrLinux(t, "the Claude Code adapter")
	path, _ := claudeFixture(t)
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), []string{"run", path, "--adapter", "claude", "--claude-bin", filepath.Join(t.TempDir(), "missing"), "--json"}, &out, &errout)
	if code != exitRun || !strings.Contains(out.String(), `"diagnostic_codes":["agent_unavailable"]`) || !strings.Contains(out.String(), "--claude-bin is installed and runs") || strings.Contains(out.String(), `"type":"node_evidence"`) {
		t.Fatalf("code=%d %s %s", code, &out, &errout)
	}
}

func TestClaudeRemediesNameTheEntryPointFlags(t *testing.T) {
	if got := cliAgentRemedy("claude"); !strings.Contains(got, "--adapter claude --claude-bin ABSOLUTE_PATH") {
		t.Fatalf("run remedy = %q", got)
	}
	if got := cliAgentRemedy("codex"); got != "run with --adapter codex --codex-bin ABSOLUTE_PATH of the installed Codex CLI" {
		t.Fatalf("codex run remedy changed: %q", got)
	}
	if got := registeredAgentRemedy("claude"); strings.Contains(got, "--adapter") || !strings.Contains(got, "--claude-bin") {
		t.Fatalf("registered claude remedy = %q", got)
	}
	if got := serveAgentRemedy("claude"); !strings.Contains(got, "desktop engine") || !strings.Contains(got, "agentplane run --adapter claude --claude-bin ABSOLUTE_PATH") {
		t.Fatalf("serve remedy = %q", got)
	}
}

func TestServeRejectsARelativeClaudeBinBeforeAnnouncing(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runCommand(context.Background(), []string{"serve", "--store-dir", t.TempDir(), "--claude-bin", "claude"}, &out, &errOut)
	if code != exitUsage || out.Len() != 0 || !strings.Contains(errOut.String(), "--claude-bin must be an absolute path") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

func TestServeCapabilityClaudeBecomesAnAdapterRowWhenConfigured(t *testing.T) {
	r := capabilityDetector("", filepath.Join(t.TempDir(), "claude")).Detect(context.Background())
	var claude []capability.Tool
	for _, tool := range r.Tools {
		if tool.ID == "claude" {
			claude = append(claude, tool)
		}
	}
	posix := goruntime.GOOS == "linux" || goruntime.GOOS == "darwin"
	want := map[bool]capability.Status{true: capability.StatusMissing, false: capability.StatusUnsupportedPlatform}[posix]
	if len(claude) != 1 || claude[0].Kind != "agent_adapter" || claude[0].Status != want {
		t.Fatalf("claude rows %+v", claude)
	}
}
