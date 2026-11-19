package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/testenv"
)

const diffSecret = "diff-secret-VALUE-4711"

// diffRepo is a committed repository plus a fake Codex that writes the secret
// into src/a.go and a second file, then reports a summary.
func diffRepo(t *testing.T) (graphPath, codexBin, workspace string) {
	t.Helper()
	testenv.RequireDarwinOrLinux(t, "the Codex adapter")
	testenv.RequireExecutable(t, "/usr/bin/git")
	workspace = t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		c := exec.Command("/usr/bin/git", args...)
		c.Dir = workspace
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	codexBin = filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --cd) shift;work=$1;; --output-last-message) shift;result=$1;; esac;shift;done
cd "$work" || exit 1
cat >/dev/null
mkdir -p src
printf 'token=%s\nsecond line\n' >src/a.go
printf 'plain\n' >src/b.go
printf '{"summary":"candidate","artifacts":[]}' >"$result"
`, diffSecret)
	if err := os.WriteFile(codexBin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	graphPath = filepath.Join(t.TempDir(), "graph.yaml")
	doc := fmt.Sprintf(`schema_version: 0
id: diffs
units: [work]
inputs: {}
nodes:
  work:
    type: agent
    config: {adapter: codex, workspace: %q, task: edit, allowed_paths: ["src/**"]}
    outputs: {summary: {type: string, required: true}}
    loop:
      max_attempts: 1
      gate:
        inputs: {candidate: {type: string, required: true}}
        config: {check_id: scope, kind: changed_files_scope, input: candidate, mode: deny, workspace: %q, allowed_paths: ["src/**"]}
      bindings:
        - {from: candidate.outputs.summary, to: gate.inputs.candidate}
edges: []
outputs: {}
`, workspace, workspace)
	if err := os.WriteFile(graphPath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return graphPath, codexBin, workspace
}

func attemptDiffsOf(t *testing.T, stdout string) []events.AttemptDiffPayload {
	t.Helper()
	var response struct {
		Events []events.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	var out []events.AttemptDiffPayload
	for _, e := range response.Events {
		if p, ok := e.Payload.(events.AttemptDiffPayload); ok {
			out = append(out, p)
		}
	}
	return out
}

func scratchLeft(t *testing.T, tmp string) []string {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(tmp, "agentplane-diff-*"))
	return left
}

func TestRecordDiffsCapturesAgentAttemptsRedactedAndCleansScratch(t *testing.T) {
	graphPath, codexBin, _ := diffRepo(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("AP_DIFF_SECRET", diffSecret)
	store := t.TempDir()
	var out, errout bytes.Buffer
	args := []string{"run", graphPath, "--adapter", "codex", "--codex-bin", codexBin, "--json", "--record-diffs", "--redact-env", "AP_DIFF_SECRET", "--store-dir", store}
	if code := runCommand(context.Background(), args, &out, &errout); code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errout)
	}
	if !strings.Contains(errout.String(), "--record-diffs records changed file contents") {
		t.Fatalf("missing notice: %q", errout.String())
	}
	got := attemptDiffsOf(t, out.String())
	if len(got) != 3 || got[0].Path != "src/a.go" || got[1].Path != "src/b.go" || got[2].Path != "" || got[2].Files != 2 {
		t.Fatalf("attempt diffs = %+v", got)
	}
	if !got[0].Redacted || !strings.Contains(got[0].Patch, "token=") || strings.Contains(got[0].Patch, diffSecret) {
		t.Fatalf("a.go = %+v", got[0])
	}
	if strings.Contains(out.String(), diffSecret) {
		t.Fatal("secret reached stdout")
	}
	assertNoSecretIn(t, store, diffSecret)
	if left := scratchLeft(t, tmp); len(left) != 0 {
		t.Fatalf("scratch directories survived: %v", left)
	}
}

func assertNoSecretIn(t *testing.T, dir, secret string) {
	t.Helper()
	files := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files++
			if data, _ := os.ReadFile(path); strings.Contains(string(data), secret) {
				t.Fatalf("secret reached %s", path)
			}
		}
		return nil
	})
	if files == 0 {
		t.Fatalf("nothing was persisted under %s", dir)
	}
}

func TestRecordDiffsOffByDefaultEmitsNoAttemptDiff(t *testing.T) {
	graphPath, codexBin, _ := diffRepo(t)
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), []string{"run", graphPath, "--adapter", "codex", "--codex-bin", codexBin, "--json"}, &out, &errout)
	if code != 0 || strings.Contains(out.String(), "attempt_diff") || errout.Len() != 0 {
		t.Fatalf("code=%d stderr=%s", code, &errout)
	}
	requireDemoTools(t)
	code, demo, _ := cli(t, "run", demoGraphPath(t), "--approve", "approve", "--json", "--record-diffs")
	if code != 0 || strings.Contains(demo, "attempt_diff") {
		t.Fatalf("a graph without a loop Agent must behave as without the flag (code %d)", code)
	}
}

func TestRecordDiffsFlagDefaultsOffAndIsValidated(t *testing.T) {
	for _, args := range [][]string{{"run", "unused", "--record-diffs=maybe"}, {"serve", "--record-diffs=maybe"}} {
		var out, errout bytes.Buffer
		if code := runCommand(context.Background(), args, &out, &errout); code != exitUsage {
			t.Errorf("%v: code %d, want usage error", args, code)
		}
	}
	if !strings.Contains(usageText(t), "--record-diffs") {
		t.Fatal("usage must list --record-diffs")
	}
}

func usageText(t *testing.T) string {
	t.Helper()
	var out, errout bytes.Buffer
	runCommand(context.Background(), nil, &out, &errout)
	return out.String() + errout.String()
}

func TestServeAcceptsRecordDiffsAndLeavesNoScratch(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/printf")
	shellGraph := filepath.Join(t.TempDir(), "shell.yaml")
	if err := os.WriteFile(shellGraph, []byte("schema_version: 0\nid: s\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: /usr/bin/printf, args: [hi]}\nedges: []\noutputs: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	s := startServe(t, "--record-diffs")
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(shellGraph)+`}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %s", code, body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal([]byte(body), &started)
	_, stream := s.call(t, "GET", "/runs/"+started.RunID+"/events", "")
	if strings.Contains(stream, "attempt_diff") || !strings.Contains(stream, "event: end") {
		t.Fatalf("stream: %s", stream)
	}
	s.closeStdin()
	select {
	case <-s.exit:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not exit")
	}
	if left := scratchLeft(t, tmp); len(left) != 0 {
		t.Fatalf("scratch directories survived: %v", left)
	}
}
