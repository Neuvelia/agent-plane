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
	"runtime"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

// isolationFixture is a clean Git source repository, a fake installed Codex
// that writes one file per unit into its --cd directory, and a graph whose
// units opt in to worktree isolation.
type isolationFixture struct {
	source, root, binary, graphPath string
}

func isolationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	testenv.RequireExecutable(t, "/usr/bin/git")
	c := exec.Command("/usr/bin/git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newIsolationFixture writes shared.txt from every unit when conflict is true,
// otherwise one distinct file per unit.
func newIsolationFixture(t *testing.T, conflict bool) isolationFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("isolation fixtures use POSIX Git and shell")
	}
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolationGit(t, source, "init", "-q")
	if err := os.WriteFile(filepath.Join(source, "base.txt"), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	isolationGit(t, source, "add", "base.txt")
	isolationGit(t, source, "commit", "-qm", "base")

	target := `"$unit.txt"`
	if conflict {
		target = "shared.txt"
	}
	script := `#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --cd) shift;work=$1;; --output-last-message) shift;result=$1;; esac;shift;done
cd "$work" || exit 1
prompt=$(cat)
unit=x
case "$prompt" in *UNIT_A*) unit=a;; *UNIT_B*) unit=b;; esac
printf "$unit" >` + target + `
printf '{"summary":"%s","artifacts":[]}' "$unit" >"$result"
`
	binary := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var doc strings.Builder
	doc.WriteString("schema_version: 0\nid: isolated\nunits: [a, b]\ninputs: {}\nnodes:\n")
	for _, id := range []string{"a", "b"} {
		fmt.Fprintf(&doc, `  %[1]s:
    type: agent
    config: {adapter: codex, workspace: %[2]q, task: UNIT_%[3]s, allowed_paths: ["./"], isolation: worktree}
    outputs: {summary: {type: string, required: true}}
    loop:
      max_attempts: 1
      gate:
        inputs: {candidate: {type: string, required: true}}
        config: {check_id: scope-%[1]s, kind: changed_files_scope, input: candidate, mode: warn, workspace: %[2]q, allowed_paths: ["./"]}
      bindings:
        - {from: candidate.outputs.summary, to: gate.inputs.candidate}
`, id, source, strings.ToUpper(id))
	}
	doc.WriteString("edges: []\noutputs: {}\n")
	graphPath := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(graphPath, []byte(doc.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return isolationFixture{source: source, root: filepath.Join(t.TempDir(), "worktrees"), binary: binary, graphPath: graphPath}
}

func (f isolationFixture) args(extra ...string) []string {
	return append([]string{"run", f.graphPath, "--adapter", "codex", "--codex-bin", f.binary, "--worktree-root", f.root}, extra...)
}

type isolationJSON struct {
	Isolation *struct {
		BaseSHA          string `json:"base_sha"`
		IntegratedRef    string `json:"integrated_ref"`
		IntegratedCommit string `json:"integrated_commit"`
		Units            []struct {
			NodeID     string `json:"node_id"`
			Ref        string `json:"ref"`
			Commit     string `json:"commit"`
			Integrated bool   `json:"integrated"`
		} `json:"units"`
		Retained []struct {
			NodeID string `json:"node_id"`
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"retained"`
	} `json:"workspace_isolation"`
	Schema int `json:"cli_schema_version"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Diagnostics []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"diagnostics"`
}

func TestCLIIsolationIntegratesUnitsAndReportsJSON(t *testing.T) {
	f := newIsolationFixture(t, false)
	binary := buildGoldenBinary(t)
	stdout, stderr, exit := runGolden(t, binary, f.args("--retain-worktrees", "never", "--json")...)
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
	}
	var doc isolationJSON
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatal(err, string(stdout))
	}
	if doc.Schema != 1 || doc.Isolation == nil {
		t.Fatalf("schema=%d isolation=%v in %s", doc.Schema, doc.Isolation, stdout)
	}
	iso := doc.Isolation
	if want := isolationGit(t, f.source, "rev-parse", "HEAD"); iso.BaseSHA != want {
		t.Fatalf("base_sha=%q want %q", iso.BaseSHA, want)
	}
	if !strings.HasPrefix(iso.IntegratedRef, "refs/agentplane/") || len(iso.Units) != 2 || len(iso.Retained) != 0 {
		t.Fatalf("unexpected isolation result: %+v", iso)
	}
	for _, unit := range iso.Units {
		if !unit.Integrated || unit.Commit == "" {
			t.Fatalf("unit not integrated: %+v", unit)
		}
	}
	for _, name := range []string{"a.txt", "b.txt", "base.txt"} {
		if got := isolationGit(t, f.source, "cat-file", "-e", iso.IntegratedRef+":"+name); got != "" {
			t.Fatalf("integrated ref lacks %s", name)
		}
		if name != "base.txt" {
			if _, err := os.Stat(filepath.Join(f.source, name)); !os.IsNotExist(err) {
				t.Fatalf("%s leaked into the source checkout: %v", name, err)
			}
		}
	}
	if status := isolationGit(t, f.source, "status", "--porcelain"); status != "" {
		t.Fatalf("source checkout changed: %s", status)
	}
	entries, err := os.ReadDir(f.root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("worktree root should be empty under never: %v %v", entries, err)
	}
}

func TestCLIIsolationHumanOutputNamesBaseRefAndSandboxCaveat(t *testing.T) {
	f := newIsolationFixture(t, false)
	binary := buildGoldenBinary(t)
	_, stderr, exit := runGolden(t, binary, f.args("--retain-worktrees", "always")...)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	base := isolationGit(t, f.source, "rev-parse", "HEAD")
	for _, want := range []string{base, "refs/agentplane/", "not a sandbox", "git merge", f.root} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("human output lacks %q:\n%s", want, stderr)
		}
	}
}

func TestCLIIsolationConflictExitsFourAndRetainsWorktree(t *testing.T) {
	f := newIsolationFixture(t, true)
	binary := buildGoldenBinary(t)
	stdout, stderr, exit := runGolden(t, binary, f.args("--json")...)
	if exit != 4 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
	}
	var doc isolationJSON
	if err := json.Unmarshal(stdout, &doc); err != nil || doc.Isolation == nil {
		t.Fatalf("no isolation object: %v %s", err, stdout)
	}
	if len(doc.Isolation.Retained) != 1 || doc.Isolation.Retained[0].Reason != "conflict" {
		t.Fatalf("expected the conflicting worktree retained: %+v", doc.Isolation.Retained)
	}
	if _, err := os.Stat(doc.Isolation.Retained[0].Path); err != nil {
		t.Fatalf("retained path missing: %v", err)
	}
	if !strings.HasPrefix(doc.Isolation.Retained[0].Path, f.root+string(filepath.Separator)) {
		t.Fatalf("retained path %q outside --worktree-root %q", doc.Isolation.Retained[0].Path, f.root)
	}
}

func TestCLIIsolationPreflightFailuresExitThree(t *testing.T) {
	f := newIsolationFixture(t, false)
	binary := buildGoldenBinary(t)
	if err := os.WriteFile(filepath.Join(f.source, "base.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exit := runGolden(t, binary, f.args("--json")...)
	if exit != 3 {
		t.Fatalf("dirty source: exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
	}
	var doc isolationJSON
	if err := json.Unmarshal(stdout, &doc); err != nil || len(doc.Diagnostics) == 0 || doc.Diagnostics[0].Code != "E_ISOLATION_PREFLIGHT" || !strings.Contains(doc.Diagnostics[0].Message, "uncommitted") {
		t.Fatalf("preflight diagnostic: %v %s", err, stdout)
	}
	if _, err := os.Stat(f.root); !os.IsNotExist(err) {
		t.Fatalf("a failed preflight must not leave a worktree root behind: %v", err)
	}

	inside := filepath.Join(f.source, "wt")
	if err := os.WriteFile(filepath.Join(f.source, "base.txt"), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, exit = runGolden(t, binary, "run", f.graphPath, "--adapter", "codex", "--codex-bin", f.binary, "--worktree-root", inside)
	if exit != 3 || !strings.Contains(stderr, "inside the source workspace") {
		t.Fatalf("root inside source: exit=%d stderr=%s", exit, stderr)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatalf("a root inside the source must never be created: %v", err)
	}
}

func TestCLIIsolationFlagValidation(t *testing.T) {
	f := newIsolationFixture(t, false)
	cases := map[string][]string{
		"relative root":  {"--worktree-root", "relative/dir"},
		"bad policy":     {"--retain-worktrees", "sometimes"},
		"newline in dir": {"--worktree-root", "/tmp/a\nb"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			var out, errout bytes.Buffer
			args := append([]string{"run", f.graphPath, "--adapter", "codex", "--codex-bin", f.binary}, extra...)
			if code := runCommand(context.Background(), args, &out, &errout); code != exitUsage {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errout)
			}
			out.Reset()
			if code := runCommand(context.Background(), append(args, "--json"), &out, &errout); code != exitUsage || !strings.Contains(out.String(), `"kind":"usage"`) {
				t.Fatalf("json code=%d stdout=%s", code, &out)
			}
		})
	}
}

func TestCLIGraphWithoutIsolationIsUnchangedByIsolationFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("golden fixtures use POSIX commands")
	}
	binary := buildGoldenBinary(t)
	root := filepath.Join(t.TempDir(), "never-created")
	for _, tc := range []struct {
		fixture string
		exit    int
	}{{"chain", 0}, {"fail", 4}} {
		stdout, stderr, exit := runGolden(t, binary, "run", filepath.Join("testdata", "golden", tc.fixture+".yaml"), "--json", "--worktree-root", root, "--retain-worktrees", "always")
		if exit != tc.exit || stderr != "" {
			t.Fatalf("%s: exit=%d stderr=%q", tc.fixture, exit, stderr)
		}
		if bytes.Contains(stdout, []byte("workspace_isolation")) {
			t.Fatalf("%s: workspace_isolation must be absent without isolation", tc.fixture)
		}
		compareGolden(t, tc.fixture+".run.golden.json", normalizeGolden(stdout))
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("no directory work is allowed for a graph without isolation: %v", err)
	}
}

func TestIsolationOptionsComposeAManagerOnlyForIsolatedGraphs(t *testing.T) {
	flags := isolationFlags{root: filepath.Join(t.TempDir(), "wt"), retain: "never"}
	policy, err := flags.policy()
	if err != nil || policy != "never" {
		t.Fatalf("policy=%q err=%v", policy, err)
	}
	if _, err := (isolationFlags{retain: "nope"}).policy(); err == nil {
		t.Fatal("an unknown policy must be rejected")
	}
	if _, err := (isolationFlags{root: "rel"}).policy(); err == nil {
		t.Fatal("a relative root must be rejected")
	}
}

func TestServeRejectsInvalidIsolationFlags(t *testing.T) {
	for name, extra := range map[string][]string{
		"relative root": {"--worktree-root", "relative/dir"},
		"bad policy":    {"--retain-worktrees", "sometimes"},
	} {
		t.Run(name, func(t *testing.T) {
			var out, errout bytes.Buffer
			args := append([]string{"serve", "--store-dir", t.TempDir()}, extra...)
			if code := runCommand(context.Background(), args, &out, &errout); code != exitUsage || out.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%s", code, &out, &errout)
			}
		})
	}
}

// serve takes the same options and no new endpoint. Its executor has no Agent
// adapter yet (ADR-0019), so an isolated graph stops at the Agent preflight
// before any Git or directory work; a graph without isolation runs unchanged.
func TestServeAcceptsIsolationOptionsAndDoesNoWorkForOtherGraphs(t *testing.T) {
	f := newIsolationFixture(t, false)
	s := startServe(t, "--worktree-root", f.root, "--retain-worktrees", "always")

	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(f.graphPath)+`,"inputs":[]}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "E_LOOP_PREFLIGHT") {
		t.Fatalf("isolated graph through serve: %d %s", code, body)
	}

	chain, err := filepath.Abs(filepath.Join("testdata", "golden", "chain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	code, body = s.call(t, "POST", "/runs", `{"graph_path":`+quote(chain)+`,"inputs":[]}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %s", code, body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(body), &started); err != nil {
		t.Fatal(err)
	}
	_, stream := s.call(t, "GET", "/runs/"+started.RunID+"/events", "")
	if !strings.Contains(stream, `"type":"run_finished"`) || strings.Contains(stream, "workspace_") {
		t.Fatalf("unexpected stream for a graph without isolation: %s", stream)
	}
	if _, err := os.Stat(f.root); !os.IsNotExist(err) {
		t.Fatalf("serve touched the worktree root for a graph without isolation: %v", err)
	}
}
