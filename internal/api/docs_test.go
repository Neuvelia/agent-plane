package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
)

const pairGraph = `schema_version: 0
id: pair
inputs: {}

nodes:
  # the producer
  a:
    type: shell
    config:
      command: "true"
    outputs:
      stdout: {type: string}

  b:
    type: shell
    config:
      command: "true"
    inputs:
      text: {type: string}

edges: []
outputs: {}
`

func docServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	s, ts := newServer(t, nil)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s, ts, dir
}

func call(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, text := do(t, ts, method, path, string(raw), true)
	var out map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	return resp.StatusCode, out
}

func writeGraph(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func grantFor(t *testing.T, ts *httptest.Server, path, mode string) string {
	t.Helper()
	status, out := call(t, ts, "POST", "/graphs/grants", map[string]string{"path": path, "mode": mode})
	if status != http.StatusCreated {
		t.Fatalf("grant %s %s: %d %v", mode, path, status, out)
	}
	return out["grant"].(string)
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestDocumentEndpointsRequireTheToken(t *testing.T) {
	_, ts, _ := docServer(t)
	for _, c := range [][2]string{{"POST", "/graphs/grants"}, {"POST", "/graphs/open"}, {"POST", "/graphs/stat"}, {"POST", "/graphs/edit"}, {"PUT", "/graphs/save"}} {
		if resp, _ := do(t, ts, c[0], c[1], "{}", false); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without token: %d", c[0], c[1], resp.StatusCode)
		}
	}
}

func TestGrantsAcceptOnlyWhatMayBeEdited(t *testing.T) {
	_, ts, dir := docServer(t)
	good := writeGraph(t, dir, "good.yaml", pairGraph)
	cases := []struct {
		name string
		body map[string]string
		want int
		code string
	}{
		{"relative path", map[string]string{"path": "good.yaml", "mode": "open"}, 400, "bad_request"},
		{"no mode", map[string]string{"path": good}, 400, "bad_request"},
		{"not yaml", map[string]string{"path": filepath.Join(dir, "x.txt"), "mode": "create"}, 422, "not_yaml"},
		{"missing directory", map[string]string{"path": filepath.Join(dir, "nope", "x.yaml"), "mode": "create"}, 422, "bad_directory"},
		{"open a missing file", map[string]string{"path": filepath.Join(dir, "gone.yaml"), "mode": "open"}, 404, "not_found"},
		{"create over a file", map[string]string{"path": good, "mode": "create"}, 409, "exists"},
		{"open a directory", map[string]string{"path": writeDir(t, dir, "d.yaml"), "mode": "open"}, 422, "not_regular"},
	}
	for _, c := range cases {
		status, out := call(t, ts, "POST", "/graphs/grants", c.body)
		if status != c.want || errCode(out) != c.code {
			t.Errorf("%s: %d %v, want %d %s", c.name, status, out, c.want, c.code)
		}
	}
	if status, out := call(t, ts, "POST", "/graphs/grants", map[string]string{"path": good, "mode": "open"}); status != 201 || len(out["grant"].(string)) != 32 || out["label"] != "good.yaml" {
		t.Errorf("a valid grant: %d %v", status, out)
	}
}

func writeDir(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGrantsRefuseASymlinkFinalComponent(t *testing.T) {
	_, ts, dir := docServer(t)
	target := writeGraph(t, dir, "real.yaml", pairGraph)
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	if status, out := call(t, ts, "POST", "/graphs/grants", map[string]string{"path": link, "mode": "open"}); status != 422 || errCode(out) != "not_regular" {
		t.Errorf("symlink opened: %d %v", status, out)
	}
	if status, out := call(t, ts, "POST", "/graphs/grants", map[string]string{"path": link, "mode": "create"}); status != 409 {
		t.Errorf("a symlink counts as existing for create: %d %v", status, out)
	}
}

func TestGrantCountIsBounded(t *testing.T) {
	_, ts, dir := docServer(t)
	p := writeGraph(t, dir, "g.yaml", pairGraph)
	for i := 0; i < MaxGrants; i++ {
		grantFor(t, ts, p, "open")
	}
	if status, out := call(t, ts, "POST", "/graphs/grants", map[string]string{"path": p, "mode": "open"}); status != 409 || errCode(out) != "too_many_grants" {
		t.Errorf("grant %d: %d %v", MaxGrants+1, status, out)
	}
}

func TestOpenReturnsTheDocument(t *testing.T) {
	_, ts, dir := docServer(t)
	g := grantFor(t, ts, writeGraph(t, dir, "pair.yaml", pairGraph), "open")
	status, out := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	if status != 200 {
		t.Fatalf("open: %d %v", status, out)
	}
	if out["text"] != pairGraph || out["hash"] != hashOf([]byte(pairGraph)) || out["label"] != "pair.yaml" || out["line_ending"] != "lf" || out["readonly_canvas"] != false {
		t.Errorf("document header: %v", out)
	}
	if v, _ := out["version"].(string); !strings.HasPrefix(v, "sha256-v0:") {
		t.Errorf("version %q", v)
	}
	def := out["definition"].(map[string]any)
	nodes := def["nodes"].(map[string]any)
	if len(nodes) != 2 || nodes["a"].(map[string]any)["type"] != "shell" || def["id"] != "pair" {
		t.Errorf("definition DTO: %v", def)
	}
	cfg := nodes["a"].(map[string]any)["config"].(map[string]any)
	if cfg["command"] != "true" {
		t.Errorf("config: %v", cfg)
	}
	if diags, ok := out["diagnostics"].([]any); !ok || len(diags) == 0 {
		t.Errorf("an unconsumed output should produce a warning, got %v", out["diagnostics"])
	}
}

func TestOpenSaysWhyACanvasIsReadOnly(t *testing.T) {
	_, ts, dir := docServer(t)
	hostile, err := os.ReadFile("../adapter/yamlgraph/edit/testdata/hostile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, out := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": grantFor(t, ts, writeGraph(t, dir, "h.yaml", string(hostile)), "open")})
	if out["readonly_canvas"] != true || len(out["readonly_reasons"].([]any)) != 3 {
		t.Errorf("anchors: %v", out)
	}
	_, out = call(t, ts, "POST", "/graphs/open", map[string]string{"grant": grantFor(t, ts, writeGraph(t, dir, "bad.yaml", "schema_version: 0\nid: [\n"), "open")})
	if out["readonly_canvas"] != true || out["parse_error"] == nil || out["definition"] != nil {
		t.Errorf("unparseable file: %v", out)
	}
	if out["text"] != "schema_version: 0\nid: [\n" {
		t.Errorf("the text of an unparseable file must still be returned")
	}
}

func TestOpenRefusesNonTextAndHugeFiles(t *testing.T) {
	_, ts, dir := docServer(t)
	bin := grantFor(t, ts, writeGraph(t, dir, "bin.yaml", "id: \xff\xfe\n"), "open")
	if status, out := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": bin}); status != 422 || errCode(out) != "not_text" {
		t.Errorf("binary: %d %v", status, out)
	}
	huge := writeGraph(t, dir, "huge.yaml", strings.Repeat("# x\n", MaxDocumentBytes/4+10))
	if status, out := call(t, ts, "POST", "/graphs/grants", map[string]string{"path": huge, "mode": "open"}); status != 413 {
		t.Errorf("oversize file granted: %d %v", status, out)
	}
	if status, out := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": "nope"}); status != 404 || errCode(out) != "unknown_grant" {
		t.Errorf("unknown grant: %d %v", status, out)
	}
}

