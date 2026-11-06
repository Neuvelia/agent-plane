package codex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/graph"
	"agentplane/internal/process"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-process-supervisor" {
		os.Exit(process.Supervise(os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("/usr/bin/git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
}

func TestFrozenEnvironmentNeverFallsBackToAmbient(t *testing.T) {
	t.Setenv("AGENTPLANE_FREEZE_ONE", "ambient")
	t.Setenv("AGENTPLANE_FREEZE_TWO", "ambient-two")
	a := Adapter{FrozenEnvironment: map[string]string{"AGENTPLANE_FREEZE_ONE": "captured"}}
	got, err := a.environment([]string{"AGENTPLANE_FREEZE_ONE", "AGENTPLANE_FREEZE_ONE"})
	if err != nil || !reflect.DeepEqual(got, []string{"AGENTPLANE_FREEZE_ONE=captured"}) {
		t.Fatalf("frozen environment = %#v, %v", got, err)
	}
	if _, err := a.environment([]string{"AGENTPLANE_FREEZE_TWO"}); err == nil || strings.Contains(err.Error(), "ambient") {
		t.Fatalf("missing frozen ref fell back or leaked ambient value: %v", err)
	}
	a = Adapter{FrozenEnvironment: map[string]string{}}
	if _, err := a.environment([]string{"AGENTPLANE_FREEZE_ONE"}); err == nil {
		t.Fatal("empty frozen map fell back to ambient")
	}
	legacy, err := (Adapter{}).environment([]string{"AGENTPLANE_FREEZE_ONE"})
	if err != nil || !reflect.DeepEqual(legacy, []string{"AGENTPLANE_FREEZE_ONE=ambient"}) {
		t.Fatalf("legacy environment changed: %#v, %v", legacy, err)
	}
}
func repo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	gitTest(t, d, "init", "-q")
	os.WriteFile(filepath.Join(d, "file.txt"), []byte("base"), 0600)
	gitTest(t, d, "add", ".")
	gitTest(t, d, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	return d
}

const helpFlags = "--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message"

func fake(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "codex")
	s := "#!/bin/sh\ncase \"$*\" in\n *--version*) echo 'codex-cli 0.163.0-alpha.10.2'; exit;;\n *--help*) echo '" + helpFlags + "'; exit;;\nesac\nwhile [ $# -gt 0 ]; do\n case \"$1\" in --output-last-message) shift; result=$1;; --cd) shift; workspace=$1;; esac\n shift\ndone\ncd \"$workspace\" || exit 9\ncat >/dev/null\n" + body
	if e := os.WriteFile(p, []byte(s), 0700); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestFakeExecutionGitEvidenceAndSafeProgress(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "dirty"), []byte("unchanged"), 0600)
	a := Adapter{Binary: fake(t, `printf edited >file.txt
printf '{"type":"thread.started","thread_id":"SECRET"}\n{"type":"item.completed","item":{"text":"PRIVATE_REASONING","changed_files":["invented"]}}\n'
printf 'SECRET STDERR' >&2
printf '{"summary":"done","artifacts":[{"ref":"artifact:report"}]}' >"$result"
`)}
	var logs strings.Builder
	r, e := a.Detect(context.Background())
	if e != nil || !r.Available || r.Version == nil {
		t.Fatalf("detect=%+v %v", r, e)
	}
	out, e := a.ExecuteWithProgress(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "task"}, func(_ string, b []byte) { logs.Write(b) })
	if e != nil {
		t.Fatal(e)
	}
	if out.Summary != "done" || !reflect.DeepEqual(out.ChangedFiles, []agent.ChangedFile{{Path: "file.txt", Provenance: agent.ProvenanceObserved}}) {
		t.Fatalf("output=%+v", out)
	}
	if strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), "PRIVATE") || logs.Len() == 0 {
		t.Fatalf("logs=%q", logs.String())
	}
}

