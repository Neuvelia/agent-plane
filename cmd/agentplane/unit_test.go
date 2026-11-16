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

func TestCLIFakeCodexUnitsRetryOnlyRejectedCandidate(t *testing.T) {
	for _, mode := range []string{"correct", "exhaust"} {
		t.Run(mode, func(t *testing.T) {
			root, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
				testenv.RequireExecutable(t, "/usr/bin/git")
				c := exec.Command("/usr/bin/git", args...)
				c.Dir = root
				if b, e := c.CombinedOutput(); e != nil {
					t.Fatal(e, string(b))
				}
			}
			if e := os.WriteFile(filepath.Join(root, "expected"), []byte("green"), 0600); e != nil {
				t.Fatal(e)
			}
			binary := filepath.Join(t.TempDir(), "codex")
			script := `#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --cd) shift;work=$1;; --output-last-message) shift;result=$1;; esac;shift;done
cd "$work" || exit 1
prompt=$(cat)
unit=c
case "$prompt" in *UNIT_A*) unit=a;; *UNIT_B*) unit=b;; *UNIT_D*) unit=d;; esac
printf 'call\n' >>"$unit.calls"
printf green >"$unit.candidate"
if [ "$unit" = c ];then
 printf red >c.candidate
`
			if mode == "correct" {
				script += ` case "$prompt" in *'UNTRUSTED CHECK FEEDBACK'*) printf green >c.candidate;; esac
`
			}
			script += `fi
printf '{"summary":"%s.candidate","artifacts":[]}' "$unit" >"$result"
`
			if e := os.WriteFile(binary, []byte(script), 0700); e != nil {
				t.Fatal(e)
			}
			d := &graph.Definition{ID: "unit-cli", Inputs: map[string]graph.InputDefinition{"reference": {Type: graph.ValueTypeString, Required: true}}, Nodes: map[string]graph.NodeDefinition{}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}, Units: []string{"a", "b", "d", "z_c"}}
			for _, id := range d.Units {
				unit := id
				if id == "z_c" {
					unit = "c"
				}
				d.Nodes[id] = graph.NodeDefinition{Type: "agent", Inputs: map[string]graph.PortDefinition{"reference": {Type: graph.ValueTypeString, Required: true}}, Config: graph.NodeConfig{"adapter": "codex", "workspace": root, "task": "UNIT_" + strings.ToUpper(unit), "allowed_paths": []string{unit + ".candidate"}}, Outputs: map[string]graph.PortDefinition{"summary": {Type: graph.ValueTypeString, Required: true}}, Loop: &graph.LoopDefinition{Gate: graph.LoopGateDefinition{Inputs: map[string]graph.PortDefinition{"actual": {Type: graph.ValueTypeString, Required: true}, "reference": {Type: graph.ValueTypeString, Required: true}}, Config: graph.NodeConfig{"check_id": "compare-" + unit, "kind": "command", "cwd": root, "command": "/usr/bin/diff", "left_input": "actual", "right_input": "reference"}}, Bindings: []graph.LoopBinding{{From: "candidate.outputs.summary", To: "gate.inputs.actual"}, {From: "node.inputs.reference", To: "gate.inputs.reference"}}}}
				d.GraphInputBindings = append(d.GraphInputBindings, graph.GraphInputBindingDefinition{From: "inputs.reference", To: graph.NodeInputReference(id + ".inputs.reference")})
			}
			d.Nodes["join"] = graph.NodeDefinition{Type: "shell", Inputs: map[string]graph.PortDefinition{}, Config: graph.NodeConfig{"command": "/usr/bin/printf", "args": []string{"joined"}}, Outputs: map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString}}}
			for _, id := range d.Units {
				d.Nodes["join"].Inputs[id] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
				d.Edges = append(d.Edges, graph.EdgeDefinition{From: graph.NodeOutputReference(id + ".outputs.summary"), To: graph.NodeInputReference("join.inputs." + id)})
			}
			d.Outputs["joined"] = graph.OutputDefinition{From: "join.outputs.stdout"}
			encoded, e := graph.Marshal(d)
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(t.TempDir(), "units.yaml")
			if e := os.WriteFile(path, encoded, 0600); e != nil {
				t.Fatal(e)
			}
			var out, errout bytes.Buffer
			code := runCommand(context.Background(), []string{"run", path, "--adapter", "codex", "--codex-bin", binary, "--input", "reference=expected", "--max-concurrency", "4", "--json"}, &out, &errout)
			want := 0
			cCalls := 2
			if mode == "exhaust" {
				want = 4
				cCalls = 3
			}
			if code != want {
				t.Fatalf("code %d want %d: %s %s", code, want, &out, &errout)
			}
			for _, unit := range []string{"a", "b", "c", "d"} {
				calls, e := os.ReadFile(filepath.Join(root, unit+".calls"))
				if e != nil {
					t.Fatal(e)
				}
				want := 1
				if unit == "c" {
					want = cCalls
				}
				if bytes.Count(calls, []byte("call\n")) != want {
					t.Fatal(unit, string(calls))
				}
			}
			var doc struct {
				Schema  int            `json:"cli_schema_version"`
				Events  []events.Event `json:"events"`
				Nodes   []cliNode      `json:"nodes"`
				Outputs []cliOutput    `json:"outputs"`
			}
			if e := json.Unmarshal(out.Bytes(), &doc); e != nil {
				t.Fatal(e, out.String())
			}
			if doc.Schema != 1 {
				t.Fatal(doc.Schema)
			}
			attempts := map[string]int{}
			envelopes := map[string]int{}
			acceptance := map[string]events.AttemptID{}
			var aAccepted, cRed uint64
			joins := 0
			for _, ev := range doc.Events {
				if ev.SchemaVersion != events.SchemaVersion {
					t.Fatal(ev.SchemaVersion)
				}
				switch ev.Type {
				case events.AttemptStarted:
					attempts[ev.NodeID]++
				case events.ReturnEnvelope:
					p := ev.Payload.(events.ReturnEnvelopePayload)
					envelopes[ev.NodeID]++
					if p.Verdict == "green" {
						acceptance[ev.NodeID] = p.AttemptID
						if ev.NodeID == "a" {
							aAccepted = ev.Sequence
						}
					} else if ev.NodeID == "z_c" && cRed == 0 {
						cRed = ev.Sequence
					}
				case events.NodeStarted:
					if ev.NodeID == "join" {
						joins++
					}
				}
			}
			if aAccepted == 0 || aAccepted >= cRed {
				t.Fatal("A not accepted before C red")
			}
			for _, id := range d.Units {
				want := 1
				if id == "z_c" {
					want = cCalls
				}
				if attempts[id] != want || envelopes[id] != want {
					t.Fatal(attempts, envelopes)
				}
			}
			for _, n := range doc.Nodes {
				if n.NodeID == "a" || n.NodeID == "b" || n.NodeID == "d" {
					if n.AcceptedAttemptID == "" || n.AcceptedAttemptID != acceptance[n.NodeID] {
						t.Fatal(n, acceptance)
					}
				}
			}
			if mode == "correct" {
				if joins != 1 || len(doc.Outputs) != 1 || fmt.Sprint(doc.Outputs[0].Value) != "joined" {
					t.Fatal(joins, doc.Outputs)
				}
			} else if joins != 0 || len(doc.Outputs) != 0 {
				t.Fatal("exhausted candidate escaped")
			}
		})
	}
}

func TestCLIInvalidUnitScopeIsPreflightDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-unit.yaml")
	document := `schema_version: 0
id: invalid-unit
units: [work]
inputs: {}
nodes:
  work:
    type: agent
    config: {adapter: codex, workspace: /tmp, task: task, allowed_paths: [../outside]}
    outputs: {raw_metadata: {type: json, required: true}}
    loop:
      gate:
        inputs: {candidate: {type: json, required: true}}
        config: {check_id: check, kind: json_predicate, input: candidate, field: [ok], operator: eq, expected: true}
      bindings: [{from: candidate.outputs.raw_metadata, to: gate.inputs.candidate}]
edges: []
outputs: {}
`
	if e := os.WriteFile(path, []byte(document), 0600); e != nil {
		t.Fatal(e)
	}
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), []string{"run", path, "--adapter", "codex", "--codex-bin", filepath.Join(t.TempDir(), "missing", "codex"), "--json"}, &out, &errout)
	if code != exitGraph || !strings.Contains(out.String(), `"code":"E_UNIT_SCOPE"`) || !strings.Contains(out.String(), `"status":"invalid"`) {
		t.Fatal(code, out.String(), errout.String())
	}
}
