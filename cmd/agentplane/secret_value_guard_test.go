package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/testenv"
)

// guardNode assigns the secret to a node that records that it ran, so a test
// can prove no process started. extra is spliced into the node's config.
func guardNode(marker, extra string) string {
	return "  a:\n    type: shell\n    config: {command: /bin/sh, args: [-c, 'echo ran > \"$0\"', " + marker + "], secret_env: {GH_TOKEN: tok}" + extra + "}\n"
}

func guardGraphText(inputs, node string) string {
	return corpusHead + "secrets:\n  tok: {secret_ref: env/" + deliveryEnvName + "}\ninputs: " + inputs + "\nnodes:\n" + node + "edges: []\noutputs: {}\n"
}

// yamlEscaped is deliveryValue with its first letter written as a YAML escape,
// so a byte search of the file cannot find it.
func yamlEscaped(escape string) string { return escape + deliveryValue[1:] }

// requireGuardRefusal asserts the refusal contract of the guard: exit 3, the
// diagnostic code, the secret name and location, no value anywhere, no process,
// and no run in the store.
func requireGuardRefusal(t *testing.T, exit int, out, errout, code, location, marker, store string) {
	t.Helper()
	if exit != 3 {
		t.Fatalf("exit=%d, want 3; stdout=%s stderr=%s", exit, out, errout)
	}
	shown := out + errout
	for _, want := range []string{code, `"tok"`, location} {
		if !strings.Contains(shown, want) {
			t.Errorf("output lacks %q: %s", want, shown)
		}
	}
	testenv.AssertNoSecretText(t, "CLI output", shown, deliveryValue)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a node ran although preflight refused the run")
	}
	if store != "" {
		if entries, _ := os.ReadDir(store); len(entries) != 0 {
			t.Errorf("a refused run left store entries: %v", entries)
		}
		testenv.AssertNoSecretBytes(t, store, deliveryValue)
	}
}

func TestGraphEmbeddingResolvedValueRejectedBeforeStoreCopy(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	hidden := yamlEscaped(`\x63`)
	cases := map[string]string{
		"literal in args":      "args: [-c, 'true', " + deliveryValue + "]",
		"yaml hex escape":      `args: [-c, 'true', "` + hidden + `"]`,
		"yaml unicode escape":  `args: [-c, 'true', "` + yamlEscaped(`\u0063`) + `"]`,
		"folded double quoted": "args: [-c, 'true', \"c44-Canary_\\\n    " + deliveryValue[len("c44-Canary_"):] + "\"]",
		"env value":            `env: {PLAIN: "` + hidden + `"}`,
		"env key":              `env: {"` + hidden + `": x}`,
	}
	for name, extra := range cases {
		for _, withStore := range []bool{true, false} {
			t.Run(name+map[bool]string{true: " with store", false: " without store"}[withStore], func(t *testing.T) {
				t.Setenv(deliveryEnvName, deliveryValue)
				marker := filepath.Join(t.TempDir(), "ran")
				node := guardNode(marker, "")
				if strings.HasPrefix(extra, "args:") {
					node = strings.Replace(node, "args: [-c, 'echo ran > \"$0\"', "+marker+"]", extra, 1)
				} else {
					node = guardNode(marker, ", "+extra)
				}
				args := []string{"run", writeGraph(t, guardGraphText("{}", node))}
				store := ""
				if withStore {
					store = t.TempDir()
					args = append(args, "--store-dir", store)
				}
				exit, out, errout := cli(t, args...)
				requireGuardRefusal(t, exit, out, errout, "secret_value_in_graph", "graph", marker, store)
			})
		}
	}
}

func TestGraphCommentHoldingResolvedValueIsRejected(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
	path := writeGraph(t, "# token "+deliveryValue+"\n"+guardGraphText("{}", guardNode(marker, "")))
	exit, out, errout := cli(t, "run", path, "--store-dir", store)
	requireGuardRefusal(t, exit, out, errout, "secret_value_in_graph", "graph", marker, store)
}

func TestGraphWithoutTheResolvedValueRunsAndStoresNothingSecret(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
	exit, out, errout := cli(t, "run", writeGraph(t, guardGraphText("{}", guardNode(marker, ""))), "--store-dir", store)
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	testenv.AssertNoSecretBytes(t, store, deliveryValue)
}

func inputGraph(marker string) string {
	return guardGraphText("{token: {type: string}, blob: {type: json}}", guardNode(marker, ""))
}

func TestInputHoldingResolvedValueRejected(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	cases := map[string]struct {
		inputs   []string
		location string
	}{
		"string input":      {[]string{"token=" + deliveryValue}, "inputs.token"},
		"inside a string":   {[]string{"token=bearer " + deliveryValue + " x"}, "inputs.token"},
		"json escaped leaf": {[]string{`blob={"k":["` + yamlEscaped(`\u0063`) + `"]}`}, "inputs.blob"},
		"json object key":   {[]string{`blob={"` + deliveryValue + `":1}`}, "inputs.blob"},
	}
	// Undeclared inputs never reach the guard: validation refuses them first.
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(deliveryEnvName, deliveryValue)
			marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
			args := []string{"run", writeGraph(t, inputGraph(marker)), "--store-dir", store}
			for _, input := range tc.inputs {
				args = append(args, "--input", input)
			}
			exit, out, errout := cli(t, args...)
			requireGuardRefusal(t, exit, out, errout, "secret_value_in_input", tc.location, marker, store)
		})
	}
}