func TestSupervisedProbeAndExecution(t *testing.T) {
	d := repo(t)
	a := Adapter{Binary: fake(t, `printf '{"summary":"supervised","artifacts":[]}' >"$result"`), SupervisorBinary: os.Args[0]}
	report, err := a.Detect(context.Background())
	if err != nil || !report.Available {
		t.Fatalf("supervised detect = %+v, %v", report, err)
	}
	out, err := a.Execute(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "work"})
	if err != nil || out.Summary != "supervised" {
		t.Fatalf("supervised execute = %+v, %v", out, err)
	}
}
func TestSnapshotInvocationBaseline(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "file.txt"), []byte("dirty"), 0600)
	os.WriteFile(filepath.Join(d, "untracked"), []byte("u"), 0600)
	before, e := snapshot(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	after, e := snapshot(context.Background(), d)
	if e != nil || len(changed(before, after)) != 0 {
		t.Fatal("unchanged dirty baseline")
	}
	gitTest(t, d, "add", "file.txt")
	os.WriteFile(filepath.Join(d, "untracked"), []byte("further"), 0600)
	after, e = snapshot(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	if got := changed(before, after); !reflect.DeepEqual(got, []agent.ChangedFile{{Path: "file.txt", Provenance: agent.ProvenanceObserved}, {Path: "untracked", Provenance: agent.ProvenanceObserved}}) {
		t.Fatalf("changed=%+v", got)
	}
}

func TestFinalResultBoundary(t *testing.T) {
	for name, data := range map[string]string{"case-insensitive key": `{"Summary":"x","artifacts":[]}`,
		"null artifact name": `{"summary":"x","artifacts":[{"ref":"x","name":null}]}`,
		"missing summary":    `{"artifacts":[]}`, "missing artifacts": `{"summary":"x"}`, "null artifacts": `{"summary":"x","artifacts":null}`, "null summary": `{"summary":null,"artifacts":[]}`, "blank summary": `{"summary":" ","artifacts":[]}`, "unknown": `{"summary":"x","artifacts":[],"changed_files":["invented"]}`, "artifact": `{"summary":"x","artifacts":[{}]}`, "artifact extra": `{"summary":"x","artifacts":[{"ref":"x","private":1}]}`, "trailing": `{"summary":"x","artifacts":[]} {}`, "malformed": `{`, "oversized": strings.Repeat("x", maxRecord+1), "duplicate": `{"summary":"first","summary":"second","artifacts":[]}`} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "result")
			os.WriteFile(p, []byte(data), 0600)
			if _, err := readResult(p); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}
