package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/testenv"
)

func TestCLIChangedFilesScopeDenyUsesGitNotProviderOutput(t *testing.T) {
	workspace := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		testenv.RequireExecutable(t, "/usr/bin/git")
		c := exec.Command("/usr/bin/git", args...)
		c.Dir = workspace
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	binary := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --cd) shift;work=$1;; --output-last-message) shift;result=$1;; esac;shift;done
cd "$work" || exit 1
cat >/dev/null
mkdir -p src
printf allowed >src/a.go
printf forbidden >outside.go
printf '{"summary":"candidate","artifacts":[]}' >"$result"
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(t.TempDir(), "graph.yaml")
	doc := fmt.Sprintf(`schema_version: 0
id: scope
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
	if err := os.WriteFile(graphPath, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), []string{"run", graphPath, "--adapter", "codex", "--codex-bin", binary, "--json"}, &out, &errout)
	if code != 4 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errout)
	}
	var response struct {
		Events []events.Event `json:"events"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, event := range response.Events {
		if event.Type == events.GateCheck {
			p := event.Payload.(events.CheckResult)
			found = p.Verdict == events.CheckRed && p.Reason == "changed_files_outside_scope" && p.ChangedFiles.Mode == "deny" && len(p.Evidence) == 1 && p.Evidence[0].Path == "outside.go"
		}
	}
	if !found {
		t.Fatalf("missing CLI scope evidence: %s", &out)
	}
}
