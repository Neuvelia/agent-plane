package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// F2.4: the CLI and the document API run one validation path. Every diagnostic
// code that static validation can produce has a graph here that produces it, and
// for each the API's diagnostics must equal `graph validate --json`. The meta-test
// fails when a code appears in the sources without an entry.

const corpusHead = "schema_version: 0\nid: g\n"

func nodeShell(id, extra string) string {
	return "  " + id + ":\n    type: shell\n    config: {command: /usr/bin/true}\n" + extra
}

// corpus maps a code to a graph that produces it.
var corpus = map[string]string{
	"E_GRAPH_ID_REQUIRED":                    "schema_version: 0\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n",
	"E_GRAPH_ID_INVALID":                     "schema_version: 0\nid: Bad_ID\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n",
	"E_GRAPH_FIELD_REQUIRED":                 corpusHead + "inputs: {}\nnodes: {}\noutputs: {}\n",
	"E_NODE_TYPE_REQUIRED":                   corpusHead + "inputs: {}\nnodes:\n  a: {outputs: {x: {type: string}}}\nedges: []\noutputs: {}\n",
	"E_IDENTIFIER_INVALID":                   corpusHead + "inputs: {}\nnodes:\n" + nodeShell("Bad Node", "") + "edges: []\noutputs: {}\n",
	"E_VALUE_TYPE_INVALID":                   corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    outputs:\n      x: {type: banana}\n") + "edges: []\noutputs: {}\n",
	"E_EDGE_SOURCE_REFERENCE_INVALID":        corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    inputs:\n      i: {type: string}\n") + "edges:\n  - {from: nope, to: a.inputs.i}\noutputs: {}\n",
	"E_EDGE_TARGET_REFERENCE_INVALID":        corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    outputs:\n      o: {type: string}\n") + "edges:\n  - {from: a.outputs.o, to: nope}\noutputs: {}\n",
	"E_EDGE_SOURCE_NODE_NOT_FOUND":           corpusHead + "inputs: {}\nnodes:\n" + nodeShell("b", "    inputs:\n      i: {type: string}\n") + "edges:\n  - {from: ghost.outputs.o, to: b.inputs.i}\noutputs: {}\n",
	"E_EDGE_SOURCE_OUTPUT_NOT_FOUND":         corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "") + nodeShell("b", "    inputs:\n      i: {type: string}\n") + "edges:\n  - {from: a.outputs.nope, to: b.inputs.i}\noutputs: {}\n",
	"E_EDGE_TARGET_NODE_NOT_FOUND":           corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    outputs:\n      o: {type: string}\n") + "edges:\n  - {from: a.outputs.o, to: ghost.inputs.i}\noutputs: {}\n",
	"E_EDGE_TARGET_INPUT_NOT_FOUND":          corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    outputs:\n      o: {type: string}\n") + nodeShell("b", "") + "edges:\n  - {from: a.outputs.o, to: b.inputs.nope}\noutputs: {}\n",
	"E_BINDING_TYPE_MISMATCH":                corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    outputs:\n      o: {type: string}\n") + nodeShell("b", "    inputs:\n      i: {type: number}\n") + "edges:\n  - {from: a.outputs.o, to: b.inputs.i}\noutputs: {}\n",
	"E_GRAPH_INPUT_NOT_FOUND":                corpusHead + "inputs: {}\nnodes:\n" + nodeShell("b", "    inputs:\n      i: {type: string}\n") + "graph_input_bindings:\n  - {from: inputs.ghost, to: b.inputs.i}\nedges: []\noutputs: {}\n",
	"E_GRAPH_INPUT_REFERENCE_INVALID":        corpusHead + "inputs: {}\nnodes:\n" + nodeShell("b", "    inputs:\n      i: {type: string}\n") + "graph_input_bindings:\n  - {from: nope, to: b.inputs.i}\nedges: []\noutputs: {}\n",
	"E_GRAPH_INPUT_TARGET_REFERENCE_INVALID": corpusHead + "inputs:\n  t: {type: string}\nnodes:\n" + nodeShell("b", "") + "graph_input_bindings:\n  - {from: inputs.t, to: nope}\nedges: []\noutputs: {}\n",
	"E_GRAPH_INPUT_TARGET_NODE_NOT_FOUND":    corpusHead + "inputs:\n  t: {type: string}\nnodes:\n" + nodeShell("b", "") + "graph_input_bindings:\n  - {from: inputs.t, to: ghost.inputs.i}\nedges: []\noutputs: {}\n",
	"E_GRAPH_INPUT_TARGET_INPUT_NOT_FOUND":   corpusHead + "inputs:\n  t: {type: string}\nnodes:\n" + nodeShell("b", "") + "graph_input_bindings:\n  - {from: inputs.t, to: b.inputs.nope}\nedges: []\noutputs: {}\n",
	"E_GRAPH_OUTPUT_REFERENCE_INVALID":       corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "") + "edges: []\noutputs:\n  r: {from: nope}\n",
	"E_GRAPH_OUTPUT_SOURCE_NODE_NOT_FOUND":   corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "") + "edges: []\noutputs:\n  r: {from: ghost.outputs.o}\n",
	"E_GRAPH_OUTPUT_SOURCE_OUTPUT_NOT_FOUND": corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "") + "edges: []\noutputs:\n  r: {from: a.outputs.nope}\n",
	"E_REQUIRED_INPUT_UNBOUND":               corpusHead + "inputs: {}\nnodes:\n" + nodeShell("b", "    inputs:\n      i: {type: string, required: true}\n") + "edges: []\noutputs: {}\n",
	"E_INPUT_MULTIPLE_PROVIDERS": corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    outputs:\n      o: {type: string}\n") + nodeShell("c", "    outputs:\n      o: {type: string}\n") + nodeShell("b", "    inputs:\n      i: {type: string}\n") +
		"edges:\n  - {from: a.outputs.o, to: b.inputs.i}\n  - {from: c.outputs.o, to: b.inputs.i}\noutputs: {}\n",
	"E_GRAPH_CYCLE": corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    inputs:\n      i: {type: string}\n    outputs:\n      o: {type: string}\n") + nodeShell("b", "    inputs:\n      i: {type: string}\n    outputs:\n      o: {type: string}\n") +
		"edges:\n  - {from: a.outputs.o, to: b.inputs.i}\n  - {from: b.outputs.o, to: a.inputs.i}\noutputs: {}\n",
	"W_DECLARED_OUTPUT_UNUSED": corpusHead + "inputs: {}\nnodes:\n" + nodeShell("a", "    outputs:\n      o: {type: string}\n") + "edges: []\noutputs: {}\n",
	"E_UNIT_INVALID":           corpusHead + "inputs: {}\nunits: [a]\nnodes:\n" + nodeShell("a", "") + "edges: []\noutputs: {}\n",
	"E_HUMAN_INVALID":          corpusHead + "inputs: {}\nnodes:\n  h:\n    type: human\nedges: []\noutputs: {}\n",
	"E_LOOP_INVALID":           corpusHead + "inputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: /usr/bin/true}\n    loop:\n      max_attempts: 2\n      gate:\n        config: {check_id: c, kind: json_predicate}\n      bindings: []\nedges: []\noutputs: {}\n",
	"E_NODE_CONFIG_INVALID":    corpusHead + "inputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: \"\"}\nedges: []\noutputs: {}\n",
}