func TestFailureResultsNeverAccepted(t *testing.T) {
	for name, body := range map[string]string{"nonzero": `printf 'PROVIDER_SECRET' >&2;exit 7`, "missing": `true`, "malformed result": `printf '{' >"$result"`, "malformed progress": `printf '{broken\n'`, "oversized progress": `/usr/bin/head -c 1048577 /dev/zero`, "failure event": `printf '{"type":"turn.failed","error":"PROVIDER_SECRET"}\n'`} {
		t.Run(name, func(t *testing.T) {
			a := Adapter{Binary: fake(t, body)}
			out, err := a.Execute(context.Background(), agent.AgentExecutionInput{Workspace: repo(t), Task: "work"})
			if err == nil || out.Summary != "" || out.ChangedFiles != nil {
				t.Fatalf("%+v %v", out, err)
			}
			if strings.Contains(err.Error(), "PROVIDER_SECRET") {
				t.Fatal("provider error leaked")
			}
		})
	}
}
func TestDetectionBoundaries(t *testing.T) {
	a := Adapter{Binary: filepath.Join(t.TempDir(), "absent")}
	r, e := a.Detect(context.Background())
	if e != nil || r.Available {
		t.Fatalf("absent %+v %v", r, e)
	}
	for name, script := range map[string]string{"version": "echo unexpected", "capability": "case \"$*\" in *--version*) echo 'codex-cli 1.2.3';; *) echo '--json';; esac", "hang": "exec /bin/sleep 30"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "codex")
			os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0700)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if _, err := (Adapter{Binary: p}).Detect(ctx); err == nil {
				t.Fatal("bad capability accepted")
			}
		})
	}
}
func TestEnvironmentArgvAndTaskFidelity(t *testing.T) {
	d := repo(t)
	records := t.TempDir()
	t.Setenv("RECORD_DIR", records)
	t.Setenv("EXPLICIT_VALUE", "literal ; $(x) a=b\nnext")
	t.Setenv("SECRET_AMBIENT", "must not copy")
	a := Adapter{Binary: fake(t, `printf '%s' "$EXPLICIT_VALUE" >"$RECORD_DIR/value"
printf '%s' "${SECRET_AMBIENT-unset}" >"$RECORD_DIR/ambient"
printf '{"summary":"ok","artifacts":[]}' >"$result"
`)}
	// Replace fixture's stdin discard and retain original argv before its parser.
	b, _ := os.ReadFile(a.Binary)
	b = bytes.Replace(b, []byte("while [ $#"), []byte("printf '%s\\000' \"$@\" >\"$RECORD_DIR/argv\"\nwhile [ $#"), 1)
	b = bytes.Replace(b, []byte("cat >/dev/null"), []byte("cat >\"$RECORD_DIR/task\""), 1)
	os.WriteFile(a.Binary, b, 0700)
	task := "quotes ' ; $(touch SHOULD_NOT_EXIST)\nline two"
	_, err := a.Execute(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: task, EnvironmentRefs: []string{"RECORD_DIR", "EXPLICIT_VALUE"}, AllowedPaths: []string{"file.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	value, _ := os.ReadFile(filepath.Join(records, "value"))
	ambient, _ := os.ReadFile(filepath.Join(records, "ambient"))
	gotTask, _ := os.ReadFile(filepath.Join(records, "task"))
	argv, _ := os.ReadFile(filepath.Join(records, "argv"))
	if string(value) != os.Getenv("EXPLICIT_VALUE") || string(ambient) != "unset" || !strings.HasPrefix(string(gotTask), task) || !bytes.Contains(argv, []byte("workspace-write\x00")) || bytes.Contains(argv, []byte(task)) || bytes.Contains(argv, []byte("bypass")) {
		t.Fatalf("fidelity value=%q ambient=%q task=%q argv=%q", value, ambient, gotTask, argv)
	}
	for _, refs := range [][]string{{"MISSING_AGENTPLANE_VAR"}, {"INVALID-NAME"}, {"ALL=oops"}} {
		if _, err := environment(refs); err == nil {
			t.Fatalf("refs accepted %v", refs)
		}
	}
}
func TestWorkspaceAndGitPathBoundaries(t *testing.T) {
	for _, dir := range []string{t.TempDir(), "relative", filepath.Join(repo(t), "missing")} {
		if _, e := canonicalWorkspace(context.Background(), dir); e == nil {
			t.Fatalf("workspace accepted %q", dir)
		}
	}
	d := repo(t)
	for _, name := range []string{"spaces name", "tab\tname", "newline\nname"} {
		os.WriteFile(filepath.Join(d, name), []byte("content"), 0600)
	}
	os.Symlink("/outside/secret", filepath.Join(d, "link"))
	s, e := snapshot(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	if len(s) != 5 {
		t.Fatalf("paths=%v", s)
	}
	if _, e = confinedPath(d, "link/secret"); e == nil {
		t.Fatal("symlink ancestor accepted")
	}
	for _, p := range []string{"../escape", "/absolute", "a/../b", ".git/config", string([]byte{255})} {
		if _, e := confinedPath(d, p); e == nil {
			t.Fatalf("unsafe path accepted %q", p)
		}
	}
}

func TestSnapshotStagedDeletionModesAndSymlinks(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, "staged"), []byte("staged"), 0600)
	gitTest(t, d, "add", "staged")
	os.Symlink("target-a", filepath.Join(d, "link"))
	baseline, e := snapshot(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	same, e := snapshot(context.Background(), d)
	if e != nil || len(changed(baseline, same)) != 0 {
		t.Fatal("staged baseline changed")
	}
	os.Remove(filepath.Join(d, "file.txt"))
	os.Chmod(filepath.Join(d, "staged"), 0700)
	os.Remove(filepath.Join(d, "link"))
	os.Symlink("target-b", filepath.Join(d, "link"))
	os.WriteFile(filepath.Join(d, "new"), []byte("new"), 0600)
	after, e := snapshot(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	want := []agent.ChangedFile{}
	for _, path := range []string{"file.txt", "link", "new", "staged"} {
		want = append(want, agent.ChangedFile{Path: path, Provenance: agent.ProvenanceObserved})
	}
	if got := changed(baseline, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v", got)
	}
}
func TestSnapshotRejectsSubmoduleAndConflictedIndex(t *testing.T) {
	for _, entry := range []string{"160000 0123456789012345678901234567890123456789 0\tsubmodule\x00", "100644 0123456789012345678901234567890123456789 1\tconflict\x00"} {
		d := repo(t)
		c := exec.Command("/usr/bin/git", "update-index", "-z", "--index-info")
		c.Dir = d
		c.Stdin = strings.NewReader(entry)
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("index %v %s", e, b)
		}
		if _, e := snapshot(context.Background(), d); e == nil {
			t.Fatal("unsupported index accepted")
		}
	}
}
func TestProgressKnownEmptyUnknownAndStatusBudget(t *testing.T) {
	var b strings.Builder
	p := progress{log: func(_ string, data []byte) { b.Write(data) }}
	for range 5000 {
		p.Write([]byte("{\"type\":\"item.completed\",\"private\":\"SECRET\"}\n"))
	}
	p.Write([]byte("{\"type\":\"future.type\",\"text\":\"SECRET\"}\n"))
	if e := p.finish(); e != nil {
		t.Fatal(e)
	}
	if b.Len() > 64<<10 || strings.Contains(b.String(), "SECRET") {
		t.Fatal("unsafe or unbounded progress")
	}
}
func TestBadWorkspaceIsNodeScoped(t *testing.T) {
	a := Adapter{Binary: fake(t, `exit 99`)}
	executor := agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"codex": a}}
	n := graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"adapter": "codex", "workspace": t.TempDir(), "task": "edit"}}
	values, e := executor.ExecuteNode(context.Background(), "work", n, nil)
	var typed *agent.ExecutionError
	if values != nil || !errors.As(e, &typed) || typed.NodeID != "work" || typed.Kind != "codex_workspace" {
		t.Fatalf("values=%v error=%v", values, e)
	}
}
func TestCaptureLimitCannotBeBypassedByIOCopy(t *testing.T) {
	b := limitedBuffer{limit: 3}
	if _, e := io.Copy(&b, io.LimitReader(strings.NewReader("12345"), 5)); e != nil {
		t.Fatal(e)
	}
	if b.Len() != 3 || !b.overflow {
		t.Fatalf("capture length=%d overflow=%v", b.Len(), b.overflow)
	}
}

