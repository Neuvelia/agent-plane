package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

// roadmap: F1.2-AC1
func TestCLIExplicitCodexAdapterAgentAndShell(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		testenv.RequireExecutable(t, "/usr/bin/git")
		c := exec.Command("/usr/bin/git", args...)
		c.Dir = dir
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	binary := filepath.Join(t.TempDir(), "codex")
	os.WriteFile(binary, []byte(`#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --cd) shift;work=$1;; --output-last-message) shift;result=$1;; esac;shift;done
cd "$work" || exit 1
cat >/dev/null
printf edited >actual.txt
printf '{"summary":"done","artifacts":[]}' >"$result"
`), 0700)
	path := filepath.Join(t.TempDir(), "graph.yaml")
	os.WriteFile(path, []byte("schema_version: 0\nid: codex-cli-test\ninputs: {}\nnodes:\n  agent:\n    type: agent\n    config:\n      adapter: codex\n      workspace: "+dir+"\n      task: edit\n    outputs:\n      summary: {type: string, required: true}\n  shell:\n    type: shell\n    config:\n      command: /usr/bin/printf\n      args: [shell]\n    outputs:\n      stdout: {type: string}\nedges: []\noutputs: {}\n"), 0600)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(), []string{"run", path, "--adapter", "codex", "--codex-bin", binary, "--json"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"type":"node_evidence"`) || !strings.Contains(stdout.String(), `"path":"actual.txt"`) {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, &stderr, &stdout)
	}
}
func TestCLIAdapterSelectionAndNodeScopedFailures(t *testing.T) {
	var out, errout bytes.Buffer
	for _, args := range [][]string{{"run", "unused", "--adapter", "other", "--json"}, {"run", "unused", "--adapter", "codex", "--codex-bin", "relative", "--json"}, {"run", "unused", "--codex-bin", "/absolute", "--json"}} {
		out.Reset()
		errout.Reset()
		if code := runCommand(context.Background(), args, &out, &errout); code != exitUsage {
			t.Fatalf("args=%v code=%d %s", args, code, &out)
		}
	}
	testenv.RequireDarwinOrLinux(t, "the Codex adapter")
	path := filepath.Join(t.TempDir(), "missing.yaml")
	os.WriteFile(path, []byte("schema_version: 0\nid: missing-codex\ninputs: {}\nnodes:\n  work:\n    type: agent\n    config:\n      adapter: codex\n      workspace: /tmp\n      task: edit\n    outputs:\n      summary: {type: string}\nedges: []\noutputs: {}\n"), 0600)
	out.Reset()
	errout.Reset()
	code := runCommand(context.Background(), []string{"run", path, "--adapter", "codex", "--codex-bin", filepath.Join(t.TempDir(), "missing"), "--json"}, &out, &errout)
	if code != exitRun || !strings.Contains(out.String(), `agent node \"work\"`) || strings.Contains(out.String(), `"type":"node_evidence"`) || !strings.Contains(out.String(), `"diagnostic_codes":["agent_unavailable"]`) {
		t.Fatalf("code=%d %s %s", code, &out, &errout)
	}
}