// runtimeOnly are codes of the value phase (run start and resolution) or of
// unreachable guards; static validation never produces them.
var runtimeOnly = map[string]bool{
	"E_DEFINITION_NIL": true, "E_BINDING_PLAN_NIL": true, "E_RESOLUTION_NODE_NOT_FOUND": true,
	"E_GRAPH_INPUT_VALUE_MISSING": true, "E_GRAPH_INPUT_VALUE_TYPE_MISMATCH": true, "E_GRAPH_INPUT_VALUE_UNKNOWN": true,
	"E_BOUND_OUTPUT_VALUE_MISSING": true, "E_BOUND_VALUE_TYPE_MISMATCH": true,
	"E_GRAPH_OUTPUT_VALUE_MISSING": true, "E_GRAPH_OUTPUT_VALUE_TYPE_MISMATCH": true,
	"E_CLI_INPUT_VALUE_INVALID": true, "E_HUMAN_NO_PROVIDER": true,
}

func TestEveryCorpusGraphProducesItsCodeAndTheAPIEqualsTheCLI(t *testing.T) {
	s := startServe(t)
	defer s.closeStdin()
	dir := t.TempDir()
	codes := make([]string, 0, len(corpus))
	for code := range corpus {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		text := corpus[code]
		path := filepath.Join(dir, strings.ToLower(code)+".yaml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		exit, out, _ := cli(t, "graph", "validate", path, "--json")
		var cliDoc struct {
			Status      string           `json:"status"`
			Diagnostics []map[string]any `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(out), &cliDoc); err != nil {
			t.Fatalf("%s: CLI output is not JSON: %s", code, out)
		}
		status, body := s.call(t, "POST", "/graphs/validate", `{"text":`+quote(text)+`}`)
		var apiDoc struct {
			Status      string           `json:"status"`
			Diagnostics []map[string]any `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(body), &apiDoc); err != nil || status != 200 {
			t.Fatalf("%s: API %d %s", code, status, body)
		}
		found := false
		for _, d := range cliDoc.Diagnostics {
			found = found || d["code"] == code
		}
		if !found {
			t.Errorf("%s: the corpus graph does not produce it; the CLI reported %v", code, cliDoc.Diagnostics)
		}
		for _, d := range apiDoc.Diagnostics {
			delete(d, "line") // an API-only field
		}
		if !reflect.DeepEqual(apiDoc.Diagnostics, cliDoc.Diagnostics) {
			t.Errorf("%s: API and CLI differ\n api: %v\n cli: %v", code, apiDoc.Diagnostics, cliDoc.Diagnostics)
		}
		wantExit := 0
		if strings.HasPrefix(code, "E_") {
			wantExit = exit
		}
		if (exit == 0) != (apiDoc.Status == "valid") && wantExit != 0 {
			t.Errorf("%s: CLI exit %d but API status %q", code, exit, apiDoc.Status)
		}
	}
}

// Codes built from a prefix at run time cannot be found by a literal scan, so
// they are listed; the test checks their suffixes still exist in the source.
var derived = []string{
	"E_EDGE_SOURCE_NODE_NOT_FOUND", "E_EDGE_SOURCE_OUTPUT_NOT_FOUND",
	"E_EDGE_TARGET_NODE_NOT_FOUND", "E_EDGE_TARGET_INPUT_NOT_FOUND",
}

func TestNoStaticCodeIsMissingFromTheCorpus(t *testing.T) {
	re := regexp.MustCompile(`"([EW]_[A-Z0-9_]+)"`)
	files, _ := filepath.Glob("../../internal/graph/*.go")
	files = append(files, "validation.go")
	seen := map[string]bool{}
	var source strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(data)
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			seen[m[1]] = true
		}
	}
	for _, c := range derived {
		seen[c] = true
	}
	for _, suffix := range []string{"_NODE_NOT_FOUND", "_OUTPUT_NOT_FOUND", "_INPUT_NOT_FOUND"} {
		if !strings.Contains(source.String(), `"`+suffix+`"`) && !strings.Contains(source.String(), `+"`+suffix+`"`) {
			t.Errorf("the derived suffix %s is no longer in the validator: update the derived list", suffix)
		}
	}
	// Prefixes passed to helpers are not codes themselves.
	for _, prefix := range []string{"E_EDGE_SOURCE", "E_EDGE_TARGET"} {
		delete(seen, prefix)
	}
	var missing []string
	for c := range seen {
		if _, ok := corpus[c]; !ok && !runtimeOnly[c] {
			missing = append(missing, c)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("codes with no corpus graph (add one, or list it as runtime-only): %v", missing)
	}
	for c := range corpus {
		if !seen[c] {
			t.Errorf("corpus entry %s is not a code of the validator any more", c)
		}
	}
}