func TestFullValidationIsUsedForDocuments(t *testing.T) {
	s, ts, dir := docServer(t)
	s.ValidateDefinition = func(d *graph.Definition) []graph.Diagnostic {
		return []graph.Diagnostic{{Severity: graph.SeverityError, Code: "E_NODE_CONFIG_INVALID", Message: "stub", GraphID: d.ID}}
	}
	g := grantFor(t, ts, writeGraph(t, dir, "pair.yaml", pairGraph), "open")
	_, out := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	if d := out["diagnostics"].([]any); len(d) != 1 || d[0].(map[string]any)["code"] != "E_NODE_CONFIG_INVALID" {
		t.Errorf("open did not use the full validation: %v", out["diagnostics"])
	}
	_, out = call(t, ts, "POST", "/graphs/validate", map[string]any{"text": pairGraph})
	if out["status"] != "invalid" || len(out["diagnostics"].([]any)) != 1 {
		t.Errorf("validate(text): %v", out)
	}
}

func TestDiagnosticsCarryTheSourceLine(t *testing.T) {
	_, ts, dir := docServer(t)
	g := grantFor(t, ts, writeGraph(t, dir, "lines.yaml", pairGraph), "open")
	for name, out := range map[string]map[string]any{
		"validate": func() map[string]any {
			_, o := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": pairGraph})
			return o
		}(),
		"open": func() map[string]any {
			_, o := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
			return o
		}(),
	} {
		diags := out["diagnostics"].([]any)
		if len(diags) == 0 {
			t.Fatalf("%s: no diagnostics", name)
		}
		line, _ := diags[0].(map[string]any)["line"].(float64)
		want := float64(1 + strings.Count(pairGraph[:strings.Index(pairGraph, "stdout")], "\n"))
		if line != want {
			t.Errorf("%s: line %v, want %v (the stdout port)", name, line, want)
		}
	}
	_, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": "schema_version: 0\nid: g\nnodes: [\n"})
	if out["parse_line"] == nil {
		t.Errorf("a parse error carries its line: %v", out)
	}
}

func TestParseErrorColumnIsReportedOnlyWhereTheLibraryLocatesIt(t *testing.T) {
	_, ts, dir := docServer(t)
	syntax := "schema_version: 0\nid: g\nnodes: [\n"
	unknown := "schema_version: 0\nid: g\nnodes:\n  a:\n    type: shell\n    colour: red\n"
	path := writeGraph(t, dir, "unknown.yaml", unknown)
	g := grantFor(t, ts, path, "open")

	_, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": syntax})
	if out["parse_line"] == nil {
		t.Errorf("syntax error: no line: %v", out)
	}
	if _, has := out["parse_column"]; has {
		t.Errorf("syntax error: the library reports no column, so none may be invented: %v", out)
	}
	_, out = call(t, ts, "POST", "/graphs/validate", map[string]any{"text": unknown})
	if out["parse_line"] != float64(6) || out["parse_column"] != float64(5) {
		t.Errorf("unknown field: line %v column %v, want 6 and 5", out["parse_line"], out["parse_column"])
	}
	_, out = call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	if out["parse_line"] != float64(6) || out["parse_column"] != float64(5) {
		t.Errorf("open: line %v column %v, want 6 and 5", out["parse_line"], out["parse_column"])
	}
}

func TestValidateTakesTextOrGrantOrPathExactlyOnce(t *testing.T) {
	_, ts, dir := docServer(t)
	path := writeGraph(t, dir, "pair.yaml", pairGraph)
	g := grantFor(t, ts, path, "open")
	if _, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": pairGraph}); out["status"] != "valid" || out["version"] == nil {
		t.Errorf("text: %v", out)
	}
	if _, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": "schema_version: 0\nid: ["}); out["status"] != "invalid" || out["parse_error"] == nil {
		t.Errorf("broken text: %v", out)
	}
	// The canvas follows typed YAML: validate returns the definition, and flags
	// files whose canvas must stay read-only.
	_, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": pairGraph})
	if def, ok := out["definition"].(map[string]any); !ok || def["id"] != "pair" || out["readonly_canvas"] != nil {
		t.Errorf("validate must return the definition: %v", out)
	}
	if _, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": "schema_version: 0\nid: ["}); out["definition"] != nil {
		t.Errorf("a broken text has no definition: %v", out)
	}
	anchored := "x: &a 1\ny: *a\n" + pairGraph
	if _, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": anchored}); out["readonly_canvas"] != true || out["readonly_reasons"] == nil {
		t.Errorf("anchors must make the canvas read-only: %v", out)
	}
	if _, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"grant": g}); out["status"] != "valid" {
		t.Errorf("grant: %v", out)
	}
	if status, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"path": path}); status != 200 || out["status"] != "valid" {
		t.Errorf("legacy path: %d %v", status, out)
	}
	for _, body := range []map[string]any{{}, {"text": "x", "grant": g}, {"path": path, "text": "x"}} {
		if status, _ := call(t, ts, "POST", "/graphs/validate", body); status != 400 {
			t.Errorf("%v: status %d, want 400", body, status)
		}
	}
	big := strings.Repeat("x", MaxDocumentBytes+1)
	if status, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": big}); status != 413 || errCode(out) != "too_large" {
		t.Errorf("oversize text: %d %v", status, out)
	}
	if status, out := call(t, ts, "POST", "/graphs/validate", map[string]any{"text": "a\x00b"}); status != 422 || errCode(out) != "not_text" {
		t.Errorf("NUL: %d %v", status, out)
	}
}

