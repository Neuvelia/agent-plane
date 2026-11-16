package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// The desktop's inspector stories replay real answers of the document API
// (F2.2 requirement 15). This test records them from the served composition
// and fails when a recording no longer matches what the sidecar answers.
// AGENTPLANE_UPDATE_FIXTURES=1 rewrites them. Paths in the graphs are Unix
// paths, so the recordings are made on Unix.

const fixtureDir = "../../desktop/test/fixtures"

var inspectorGraphs = map[string]string{
	// a shell producer with a json_predicate loop, an unknown nested key, a
	// graph input and an edge; a second, independent branch
	"shell": `schema_version: 0
id: inspector-shell
inputs:
  target: {type: string, required: true}
nodes:
  build:
    type: shell
    inputs:
      target: {type: string, required: true}
    config:
      command: /usr/bin/make
      args: [build]
      env_allowlist: [HOME, PATH]
      input_env: {target: TARGET}
      stdout_json: true
      retries: {max: 2, on: [timeout]}  # not a Shell setting: kept under Advanced
    outputs:
      json: {type: json, required: true}
      stdout: {type: string}
    loop:
      max_attempts: 3
      gate:
        inputs:
          candidate: {type: json, required: true}
        config: {check_id: build-ok, kind: json_predicate, input: candidate, field: [ok], operator: eq, expected: true}
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.candidate}
  report:
    type: shell
    inputs:
      text: {type: string}
    config: {command: /usr/bin/tee, args: [report.txt]}
  lint:
    type: shell
    config: {command: /usr/bin/true}
graph_input_bindings:
  - {from: inputs.target, to: build.inputs.target}
edges:
  - {from: build.outputs.stdout, to: report.inputs.text}
outputs:
  built: {from: build.outputs.json}
`,
	// one loop per Gate kind: command, json_predicate and reported_file_scope on
	// Shell producers, changed_files_scope on an Agent
	"loops": `schema_version: 0
id: inspector-loops
inputs: {}
nodes:
  diffed:
    type: shell
    config: {command: /usr/bin/printf, args: [ok]}
    inputs:
      expected: {type: string}
    outputs:
      stdout: {type: string, required: true}
    loop:
      gate:
        inputs:
          left: {type: string, required: true}
          right: {type: string, required: true}
        config: {check_id: same-text, kind: command, command: /usr/bin/diff, cwd: /work/repo, left_input: left, right_input: right}
      bindings:
        - {from: candidate.outputs.stdout, to: gate.inputs.left}
        - {from: node.inputs.expected, to: gate.inputs.right}
  status:
    type: shell
    config: {command: /usr/bin/printf, args: ['{"status":"ok"}'], stdout_json: true}
    outputs:
      json: {type: json, required: true}
    loop:
      max_attempts: 5
      gate:
        inputs:
          doc: {type: json, required: true}
        config: {check_id: status-ok, kind: json_predicate, input: doc, field: [status], operator: eq, expected_json: '"ok"'}
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.doc}
  files:
    type: shell
    config: {command: /usr/bin/printf, args: ['["src/a.go"]'], stdout_json: true}
    outputs:
      json: {type: json, required: true}
    loop:
      gate:
        inputs:
          files: {type: json, required: true}
        config: {check_id: in-scope, kind: reported_file_scope, input: files, allowed_paths: [src/]}
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.files}
  fix:
    type: agent
    config:
      adapter: codex
      workspace: /work/repo
      task: Fix the failing test.
      allowed_paths: [src/]
    outputs:
      summary: {type: string, required: true}
    loop:
      gate:
        inputs:
          candidate: {type: string, required: true}
        config: {check_id: scope, kind: changed_files_scope, input: candidate, allowed_paths: [src/], mode: warn, workspace: /work/repo}
      bindings:
        - {from: candidate.outputs.summary, to: gate.inputs.candidate}
edges: []
outputs: {}
`,
	// a standalone Gate, a human node, an agent fed its task by an input, and a
	// node of a type this sidecar does not know
	"types": `schema_version: 0
id: inspector-types
inputs:
  task: {type: string, required: true}
nodes:
  produce:
    type: shell
    config: {command: /usr/bin/printf, args: ['["src/a.go"]'], stdout_json: true}
    outputs:
      json: {type: json, required: true}
      stdout: {type: string}
  scope:
    type: gate
    inputs:
      files: {type: json, required: true}
    config: {check_id: scope, kind: reported_file_scope, input: files, allowed_paths: [src/]}
    outputs:
      verdict: {type: string}
  approve:
    type: human
    inputs:
      summary: {type: string, required: true}
    config:
      prompt: Ship it?
      context_inputs: [summary]
    outputs:
      decision: {type: string, required: true}
  agent:
    type: agent
    inputs:
      task: {type: string, required: true}
    config: {adapter: codex, workspace: /work/repo}
  future:
    type: plugin
    config: {anything: [1, 2]}
graph_input_bindings:
  - {from: inputs.task, to: agent.inputs.task}
edges:
  - {from: produce.outputs.json, to: scope.inputs.files}
  - {from: produce.outputs.stdout, to: approve.inputs.summary}
outputs: {}
`,
	// a value the executor rejects, and a value its field cannot show
	"config-error": `schema_version: 0
id: inspector-config-error
inputs: {}
nodes:
  work:
    type: shell
    config:
      command: ""
      args: not-a-list
    outputs:
      stdout: {type: string}
edges: []
outputs:
  result: {from: work.outputs.stdout}
`,
	// diagnostics no config field shows: a port of an unknown type and a loop
	// Gate input of an unknown type; an env map for the key-collision state
	"diagnostics": `schema_version: 0
id: inspector-diagnostics
inputs: {}
nodes:
  status:
    type: shell
    inputs:
      extra: {type: blob}
    config:
      command: /usr/bin/printf
      args: ['{"status":"ok"}']
      stdout_json: true
      env: {MODE: fast}
    outputs:
      json: {type: json, required: true}
    loop:
      gate:
        inputs:
          doc: {type: json, required: true}
          spare: {type: blob}
        config: {check_id: status-ok, kind: json_predicate, input: doc, field: [status], operator: eq, expected_json: '"ok"'}
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.doc}
edges: []
outputs: {}
`,
}

