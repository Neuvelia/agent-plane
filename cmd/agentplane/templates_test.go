package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"agentplane/internal/graph"
	"agentplane/internal/graph/edit"
)

// F2.12: the graph templates offered by "New graph" are ordinary graph files.
// These tests are the contract that every template parses, validates with
// exactly the diagnostics its index declares, and breaks no safety rule.

const templateDir = "../../.agents/templates"

type templateIndex struct {
	Version   int             `json:"version"`
	Templates []templateEntry `json:"templates"`
}

type templateEntry struct {
	ID           string   `json:"id"`
	File         string   `json:"file"`
	Requires     []string `json:"requires"`
	Platforms    []string `json:"platforms"`
	Placeholders []struct {
		Path   string `json:"path"`
		Marker string `json:"marker"`
	} `json:"placeholders"`
	ExpectedDiagnostics []struct {
		Code string `json:"code"`
		Path string `json:"path"`
	} `json:"expected_diagnostics"`
}

func loadTemplateIndex(t *testing.T) templateIndex {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(templateDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx templateIndex
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&idx); err != nil {
		t.Fatalf("index.json: %v", err)
	}
	if idx.Version != 1 || len(idx.Templates) != 4 {
		t.Fatalf("index version %d with %d templates, want version 1 with 4", idx.Version, len(idx.Templates))
	}
	return idx
}

type loadedTemplate struct {
	entry templateEntry
	text  []byte
	def   *graph.Definition
}

func loadTemplates(t *testing.T) []loadedTemplate {
	t.Helper()
	var out []loadedTemplate
	for _, e := range loadTemplateIndex(t).Templates {
		text, err := os.ReadFile(filepath.Join(templateDir, e.File))
		if err != nil {
			t.Fatal(err)
		}
		def, err := graph.Parse(text)
		if err != nil {
			t.Fatalf("%s: %v", e.File, err)
		}
		out = append(out, loadedTemplate{entry: e, text: text, def: def})
	}
	return out
}