func TestEditAppliesOperationsWithoutWriting(t *testing.T) {
	_, ts, dir := docServer(t)
	path := writeGraph(t, dir, "pair.yaml", pairGraph)
	g := grantFor(t, ts, path, "open")
	_, doc := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	hash := doc["hash"].(string)

	ops := json.RawMessage(`[{"op":"add_node","id":"extra","type":"shell"},{"op":"can_connect","from":"a.outputs.stdout","to":"b.inputs.text"}]`)
	status, out := call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": hash, "ops_version": 1, "ops": ops})
	if status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}
	if !strings.Contains(out["text"].(string), "  extra:\n    type: shell") || out["hash"] == hash {
		t.Errorf("edit result: %v", out["text"])
	}
	if res := out["results"].([]any); len(res) != 2 || res[1].(map[string]any)["query"].(map[string]any)["ok"] != true {
		t.Errorf("results: %v", out["results"])
	}
	if disk, _ := os.ReadFile(path); string(disk) != pairGraph {
		t.Errorf("edit wrote the file")
	}
	// The server holds the edited text: the next edit continues from it.
	next := out["hash"].(string)
	status, out = call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": next, "ops_version": 1, "ops": json.RawMessage(`[{"op":"rename_node","id":"extra","new_id":"renamed"}]`)})
	if status != 200 || !strings.Contains(out["text"].(string), "  renamed:") {
		t.Errorf("second edit: %d %v", status, out)
	}
	// An old hash is stale, and says what the current one is.
	status, out = call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": hash, "ops_version": 1, "ops": json.RawMessage(`[]`)})
	if status != 409 || errCode(out) != "stale_base" || out["current_hash"] == "" {
		t.Errorf("stale base: %d %v", status, out)
	}
}