// inspectorEdits are recorded operation answers the stories replay.
var inspectorEdits = []struct {
	name, graph string
	ops         string
}{
	{"loop-starter", "loops", `[{"op":"clear_loop","node":"status"},{"op":"set_loop","node":"status","max_attempts":3,"gate":{"inputs":{"candidate":{"type":"json","required":true}},"config":{"check_id":"status-check","kind":"json_predicate","input":"candidate","field":["ok"],"operator":"eq","expected":true}},"bindings":[{"from":"candidate.outputs.json","to":"gate.inputs.candidate"}]}]`},
	{"rename-input", "shell", `[{"op":"update_port","node":"report","direction":"input","port":"text","new_port":"renamed"}]`},
	{"remove-port", "shell",`[{"op":"remove_port","node":"build","direction":"output","port":"stdout"}]`},
	{"set-command", "shell", `[{"op":"set_config","node":"build","path":["command"],"value":"/usr/bin/gmake"}]`},
}

func TestInspectorFixturesAreCurrent(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the recorded graphs use Unix paths")
	}
	s := startServe(t)
	dir := t.TempDir()
	grant := func(name, text string) string {
		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		status, body := s.call(t, "POST", "/graphs/grants", `{"path":`+jsonString(path)+`,"mode":"open"}`)
		if status != 201 {
			t.Fatalf("grant %s: %d %s", name, status, body)
		}
		var g struct{ Grant string }
		_ = json.Unmarshal([]byte(body), &g)
		return g.Grant
	}
	for name, text := range inspectorGraphs {
		status, body := s.call(t, "POST", "/graphs/open", `{"grant":`+jsonString(grant(name, text))+`}`)
		if status != 200 {
			t.Fatalf("open %s: %d %s", name, status, body)
		}
		checkFixture(t, "inspector-open-"+name+".json", []byte(body))
	}
	for _, e := range inspectorEdits {
		text := inspectorGraphs[e.graph]
		sum := sha256.Sum256([]byte(text))
		req, _ := json.Marshal(map[string]any{"grant": grant("edit-"+e.name, text), "base_hash": hex.EncodeToString(sum[:]), "text": text, "ops_version": 1, "ops": json.RawMessage(e.ops)})
		status, body := s.call(t, "POST", "/graphs/edit", string(req))
		if status != 200 {
			t.Fatalf("edit %s: %d %s", e.name, status, body)
		}
		wrapped, _ := json.Marshal(map[string]any{"status": status, "body": json.RawMessage(body)})
		checkFixture(t, "inspector-edit-"+e.name+".json", wrapped)
	}
}

// checkFixture compares a recording with the answer, or rewrites it.
func checkFixture(t *testing.T, name string, answer []byte) {
	t.Helper()
	path := filepath.Join(fixtureDir, name)
	var v any
	if err := json.Unmarshal(answer, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	pretty, _ := json.MarshalIndent(v, "", " ")
	pretty = append(pretty, '\n')
	if os.Getenv("AGENTPLANE_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(path, pretty, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (record it with AGENTPLANE_UPDATE_FIXTURES=1)", err)
	}
	if string(normalizeJSON(t, want)) != string(normalizeJSON(t, pretty)) {
		t.Errorf("%s is out of date: run the cmd/agentplane tests with AGENTPLANE_UPDATE_FIXTURES=1", path)
	}
}