func TestInvocationIgnoreTransitions(t *testing.T) {
	for _, action := range []string{"unchanged", "modified", "deleted"} {
		t.Run("newly ignored/"+action, func(t *testing.T) {
			d := repo(t)
			os.WriteFile(filepath.Join(d, "preexisting.txt"), []byte("baseline"), 0600)
			body := "printf 'preexisting.txt\\n' >.gitignore\n"
			if action == "modified" {
				body += "printf changed >preexisting.txt\n"
			}
			if action == "deleted" {
				body += "rm preexisting.txt\n"
			}
			body += `printf '{"summary":"ok","artifacts":[]}' >"$result"`
			out, err := (Adapter{Binary: fake(t, body)}).Execute(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "edit"})
			if err != nil {
				t.Fatal(err)
			}
			want := []agent.ChangedFile{{Path: ".gitignore", Provenance: agent.ProvenanceObserved}}
			if action != "unchanged" {
				want = append(want, agent.ChangedFile{Path: "preexisting.txt", Provenance: agent.ProvenanceObserved})
			}
			if !reflect.DeepEqual(out.ChangedFiles, want) {
				t.Fatalf("changed=%v want=%v", out.ChangedFiles, want)
			}
		})
	}
	for _, setup := range []string{"ignored file", "ignored directory", "new descendant in ignored directory"} {
		t.Run("newly exposed/"+setup, func(t *testing.T) {
			d := repo(t)
			pattern := "hidden.txt"
			if setup != "ignored file" {
				pattern = "hidden/"
				os.Mkdir(filepath.Join(d, "hidden"), 0700)
			}
			os.WriteFile(filepath.Join(d, ".gitignore"), []byte(pattern+"\n"), 0600)
			path := "hidden.txt"
			if setup != "ignored file" {
				path = "hidden/old.txt"
			}
			os.WriteFile(filepath.Join(d, path), []byte("untouched baseline"), 0600)
			body := ": >.gitignore\n"
			if setup == "new descendant in ignored directory" {
				body += "printf new >hidden/new.txt\n"
			}
			body += `printf '{"summary":"ok","artifacts":[]}' >"$result"`
			out, err := (Adapter{Binary: fake(t, body)}).Execute(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "edit"})
			var typed *agent.ExecutionError
			if !errors.As(err, &typed) || typed.Kind != "codex_git" || out.Summary != "" || out.ChangedFiles != nil {
				t.Fatalf("out=%+v error=%v", out, err)
			}
		})
	}
}