func TestEditGraphDataOperationsRoundTrip(t *testing.T) {
	_, ts, dir := docServer(t)
	g := grantFor(t, ts, writeGraph(t, dir, "pair.yaml", pairGraph), "open")
	_, doc := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	hash := doc["hash"].(string)
	send := func(hash, ops string) (int, map[string]any) {
		return call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": hash, "ops_version": 1, "ops": json.RawMessage(ops)})
	}

	status, out := send(hash, `[{"op":"add_graph_input","id":"words","type":"string","required":true},{"op":"add_graph_input_binding","from":"inputs.words","to":"b.inputs.text"},{"op":"set_graph_output","id":"shout","from":"a.outputs.stdout"}]`)
	if status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}
	text := out["text"].(string)
	for _, want := range []string{"  words: {type: string, required: true}", "graph_input_bindings:", "    to: b.inputs.text", "  shout:", "    from: a.outputs.stdout"} {
		if !strings.Contains(text, want) {
			t.Errorf("edited text lacks %q:\n%s", want, text)
		}
	}
	def := out["definition"].(map[string]any)
	if def["inputs"] == nil || def["graph_input_bindings"] == nil || def["outputs"] == nil {
		t.Errorf("definition lacks the new data: %v", def)
	}
	next := out["hash"].(string)

	// list_connections is a query: it answers and changes nothing.
	status, out = send(next, `[{"op":"list_connections","to":"b.inputs.text"}]`)
	if status != 200 || out["hash"] != next {
		t.Fatalf("query: %d %v", status, out)
	}
	query := out["results"].([]any)[0].(map[string]any)["query"].(map[string]any)
	options := query["options"].([]any)
	first := options[0].(map[string]any)
	if query["ok"] != true || first["ref"] != "inputs.words" || first["ok"] != false || first["code"] != "E_INPUT_MULTIPLE_PROVIDERS" || first["message"] == "" {
		t.Errorf("options: %v", options)
	}

	// A refused operation is the F2.1 error shape and keeps the held text.
	status, out = send(next, `[{"op":"remove_graph_input_binding","from":"inputs.words","to":"a.inputs.zzz"}]`)
	detail, _ := out["detail"].(map[string]any)
	if status != 422 || errCode(out) != "E_EDIT_BINDING_NOT_FOUND" || detail["op_index"] != float64(0) {
		t.Errorf("refusal: %d %v", status, out)
	}
	// A stale base hash is handled as before.
	if status, out = send(hash, `[]`); status != 409 || errCode(out) != "stale_base" {
		t.Errorf("stale base: %d %v", status, out)
	}
	if status, out = send(next, `[{"op":"list_connections"}]`); status != 422 || errCode(out) != "E_EDIT_INVALID_OP" {
		t.Errorf("list_connections without from or to: %d %v", status, out)
	}
}