func TestInputWithoutTheResolvedValueRunsAndRecordsNoSecret(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
	exit, out, errout := cli(t, "run", writeGraph(t, inputGraph(marker)), "--store-dir", store, "--record-values", "--input", "token=public", "--input", `blob={"k":"v"}`)
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	testenv.AssertNoSecretBytes(t, store, deliveryValue)
}

// A refused input is refused before any value_recorded event, so recorded graph
// inputs can never hold a resolved secret.
func TestRecordedGraphInputsNeverHoldSecret(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
	exit, out, errout := cli(t, "run", writeGraph(t, inputGraph(marker)), "--store-dir", store, "--record-values", "--input", "token="+deliveryValue)
	requireGuardRefusal(t, exit, out, errout, "secret_value_in_input", "inputs.token", marker, store)
}

func TestResolvedValueGuardIsIndependentOfStoreDir(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker := filepath.Join(t.TempDir(), "ran")
	exit, out, errout := cli(t, "run", writeGraph(t, inputGraph(marker)), "--input", "token="+deliveryValue)
	requireGuardRefusal(t, exit, out, errout, "secret_value_in_input", "inputs.token", marker, "")
}

func TestServeInputHoldingResolvedValueIsRefusedWith422(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker := filepath.Join(t.TempDir(), "ran")
	s := startServe(t)
	body, _ := json.Marshal(map[string]any{"graph_path": writeGraph(t, inputGraph(marker)), "inputs": []string{"token=" + deliveryValue}})
	code, response := s.call(t, "POST", "/runs", string(body))
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("%d %s", code, response)
	}
	for _, want := range []string{"secret_value_in_input", "tok", "inputs.token"} {
		if !strings.Contains(response, want) {
			t.Errorf("response lacks %q: %s", want, response)
		}
	}
	testenv.AssertNoSecretText(t, "API response", response, deliveryValue)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a node ran")
	}
	s.closeStdin()
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Errorf("a refused run left store entries: %v", entries)
	}
}

func TestServeGraphHoldingResolvedValueIsRefusedWith422(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	marker := filepath.Join(t.TempDir(), "ran")
	path := writeGraph(t, guardGraphText("{}", guardNode(marker, ", env: {PLAIN: \""+yamlEscaped(`\x63`)+"\"}")))
	s := startServe(t)
	code, response := s.call(t, "POST", "/runs", `{"graph_path":`+quote(path)+`}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(response, "secret_value_in_graph") {
		t.Fatalf("%d %s", code, response)
	}
	testenv.AssertNoSecretText(t, "API response", response, deliveryValue)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a node ran")
	}
}

// --redact-env keeps its behaviour: it is not compared with --input and its
// graph check stays conditional on --store-dir.
func TestRedactEnvValueAsInputStillPassesAndIsRedacted(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv("AGENTPLANE_C46_REDACT", deliveryValue)
	marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
	node := "  a:\n    type: shell\n    config: {command: /bin/sh, args: [-c, 'echo ran > \"$0\"', " + marker + "]}\n"
	path := writeGraph(t, corpusHead+"inputs: {token: {type: string}}\nnodes:\n"+node+"edges: []\noutputs: {}\n")
	exit, out, errout := cli(t, "run", path, "--redact-env", "AGENTPLANE_C46_REDACT", "--store-dir", store, "--record-values", "--input", "token="+deliveryValue)
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	testenv.AssertNoSecretBytes(t, store, deliveryValue)
}

func TestRedactEnvGraphCheckStaysConditionalOnStoreDir(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv("AGENTPLANE_C46_REDACT", deliveryValue)
	marker := filepath.Join(t.TempDir(), "ran")
	node := "  a:\n    type: shell\n    config: {command: /bin/sh, args: [-c, 'echo ran > \"$0\"', " + marker + ", " + deliveryValue + "]}\n"
	path := writeGraph(t, corpusHead+"inputs: {}\nnodes:\n"+node+"edges: []\noutputs: {}\n")
	if exit, out, errout := cli(t, "run", path, "--redact-env", "AGENTPLANE_C46_REDACT"); exit != 0 {
		t.Fatalf("without a store the graph must still run: exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	exit, _, errout := cli(t, "run", path, "--redact-env", "AGENTPLANE_C46_REDACT", "--store-dir", t.TempDir())
	if exit != 3 || !strings.Contains(errout, errGraphHoldsSecret.Error()) {
		t.Fatalf("exit=%d stderr=%s", exit, errout)
	}
}

func TestStoredMaterialHoldsKeysAndValuesOfTheDefinition(t *testing.T) {
	def := parseGraphText(t, guardGraphText("{}", guardNode("/tmp/m", ", env: {\"keyed-secret\": x}")))
	holds := func(needle string) bool {
		return len(storedMaterial{def: def}.Locations(func(text string) bool { return strings.Contains(text, needle) })) > 0
	}
	for _, needle := range []string{"keyed-secret", "GH_TOKEN", "env/" + deliveryEnvName, "/tmp/m"} {
		if !holds(needle) {
			t.Errorf("the definition walk missed %q", needle)
		}
	}
	if holds("absent-everywhere") {
		t.Error("false positive on a clean definition")
	}
}

func TestValueStringsCoverTypedValues(t *testing.T) {
	jsonValue, err := graph.NewJSONValue([]byte(`{"k":["\u0061bc",{"deep":"x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	texts := strings.Join(valueStrings(jsonValue), "|")
	for _, want := range []string{"abc", "deep", "x", "k"} {
		if !strings.Contains(texts, want) {
			t.Errorf("valueStrings missed %q in %q", want, texts)
		}
	}
	if got := valueStrings(graph.NewBooleanValue(true)); len(got) != 0 {
		t.Errorf("boolean carries no text, got %v", got)
	}
}