func TestTemplateIndexMatchesTheDirectory(t *testing.T) {
	idx := loadTemplateIndex(t)
	var named []string
	seen := map[string]bool{}
	allowedRequires := map[string]bool{"git": true, "codex": true}
	for _, e := range idx.Templates {
		if seen[e.ID] {
			t.Fatalf("duplicate template id %q", e.ID)
		}
		seen[e.ID] = true
		if e.File != e.ID+".yaml" {
			t.Errorf("%s: file %q, want %q", e.ID, e.File, e.ID+".yaml")
		}
		named = append(named, e.File)
		for _, r := range e.Requires {
			if !allowedRequires[r] {
				t.Errorf("%s: unknown requirement %q", e.ID, r)
			}
		}
		if !reflect.DeepEqual(e.Platforms, []string{"linux", "darwin"}) {
			t.Errorf("%s: platforms %v", e.ID, e.Platforms)
		}
		for _, p := range e.Placeholders {
			if p.Marker != "FILL" {
				t.Errorf("%s: placeholder marker %q", e.ID, p.Marker)
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(templateDir, "*.yaml"))
	var onDisk []string
	for _, f := range files {
		onDisk = append(onDisk, filepath.Base(f))
	}
	sort.Strings(named)
	sort.Strings(onDisk)
	if !reflect.DeepEqual(named, onDisk) {
		t.Fatalf("index names %v, directory has %v", named, onDisk)
	}
}

func TestTemplatesFollowTheFileRules(t *testing.T) {
	for _, tpl := range loadTemplates(t) {
		id, text := tpl.entry.ID, tpl.text
		if len(text) > 8192 {
			t.Errorf("%s: %d bytes, limit 8192", id, len(text))
		}
		if !utf8.Valid(text) || bytes.HasPrefix(text, []byte("\xef\xbb\xbf")) || bytes.Contains(text, []byte("\r")) {
			t.Errorf("%s: must be UTF-8 without BOM and with LF line endings", id)
		}
		if !bytes.HasPrefix(text, []byte("# Template: ")) {
			t.Errorf("%s: must start with a '# Template: ' header comment", id)
		}
		if got := regexp.MustCompile(`(?m)^id: .*$`).FindAll(text, -1); len(got) != 1 || string(got[0]) != "id: "+id {
			t.Errorf("%s: want exactly one top-level line %q, got %q", id, "id: "+id, got)
		}
		if tpl.def.ID != id || tpl.def.SchemaVersion != 0 || strings.TrimSpace(tpl.def.Name) == "" {
			t.Errorf("%s: id %q, schema %d, name %q", id, tpl.def.ID, tpl.def.SchemaVersion, tpl.def.Name)
		}
		if reasons := edit.Inspect(text); len(reasons) != 0 {
			t.Errorf("%s: canvas would be read-only: %v", id, reasons)
		}
		res, err := edit.Apply(text, nil)
		if err != nil || !bytes.Equal(res.Text, text) {
			t.Errorf("%s: a no-op edit must return the same bytes (err %v)", id, err)
		}
		nodes := asMap(asMap(tpl.def.Metadata["layout"])["nodes"])
		for nodeID := range tpl.def.Nodes {
			if _, ok := nodes[nodeID]; !ok {
				t.Errorf("%s: node %q has no metadata.layout position", id, nodeID)
			}
		}
	}
}

type diagKey struct{ Code, Path string }

func diagKeys(diags []graph.Diagnostic) []diagKey {
	out := []diagKey{}
	for _, d := range diags {
		out = append(out, diagKey{d.Code, d.Location.Path})
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
	return out
}

func TestTemplatesValidateWithExactlyTheDeclaredDiagnostics(t *testing.T) {
	for _, tpl := range loadTemplates(t) {
		want := []diagKey{}
		for _, d := range tpl.entry.ExpectedDiagnostics {
			want = append(want, diagKey{d.Code, d.Path})
		}
		sort.Slice(want, func(i, j int) bool { return fmt.Sprint(want[i]) < fmt.Sprint(want[j]) })
		if got := diagKeys(validateGraph(tpl.def).Diagnostics); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: diagnostics %v, want %v", tpl.entry.ID, got, want)
		}
		if len(tpl.entry.Placeholders) == 0 && len(want) != 0 {
			t.Errorf("%s: a template without placeholders must validate with no diagnostic", tpl.entry.ID)
		}
	}
}

// The CLI command is the one a person runs; its exit status must agree.
func TestTemplateValidateCommandExitStatus(t *testing.T) {
	for _, tpl := range loadTemplates(t) {
		code, _, _ := invoke(t, context.Background(), "graph", "validate", filepath.Join(templateDir, tpl.entry.File))
		if want := map[bool]int{true: 0, false: 3}[len(tpl.entry.ExpectedDiagnostics) == 0]; code != want {
			t.Errorf("%s: graph validate exit %d, want %d", tpl.entry.ID, code, want)
		}
	}
}

func TestTemplatePlaceholdersAreMarkedAndAreTheOnlyGap(t *testing.T) {
	workspace, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range loadTemplates(t) {
		id := tpl.entry.ID
		marks := regexp.MustCompile(`(?m)^\s+(\w+): "" # FILL: \S.*$`).FindAllSubmatch(tpl.text, -1)
		if got := bytes.Count(tpl.text, []byte("# FILL:")); got != len(marks) || got != len(tpl.entry.Placeholders) {
			t.Fatalf("%s: %d FILL comments, %d well-formed, %d placeholders", id, got, len(marks), len(tpl.entry.Placeholders))
		}
		for i, p := range tpl.entry.Placeholders {
			parts := strings.Split(p.Path, ".")
			if len(parts) != 4 || parts[0] != "nodes" || parts[2] != "config" || parts[3] != string(marks[i][1]) {
				t.Fatalf("%s: placeholder %q does not match FILL line %q", id, p.Path, marks[i][0])
			}
			node := tpl.def.Nodes[parts[1]]
			if v, ok := node.Config[parts[3]]; !ok || v != "" {
				t.Fatalf("%s: %s must be the empty string, is %#v", id, p.Path, v)
			}
			node.Config[parts[3]] = workspace
		}
		if diags := validateGraph(tpl.def).Diagnostics; len(diags) != 0 {
			t.Errorf("%s: filled template still reports %v", id, diagKeys(diags))
		}
	}
}

func TestAgentTemplateScopeIsConsistentAndGuardedByApproval(t *testing.T) {
	for _, tpl := range loadTemplates(t) {
		for nodeID, n := range tpl.def.Nodes {
			if n.Type != "agent" {
				continue
			}
			if n.Loop == nil || !reflect.DeepEqual(n.Config["allowed_paths"], n.Loop.Gate.Config["allowed_paths"]) {
				t.Errorf("%s: %s allowed_paths must equal its gate's", tpl.entry.ID, nodeID)
			}
			// The agent must depend on a human decision, so no model call happens
			// before a person approves it (owner decision 2026-11-15).
			guarded := false
			for _, e := range tpl.def.Edges {
				src := strings.SplitN(string(e.From), ".", 2)[0]
				if strings.HasPrefix(string(e.To), nodeID+".inputs.") && tpl.def.Nodes[src].Type == "human" {
					guarded = true
				}
			}
			if !guarded {
				t.Errorf("%s: agent %s does not wait for a human approval", tpl.entry.ID, nodeID)
			}
		}
	}
}

// --- Safety rules S1-S7 (F2.12 requirement 6) ---

var (
	shMetachars   = []string{"`", "$(", ">", "<", "|", "&"}
	shDeniedWords = regexp.MustCompile(`\b(rm|mv|cp|dd|ln|chmod|chown|sudo|su|curl|wget|nc|ncat|ssh|scp|rsync|git|eval|exec|source|kill|mkfs|npm|pip|go|docker)\b`)
	shVariable    = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)`)
	secretKey     = regexp.MustCompile(`(?im)^\s*-?\s*"?(token|secret|password|api_key)"?\s*:`)
	secretValue   = regexp.MustCompile(`sk-|ghp_|github_pat_|AKIA[0-9A-Z]{16}|xox[abp]-|-----BEGIN`)
)

// safetyViolations returns the ids of the rules a template breaks.
func safetyViolations(text []byte, def *graph.Definition) []string {
	bad := map[string]bool{}
	agents := 0
	for _, n := range def.Nodes {
		if n.Loop != nil && (n.Loop.MaxAttempts < 1 || n.Loop.MaxAttempts > 3) {
			bad["S7"] = true
		}
		switch n.Type {
		case "shell":
			cmd, _ := n.Config["command"].(string)
			if cmd != "/usr/bin/printf" && cmd != "/bin/sh" {
				bad["S1"] = true
			}
			if cmd == "/bin/sh" && !safeShellScript(n) {
				bad["S2"] = true
			}
			for _, k := range []string{"cwd", "env", "env_allowlist"} {
				if _, ok := n.Config[k]; ok {
					bad["S3"] = true
				}
			}
		case "agent":
			agents++
			if n.Config["adapter"] != "codex" {
				bad["S7"] = true
			}
			for _, k := range []string{"environment_refs", "context_refs"} {
				if _, ok := n.Config[k]; ok {
					bad["S3"] = true
				}
			}
		case "gate":
			if k := n.Config["kind"]; k == "command" || k == "changed_files_scope" {
				bad["S4"] = true
			}
		}
		if n.Loop != nil {
			if k := n.Loop.Gate.Config["kind"]; k == "command" || k == "changed_files_scope" {
				bad["S4"] = true
			}
		}
	}
	if agents > 1 {
		bad["S7"] = true
	}
	if bytes.Contains(text, []byte("://")) || bytes.Contains(text, []byte("git@")) || bytes.Contains(text, []byte("www.")) {
		bad["S5"] = true
	}
	if secretKey.Match(text) || secretValue.Match(text) {
		bad["S6"] = true
	}
	out := []string{}
	for k := range bad {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// asMap reads a nested YAML mapping, which graph.Parse may return as one of
// its named map types.
func asMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case graph.Metadata:
		return m
	case graph.NodeConfig:
		return m
	}
	return nil
}

func safeShellScript(n graph.NodeDefinition) bool {
	args, _ := n.Config["args"].([]any)
	if len(args) != 2 || args[0] != "-c" {
		return false
	}
	script, ok := args[1].(string)
	if !ok {
		return false
	}
	for _, m := range shMetachars {
		if strings.Contains(script, m) {
			return false
		}
	}
	if shDeniedWords.MatchString(script) {
		return false
	}
	allowed := map[string]bool{"AGENTPLANE_ATTEMPT": true}
	for _, v := range asMap(n.Config["input_env"]) {
		if s, ok := v.(string); ok {
			allowed[s] = true
		}
	}
	for _, m := range shVariable.FindAllStringSubmatch(script, -1) {
		if !allowed[m[1]] {
			return false
		}
	}
	return true
}

func TestTemplatesBreakNoSafetyRule(t *testing.T) {
	for _, tpl := range loadTemplates(t) {
		if got := safetyViolations(tpl.text, tpl.def); len(got) != 0 {
			t.Errorf("%s breaks safety rules %v", tpl.entry.ID, got)
		}
	}
}

// Each checker must reject a deliberately unsafe graph, or the test above
// proves nothing.
func TestSafetyRulesRejectUnsafeFixtures(t *testing.T) {
	const head = "schema_version: 0\nid: unsafe\ninputs: {}\nedges: []\noutputs: {}\nnodes:\n"
	shell := func(cfg string) string {
		return head + "  a:\n    type: shell\n    config:\n" + cfg + "    outputs:\n      stdout: {type: string}\n"
	}
	agent := func(extra string) string {
		return head + "  a:\n    type: agent\n    config:\n      adapter: codex\n      workspace: /tmp\n      task: t\n" + extra
	}
	cases := []struct{ rule, text string }{
		{"S1", shell("      command: /bin/rm\n      args: [\"-rf\", \"x\"]\n")},
		{"S1", shell("      command: printf\n      args: [\"x\"]\n")},
		{"S2", shell("      command: /bin/sh\n      args: [\"-c\", \"printf x > out\"]\n")},
		{"S2", shell("      command: /bin/sh\n      args: [\"-c\", \"printf x | cat\"]\n")},
		{"S2", shell("      command: /bin/sh\n      args: [\"-c\", \"echo $(id)\"]\n")},
		{"S2", shell("      command: /bin/sh\n      args: [\"-c\", \"rm x\"]\n")},
		{"S2", shell("      command: /bin/sh\n      args: [\"-c\", \"printf %s $HOME\"]\n")},
		{"S2", shell("      command: /bin/sh\n      args: [\"printf x\"]\n")},
		{"S3", shell("      command: /usr/bin/printf\n      args: [\"x\"]\n      cwd: /tmp\n")},
		{"S3", shell("      command: /usr/bin/printf\n      args: [\"x\"]\n      env_allowlist: [HOME]\n")},
		{"S3", shell("      command: /usr/bin/printf\n      args: [\"x\"]\n      env: {A: b}\n")},
		{"S3", agent("      environment_refs: [HOME]\n")},
		{"S3", agent("      context_refs: [docs/a.md]\n")},
		{"S4", head + "  a:\n    type: gate\n    config:\n      check_id: c\n      kind: command\n"},
		{"S4", agent("      allowed_paths: []\n    loop:\n      max_attempts: 2\n      gate:\n        config: {check_id: c, kind: changed_files_scope}\n      bindings: []\n")},
		{"S5", shell("      command: /usr/bin/printf\n      args: [\"see https://example.com\"]\n")},
		{"S5", shell("      command: /usr/bin/printf\n      args: [\"git@host:repo\"]\n")},
		{"S6", shell("      command: /usr/bin/printf\n      args: [\"ghp_abcdef\"]\n")},
		{"S6", shell("      command: /usr/bin/printf\n      args: [\"AKIAABCDEFGHIJKLMNOP\"]\n")},
		{"S6", shell("      command: /usr/bin/printf\n      args: [\"x\"]\n      password: hunter2\n")},
		{"S7", agent("      allowed_paths: []\n    loop:\n      max_attempts: 5\n      gate:\n        config: {check_id: c, kind: json_predicate}\n      bindings: []\n")},
		{"S7", strings.Replace(agent(""), "adapter: codex", "adapter: other", 1)},
		{"S7", agent("  b:\n    type: agent\n    config:\n      adapter: codex\n      workspace: /tmp\n      task: t\n")},
	}
	for _, c := range cases {
		def, err := graph.Parse([]byte(c.text))
		if err != nil {
			t.Fatalf("fixture for %s does not parse: %v\n%s", c.rule, err, c.text)
		}
		got := safetyViolations([]byte(c.text), def)
		found := false
		for _, r := range got {
			found = found || r == c.rule
		}
		if !found {
			t.Errorf("%s fixture not rejected (got %v):\n%s", c.rule, got, c.text)
		}
	}
}

// The three templates without placeholders must run unedited (Linux and macOS).
func TestRunnableTemplatesRunUnedited(t *testing.T) {
	if goruntime.GOOS != "linux" && goruntime.GOOS != "darwin" {
		t.Skip("templates use POSIX commands")
	}
	type runOut struct {
		Status  string `json:"status"`
		Outputs []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"outputs"`
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	cases := []struct {
		id      string
		extra   []string
		output  string
		attempt int
	}{
		{"parallel-shell", nil, `"left branch done + right branch done\n"`, 0},
		{"shell-check-loop", nil, `{"ready":true}`, 2},
		{"human-approval", []string{"--approve", "approve"}, "", 0},
	}
	for _, c := range cases {
		args := append([]string{"run", filepath.Join(templateDir, c.id+".yaml"), "--store-dir", t.TempDir(), "--json"}, c.extra...)
		code, stdout, stderr := invoke(t, context.Background(), args...)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", c.id, code, stderr)
		}
		var out runOut
		if err := json.Unmarshal([]byte(stdout), &out); err != nil || out.Status != "succeeded" {
			t.Fatalf("%s: status %q (%v)", c.id, out.Status, err)
		}
		if c.output != "" {
			if len(out.Outputs) != 1 || out.Outputs[0].Name != "result" || string(out.Outputs[0].Value) != c.output {
				t.Errorf("%s: outputs %+v, want result = %s", c.id, out.Outputs, c.output)
			}
		}
		if c.attempt > 0 {
			attempts := 0
			for _, e := range out.Events {
				if e.Type == "attempt_started" {
					attempts++
				}
			}
			if attempts != c.attempt {
				t.Errorf("%s: %d attempts, want %d", c.id, attempts, c.attempt)
			}
		}
	}
}