func TestEditCanStartFromTextTheClientTyped(t *testing.T) {
	_, ts, dir := docServer(t)
	g := grantFor(t, ts, writeGraph(t, dir, "pair.yaml", pairGraph), "open")
	call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	typed := strings.Replace(pairGraph, "id: pair", "id: typed", 1)
	status, out := call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": hashOf([]byte(typed)), "text": typed, "ops_version": 1, "ops": json.RawMessage(`[{"op":"set_name","name":"N"}]`)})
	if status != 200 || !strings.Contains(out["text"].(string), "id: typed") || !strings.Contains(out["text"].(string), "name: N") {
		t.Errorf("edit from typed text: %d %v", status, out)
	}
	status, out = call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": g, "base_hash": "0000", "text": typed, "ops_version": 1, "ops": json.RawMessage(`[]`)})
	if status != 400 || errCode(out) != "hash_mismatch" {
		t.Errorf("a text that does not match its hash: %d %v", status, out)
	}
}

func TestEditRefusalsAreStructured(t *testing.T) {
	_, ts, dir := docServer(t)
	g := grantFor(t, ts, writeGraph(t, dir, "pair.yaml", pairGraph), "open")
	_, doc := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	base := map[string]any{"grant": g, "base_hash": doc["hash"], "ops_version": 1}
	with := func(ops string) map[string]any {
		b := map[string]any{}
		for k, v := range base {
			b[k] = v
		}
		b["ops"] = json.RawMessage(ops)
		return b
	}
	status, out := call(t, ts, "POST", "/graphs/edit", with(`[{"op":"add_node","id":"ok","type":"shell"},{"op":"remove_node","id":"zzz"}]`))
	detail, _ := out["detail"].(map[string]any)
	if status != 422 || errCode(out) != "E_EDIT_NODE_NOT_FOUND" || detail["op_index"] != float64(1) || detail["node"] != "zzz" {
		t.Errorf("refusal: %d %v", status, out)
	}
	if status, out := call(t, ts, "POST", "/graphs/edit", with(`[{"op":"explode"}]`)); status != 422 || errCode(out) != "E_EDIT_UNKNOWN_OP" {
		t.Errorf("unknown op: %d %v", status, out)
	}
	bad := with(`[]`)
	bad["ops_version"] = 2
	if status, out := call(t, ts, "POST", "/graphs/edit", bad); status != 400 || errCode(out) != "unsupported_ops_version" {
		t.Errorf("version: %d %v", status, out)
	}
	// Nothing from the failed batch was kept.
	status, out = call(t, ts, "POST", "/graphs/edit", with(`[]`))
	if status != 200 || out["hash"] != doc["hash"] {
		t.Errorf("a refused batch changed the held text: %d %v", status, out)
	}
	// A graph that was only granted for creation has no text yet.
	created := grantFor(t, ts, filepath.Join(dir, "new.yaml"), "create")
	if status, out := call(t, ts, "POST", "/graphs/edit", map[string]any{"grant": created, "base_hash": "x", "ops_version": 1, "ops": json.RawMessage(`[]`)}); status != 400 {
		t.Errorf("edit before open or text: %d %v", status, out)
	}
}

