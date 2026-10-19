package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/events"
)

func TestCLIFakeCodexLoopInterventionGreenAndWatch(t *testing.T) {
	for _, mode := range []string{"red", "green", "watch"} {
		t.Run(mode, func(t *testing.T) {
			workspace, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
				c := exec.Command("/usr/bin/git", args...)
				c.Dir = workspace
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
			}
			// Command Gate compares the producer's candidate path with an explicitly
			// bound reference. First attempt is red, feedback permits the correction.
			if err := os.WriteFile(filepath.Join(workspace, "expected"), []byte("green"), 0600); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(t.TempDir(), "codex")
			script := `#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --cd) shift;work=$1;; --output-last-message) shift;result=$1;; esac;shift;done
cd "$work" || exit 1
cat >prompt
printf red >candidate
`
			if mode != "red" {
				script += `if /usr/bin/grep -q 'UNTRUSTED CHECK FEEDBACK' prompt;then printf green >candidate;fi
`
			}
			script += `printf '{"summary":"candidate","artifacts":[]}' >"$result"
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "graph.yaml")
			document := fmt.Sprintf(`schema_version: 0
id: correction
inputs: {reference: {type: string, required: true}}
nodes:
  work:
    type: agent
    inputs: {reference: {type: string, required: true}}
    config: {adapter: codex, workspace: %q, task: correct candidate, allowed_paths: [candidate]}
    outputs: {summary: {type: string, required: true}}
    loop:
      gate:
        inputs: {actual: {type: string, required: true}, reference: {type: string, required: true}}
        config: {check_id: compare, kind: command, cwd: %q, command: /usr/bin/diff, left_input: actual, right_input: reference}
      bindings:
        - {from: candidate.outputs.summary, to: gate.inputs.actual}
        - {from: node.inputs.reference, to: gate.inputs.reference}
  down:
    type: shell
    inputs: {candidate: {type: string, required: true}}
    config: {command: /usr/bin/printf, args: [accepted]}
    outputs: {stdout: {type: string}}
edges: [{from: work.outputs.summary, to: down.inputs.candidate}]
graph_input_bindings: [{from: inputs.reference, to: work.inputs.reference}]
outputs: {}
`, workspace, workspace)
			if err := os.WriteFile(path, []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"run", path, "--adapter", "codex", "--codex-bin", binary, "--input", "reference=expected"}
			if mode == "watch" {
				args = append(args, "--watch")
			} else {
				args = append(args, "--json")
			}
			var out, errout bytes.Buffer
			code := runCommand(context.Background(), args, &out, &errout)
			want := 0
			attempts := 2
			if mode == "red" {
				want = 4
				attempts = 3
			}
			if code != want {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errout)
			}
			if mode == "watch" {
				if strings.Count(out.String(), "accepted") != 1 || strings.Count(errout.String(), "attempt 1/3 started") != 1 || strings.Count(errout.String(), "attempt 2/3 started") != 1 || strings.Count(errout.String(), ": rejected") != 1 || strings.Count(errout.String(), ": accepted") != 1 {
					t.Fatalf("watch repeated/missing acceptance %s %s", &out, &errout)
				}
				return
			}
			var doc struct {
				Schema  int            `json:"cli_schema_version"`
				Events  []events.Event `json:"events"`
				Failure struct {
					Kind string `json:"kind"`
				} `json:"failure"`
			}
			if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if doc.Schema != 1 {
				t.Fatal(doc.Schema)
			}
			count := 0
			for _, e := range doc.Events {
				if e.Type == events.AttemptFinished {
					count++
					p := e.Payload.(events.AttemptFinishedPayload)
					if count == attempts {
						expected := "accepted"
						if mode == "red" {
							expected = "needs_intervention"
						}
						if p.Outcome != expected {
							t.Fatal(p)
						}
					}
				}
			}
			if count != attempts {
				t.Fatal(count)
			}
			if mode == "red" && doc.Failure.Kind != "needs_intervention" {
				t.Fatal(doc.Failure)
			}
			prompt, err := os.ReadFile(filepath.Join(workspace, "prompt"))
			if err != nil || !bytes.Contains(prompt, []byte("UNTRUSTED CHECK FEEDBACK")) || !bytes.Contains(prompt, []byte(`"previous_attempt_id"`)) {
				t.Fatal(err, string(prompt))
			}
		})
	}
}
