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
	"agentplane/internal/graph"
	"agentplane/internal/testenv"
)

func TestCLIFakeAgentGateShellBindings(t *testing.T) {
	for _, green := range []bool{true, false} {
		t.Run(fmt.Sprint(green), func(t *testing.T) {
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
			script := `#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --cd) shift;work=$1;; --output-last-message) shift;result=$1;; esac;shift;done
cd "$work" || exit 1
cat >/dev/null
printf edited >actual.txt
printf '{"summary":"done","artifacts":[]}' >"$result"
`
			if e := os.WriteFile(binary, []byte(script), 0700); e != nil {
				t.Fatal(e)
			}
			allowed := "actual.txt"
			if !green {
				allowed = "other.txt"
			}
			path := filepath.Join(t.TempDir(), "graph.yaml")
			text := fmt.Sprintf(`schema_version: 0
id: gated-agent
inputs: {}
nodes:
  agent:
    type: agent
    config: {adapter: codex, workspace: %q, task: edit}
    outputs:
      changed_files: {type: json, required: true}
  gate:
    type: gate
    config: {check_id: scope-check, kind: reported_file_scope, input: files, allowed_paths: [%q]}
    inputs:
      files: {type: json, required: true}
    outputs:
      verdict: {type: string, required: true}
  shell:
    type: shell
    config:
      command: /usr/bin/printf
      args: [downstream]
    inputs:
      accepted: {type: string, required: true}
    outputs:
      stdout: {type: string}
edges:
  - {from: agent.outputs.changed_files, to: gate.inputs.files}
  - {from: gate.outputs.verdict, to: shell.inputs.accepted}
outputs: {}
`, dir, allowed)
			if e := os.WriteFile(path, []byte(text), 0600); e != nil {
				t.Fatal(e)
			}
			var out, errout bytes.Buffer
			code := runCommand(context.Background(), []string{"run", path, "--adapter", "codex", "--codex-bin", binary, "--json"}, &out, &errout)
			if (code == 0) != green || errout.Len() != 0 {
				t.Fatalf("code %d %s %s", code, &out, &errout)
			}
			var doc struct {
				Events []events.Event `json:"events"`
			}
			if e := json.Unmarshal(out.Bytes(), &doc); e != nil {
				t.Fatal(e)
			}
			checks, starts := 0, 0
			for _, e := range doc.Events {
				if e.Type == events.GateCheck {
					checks++
					p := e.Payload.(events.GateCheckPayload)
					if p.Version != 1 || p.DurationNS < 0 || len(p.Scope.CheckedInputs) == 0 || e.SchemaVersion != events.SchemaVersion || e.RunID == "" || e.NodeRunID == "" || e.AttemptID != "" || p.Validate() != nil {
						t.Fatal("full result contract/correlation")
					}
					if len(events.CheckResults(doc.Events, e.RunID, e.NodeRunID)) != 1 {
						t.Fatal("typed retrieval")
					}
					if (p.Verdict == "green") != green || p.Evidence[0].Source != "reported" {
						t.Fatal("gate evidence")
					}
				}
				if e.Type == events.NodeStarted && e.NodeID == "shell" {
					starts++
				}
			}
			if checks != 1 || starts != map[bool]int{true: 1, false: 0}[green] {
				t.Fatalf("checks %d starts %d %s", checks, starts, &out)
			}
		})
	}
}
func TestCLILocalGateWorksWithoutAdapter(t *testing.T) {
	for _, green := range []bool{true, false} {
		t.Run(fmt.Sprint(green), func(t *testing.T) {
			testenv.RequireDarwinOrLinux(t, "the command Gate")
			testenv.RequireExecutable(t, "/usr/bin/printf")
			cwd, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			left := strings.Repeat("SECRET left\n", 3000)
			right := left
			if !green {
				right = strings.Repeat("SECRET right\n", 3000)
			}
			os.WriteFile(filepath.Join(cwd, "left"), []byte(left), 0400)
			os.WriteFile(filepath.Join(cwd, "right"), []byte(right), 0400)
			n := graph.NodeDefinition{Type: "gate", Config: graph.NodeConfig{"check_id": "diff-check", "kind": "command", "cwd": cwd, "command": "/usr/bin/diff", "left_input": "left", "right_input": "right"}, Inputs: map[string]graph.PortDefinition{"left": {Type: graph.ValueTypeString, Required: true}, "right": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{"verdict": {Type: graph.ValueTypeString, Required: true}}}
			d := &graph.Definition{ID: "local-diff", Nodes: map[string]graph.NodeDefinition{"gate": n, "shell": {Type: "shell", Config: graph.NodeConfig{"command": "/usr/bin/printf", "args": []string{"downstream"}}, Inputs: map[string]graph.PortDefinition{"accepted": {Type: graph.ValueTypeString, Required: true}}}}, Inputs: map[string]graph.InputDefinition{"left": {Type: graph.ValueTypeString, Required: true}, "right": {Type: graph.ValueTypeString, Required: true}}, GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.left", To: "gate.inputs.left"}, {From: "inputs.right", To: "gate.inputs.right"}}, Edges: []graph.EdgeDefinition{{From: "gate.outputs.verdict", To: "shell.inputs.accepted"}}}
			b, e := graph.Marshal(d)
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(t.TempDir(), "graph.yaml")
			os.WriteFile(path, b, 0600)
			t.Setenv("GATE_TEST_SECRET", "SECRET")
			var out, errout bytes.Buffer
			code := runCommand(context.Background(), []string{"run", path, "--input", "left=left", "--input", "right=right", "--redact-env", "GATE_TEST_SECRET", "--json"}, &out, &errout)
			if (code == 0) != green {
				t.Fatalf("%d %s %s", code, &out, &errout)
			}
			var doc struct {
				Events []events.Event `json:"events"`
			}
			json.Unmarshal(out.Bytes(), &doc)
			found := false
			starts := 0
			for _, e := range doc.Events {
				if e.Type == events.GateCheck {
					found = true
					p := e.Payload.(events.GateCheckPayload)
					if p.Version != 1 || p.DurationNS < 0 || len(p.Scope.CheckedInputs) == 0 || e.SchemaVersion != events.SchemaVersion || e.RunID == "" || e.NodeRunID == "" || e.AttemptID != "" || p.Validate() != nil {
						t.Fatal("full result contract/correlation")
					}
					if len(events.CheckResults(doc.Events, e.RunID, e.NodeRunID)) != 1 {
						t.Fatal("typed retrieval")
					}
					ev := p.Evidence[0]
					if strings.Contains(string(ev.Stdout), "SECRET") || (!green && ev.StdoutOmitted == 0) || (*ev.ExitCode == 0) != green {
						t.Fatal("unsafe/incorrect bounded diff evidence")
					}
				}
				if e.Type == events.NodeStarted && e.NodeID == "shell" {
					starts++
				}
			}
			if !found || starts != map[bool]int{true: 1, false: 0}[green] {
				t.Fatal("local gate scheduling")
			}
			for k, v := range map[string]string{"left": left, "right": right} {
				b, _ := os.ReadFile(filepath.Join(cwd, k))
				if string(b) != v {
					t.Fatal("changed comparison input")
				}
			}
		})
	}
}