func TestSaveCreatesThenReplacesWithConflictDetection(t *testing.T) {
	_, ts, dir := docServer(t)
	path := filepath.Join(dir, "new.yaml")
	g := grantFor(t, ts, path, "create")
	if _, out := call(t, ts, "POST", "/graphs/stat", map[string]string{"grant": g}); out["exists"] != false {
		t.Errorf("stat before save: %v", out)
	}
	status, out := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": nil, "text": pairGraph})
	if status != 200 || out["hash"] != hashOf([]byte(pairGraph)) {
		t.Fatalf("create: %d %v", status, out)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("a new graph file is 0644, got %v %v", info, err)
	}
	if _, out := call(t, ts, "POST", "/graphs/stat", map[string]string{"grant": g}); out["exists"] != true || out["hash"] != hashOf([]byte(pairGraph)) {
		t.Errorf("stat after save: %v", out)
	}
	// Overwrite needs the hash of the version last seen.
	next := strings.Replace(pairGraph, "id: pair", "id: other", 1)
	hash := out["hash"].(string)
	if status, out := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": nil, "text": next}); status != 409 || errCode(out) != "exists" {
		t.Errorf("overwrite without a hash: %d %v", status, out)
	}
	if status, _ := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": hash, "text": next}); status != 200 {
		t.Errorf("overwrite with the right hash: %d", status)
	}
	// Someone else changed the file: the save is refused and the file is theirs.
	if err := os.WriteFile(path, []byte("# edited elsewhere\n"+pairGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	status, out = call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": hashOf([]byte(next)), "text": "mine"})
	if status != 409 || errCode(out) != "conflict" || out["current_hash"] != hashOf([]byte("# edited elsewhere\n"+pairGraph)) {
		t.Errorf("conflict: %d %v", status, out)
	}
	if disk, _ := os.ReadFile(path); !strings.HasPrefix(string(disk), "# edited elsewhere") {
		t.Errorf("a conflicting save overwrote the file")
	}
	// A file that vanished.
	_ = os.Remove(path)
	if status, out := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": hash, "text": next}); status != 409 || errCode(out) != "gone" {
		t.Errorf("vanished file: %d %v", status, out)
	}
}

func TestSavePreservesTheFileMode(t *testing.T) {
	_, ts, dir := docServer(t)
	path := writeGraph(t, dir, "x.yaml", pairGraph)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	g := grantFor(t, ts, path, "open")
	status, _ := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": hashOf([]byte(pairGraph)), "text": "schema_version: 0\n"})
	if info, _ := os.Stat(path); status != 200 || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after save: %d %v", status, info.Mode())
	}
}

func TestSaveRefusesBadText(t *testing.T) {
	_, ts, dir := docServer(t)
	path := writeGraph(t, dir, "x.yaml", pairGraph)
	g := grantFor(t, ts, path, "open")
	hash := hashOf([]byte(pairGraph))
	for name, c := range map[string]struct {
		text string
		want int
	}{
		"oversize": {strings.Repeat("x", MaxDocumentBytes+1), 413},
		"NUL":      {"a\x00b", 422},
		// Invalid UTF-8 cannot arrive in a JSON body (the decoder replaces it);
		// it is refused when a file is opened (TestOpenRefusesNonTextAndHugeFiles).
	} {
		if status, _ := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": hash, "text": c.text}); status != c.want {
			t.Errorf("%s: %d, want %d", name, status, c.want)
		}
	}
	if disk, _ := os.ReadFile(path); string(disk) != pairGraph {
		t.Errorf("a refused save changed the file")
	}
	if status, _ := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": "nope", "base_hash": hash, "text": "x"}); status != 404 {
		t.Errorf("unknown grant: %d", status)
	}
}

func TestSaveRefusesWhenTheTargetBecameASymlinkOrTheDirectoryMoved(t *testing.T) {
	_, ts, dir := docServer(t)
	path := writeGraph(t, dir, "x.yaml", pairGraph)
	g := grantFor(t, ts, path, "open")
	other := writeGraph(t, dir, "other.yaml", "secret\n")
	_ = os.Remove(path)
	if err := os.Symlink(other, path); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	if status, out := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g, "base_hash": hashOf([]byte(pairGraph)), "text": "pwned"}); status != 409 || errCode(out) != "not_regular" {
		t.Errorf("symlink swapped in: %d %v", status, out)
	}
	if disk, _ := os.ReadFile(other); string(disk) != "secret\n" {
		t.Errorf("a save followed a symlink and changed %q", disk)
	}
	// The granted directory is replaced by a link to another one.
	sub := filepath.Join(dir, "sub")
	_ = os.Mkdir(sub, 0o700)
	g2 := grantFor(t, ts, filepath.Join(sub, "y.yaml"), "create")
	moved := filepath.Join(dir, "moved")
	if err := os.Rename(sub, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, sub); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	if status, out := call(t, ts, "PUT", "/graphs/save", map[string]any{"grant": g2, "base_hash": nil, "text": "x"}); status != 409 || errCode(out) != "path_changed" {
		t.Errorf("directory swapped: %d %v", status, out)
	}
}

func TestAtomicWriteLeavesNothingBehindOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := writeGraph(t, dir, "keep.yaml", "old\n")
	// Renaming a file over a directory fails: the old file and the directory survive.
	blocked := writeDir(t, dir, "blocked.yaml")
	if err := atomicWrite(dir, blocked, []byte("new\n"), 0o644, false); err == nil {
		t.Fatal("expected a failure")
	}
	if disk, _ := os.ReadFile(target); string(disk) != "old\n" {
		t.Errorf("unrelated file changed")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".agentplane-save-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
	// Create-only: the second creator loses and the first one's content stays.
	final := filepath.Join(dir, "once.yaml")
	if err := atomicWrite(dir, final, []byte("first\n"), 0o644, true); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(dir, final, []byte("second\n"), 0o644, true); err != errExists {
		t.Errorf("second create: %v, want errExists", err)
	}
	if disk, _ := os.ReadFile(final); string(disk) != "first\n" {
		t.Errorf("create-only overwrote: %q", disk)
	}
	left, _ = filepath.Glob(filepath.Join(dir, ".agentplane-save-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestRunStartsFromAGrantAndReadsTheSavedFile(t *testing.T) {
	var started string
	s, ts := newServer(t, nil)
	s.Start = func(_ context.Context, graphPath string, _ []string, _ func(events.Event) error, _ runtime.DecisionProvider) error {
		started = graphPath
		return &StartError{Status: http.StatusUnprocessableEntity, Message: "stop here"}
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	path := writeGraph(t, dir, "run.yaml", pairGraph)
	g := grantFor(t, ts, path, "open")
	call(t, ts, "POST", "/runs", map[string]any{"graph_grant": g})
	if started != path {
		t.Errorf("run started from %q, want %q", started, path)
	}
	for name, body := range map[string]map[string]any{
		"both":    {"graph_grant": g, "graph_path": path},
		"neither": {},
		"unknown": {"graph_grant": "nope"},
	} {
		status, _ := call(t, ts, "POST", "/runs", body)
		want := 400
		if name == "unknown" {
			want = 404
		}
		if status != want {
			t.Errorf("%s: %d, want %d", name, status, want)
		}
	}
	_ = os.Remove(path)
	_ = os.Mkdir(path, 0o700)
	if status, out := call(t, ts, "POST", "/runs", map[string]any{"graph_grant": g}); status != 409 {
		t.Errorf("a granted file that became a directory: %d %v", status, out)
	}
}

func TestOtherEndpointsKeepTheSmallBodyCap(t *testing.T) {
	_, ts, _ := docServer(t)
	big := `{"path":"` + strings.Repeat("a", 70<<10) + `","mode":"open"}`
	if resp, _ := do(t, ts, "POST", "/graphs/grants", big, true); resp.StatusCode != 400 {
		t.Errorf("a 70 KiB grant request: %d", resp.StatusCode)
	}
	huge := `{"text":"` + strings.Repeat("a", MaxDocumentBytes+100<<10) + `"}`
	if resp, _ := do(t, ts, "POST", "/graphs/validate", huge, true); resp.StatusCode != 413 {
		t.Errorf("a body above the document cap: %d", resp.StatusCode)
	}
}

func TestRevokedGrantsStopWorkingAndFreeASlot(t *testing.T) {
	_, ts, dir := docServer(t)
	p := writeGraph(t, dir, "g.yaml", pairGraph)
	g := grantFor(t, ts, p, "open")
	if resp, _ := do(t, ts, "DELETE", "/graphs/grants/"+g, "", true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	if status, out := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g}); status != 404 || errCode(out) != "unknown_grant" {
		t.Errorf("a revoked grant still opens: %d %v", status, out)
	}
	if resp, _ := do(t, ts, "DELETE", "/graphs/grants/"+g, "", true); resp.StatusCode != http.StatusNotFound {
		t.Errorf("revoking twice: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts, "DELETE", "/graphs/grants/"+g, "", false); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoke without a token: %d", resp.StatusCode)
	}
	for i := 0; i < MaxGrants; i++ {
		id := grantFor(t, ts, p, "open")
		if i == 0 {
			do(t, ts, "DELETE", "/graphs/grants/"+id, "", true) // frees one slot
		}
	}
	grantFor(t, ts, p, "open") // the slot freed by the revoke is usable
}
