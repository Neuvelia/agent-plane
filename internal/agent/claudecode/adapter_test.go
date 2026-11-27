package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/testenv"
)

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	testenv.RequireExecutable(t, "/usr/bin/git")
	c := exec.Command("/usr/bin/git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	gitTest(t, d, "init", "-q")
	if err := os.WriteFile(filepath.Join(d, "file.txt"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, d, "add", ".")
	gitTest(t, d, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	return d
}

// helpFlags is the fake CLI's --help text. ASSUMED(spike): the real text differs.
var helpFlags = strings.Join(requiredFlags, " ")

// Redacted, plausible stream-json records. ASSUMED(spike): shapes from the plan.
const (
	recInit   = `{"type":"system","subtype":"init","session_id":"SECRET_SESSION","tools":["Read","Edit"]}`
	recText   = `{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"public note"}]}}`
	recThink  = `{"type":"assistant","message":{"id":"msg_2","content":[{"type":"thinking","thinking":"PRIVATE_REASONING"}]}}`
	recReadTU = `{"type":"assistant","message":{"id":"msg_3","content":[{"type":"tool_use","id":"tu_read","name":"Read","input":{"file_path":"/secret/PRIVATE_INPUT"}}]}}`
	recReadTR = `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_read","content":"PRIVATE_FILE_CONTENT"}]}}`
	recResult = `{"type":"result","subtype":"success","is_error":false,"result":"PRIVATE_FINAL","structured_output":{"summary":"done","artifacts":[{"ref":"artifact:report","name":"report"}]}}`
)

// emit prints each record on its own line from the fake CLI.
func emit(records ...string) string {
	var b strings.Builder
	for _, r := range records {
		b.WriteString("printf '%s\\n' '" + r + "'\n")
	}
	return b.String()
}

// editRecords makes the fake CLI report a successful Edit of rel inside its
// working directory.
func editRecords(rel string) string {
	return `p=$(pwd -P)
printf '%s\n' "{\"type\":\"assistant\",\"message\":{\"id\":\"msg_e\",\"content\":[{\"type\":\"tool_use\",\"id\":\"tu_edit\",\"name\":\"Edit\",\"input\":{\"file_path\":\"$p/` + rel + `\"}}]}}"
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_edit","is_error":false,"content":"PRIVATE_EDIT_OUTPUT"}]}}'
`
}

// fake writes a POSIX shell script that answers the detection probes and then
// runs body with the prompt already drained from stdin.
func fake(t *testing.T, body string) string {
	t.Helper()
	testenv.RequireDarwinOrLinux(t, "the Claude Code adapter")
	p := filepath.Join(t.TempDir(), "claude")
	s := "#!/bin/sh\ncase \"$*\" in\n *--version*) echo '2.1.332 (Claude Code)'; exit;;\n *--help*) echo '" + helpFlags + "'; exit;;\nesac\ncat >/dev/null\n" + body
	if e := os.WriteFile(p, []byte(s), 0700); e != nil {
		t.Fatal(e)
	}
	return p
}

type collector struct{ got []events.Event }

func (c *collector) add(k events.Type, v events.Payload) {
	c.got = append(c.got, events.Event{Type: k, Payload: v})
}

func (c *collector) types() []events.Type {
	out := make([]events.Type, 0, len(c.got))
	for _, e := range c.got {
		out = append(out, e.Type)
	}
	return out
}

func (c *collector) json(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(c.got)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertNoPrivate(t *testing.T, text string) {
	t.Helper()
	for _, s := range []string{"SECRET", "PRIVATE"} {
		if strings.Contains(text, s) {
			t.Fatalf("private marker %s leaked: %s", s, text)
		}
	}
}

// roadmap: F1.2b-AC1
func TestFakeExecutionGitEvidenceAndSafeProgress(t *testing.T) {
	d := repo(t)
	a := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, "printf edited >file.txt\n"+emit(recInit, recThink, recText, recReadTU, recReadTR)+editRecords("file.txt")+"printf 'SECRET STDERR' >&2\n"+emit(recResult))}
	r, e := a.Detect(context.Background())
	if e != nil || !r.Available || r.Version == nil || *r.Version != "2.1.332" || *r.UsageReporting || !*r.Cancellation || !*r.Streaming || !*r.Workspace || !*r.CorrectionFeedback {
		t.Fatalf("detect=%+v %v", r, e)
	}
	var c collector
	out, e := a.ExecuteWithNormalizedProgress(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "PRIVATE_TASK"}, c.add)
	if e != nil {
		t.Fatal(e)
	}
	wantFiles := []agent.ChangedFile{{Path: "file.txt", Provenance: agent.ProvenanceObserved}}
	if out.Summary != "done" || !reflect.DeepEqual(out.ChangedFiles, wantFiles) || !reflect.DeepEqual(out.Artifacts, []agent.ArtifactReference{{Ref: "artifact:report", Name: "report"}}) || out.Usage != nil || out.RawMetadata != nil {
		t.Fatalf("output=%+v", out)
	}
	want := []events.Type{events.Status, events.TextDelta, events.ToolCallStarted, events.ToolCallFinished, events.FileChanged, events.Status}
	if !reflect.DeepEqual(c.types(), want) {
		t.Fatalf("events=%v", c.types())
	}
	if p := c.got[4].Payload.(events.FileChangedPayload); p.Path != "file.txt" || p.Provenance != "reported" {
		t.Fatalf("file event=%+v", p)
	}
	assertNoPrivate(t, c.json(t))
}

// roadmap: F1.2b-AC2
func TestReportedPathNotInGitIsNotObserved(t *testing.T) {
	d := repo(t)
	a := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, emit(recInit)+editRecords("invented.txt")+editRecords("../escape.txt")+emit(recResult))}
	var c collector
	out, e := a.ExecuteWithNormalizedProgress(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "t"}, c.add)
	if e != nil {
		t.Fatal(e)
	}
	if len(out.ChangedFiles) != 0 {
		t.Fatalf("reported path became evidence: %+v", out.ChangedFiles)
	}
	text := c.json(t)
	if strings.Contains(text, "escape") || !strings.Contains(text, "invented.txt") {
		t.Fatalf("projection must keep only the confined reported path: %s", text)
	}
}

// roadmap: F1.2b-AC3
func TestDirtyBaseline(t *testing.T) {
	d := repo(t)
	if err := os.WriteFile(filepath.Join(d, "dirty"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	a := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, "printf edited >file.txt\n"+emit(recResult))}
	out, e := a.Execute(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "t"})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(out.ChangedFiles, []agent.ChangedFile{{Path: "file.txt", Provenance: agent.ProvenanceObserved}}) {
		t.Fatalf("changed=%+v", out.ChangedFiles)
	}
}

// roadmap: F1.2b-AC3
func TestPrivateConfigDirLeavesWorkspaceClean(t *testing.T) {
	d := repo(t)
	records := t.TempDir()
	t.Setenv("RECORD_DIR", records)
	body := `printf '%s' "$HOME" >"$RECORD_DIR/home"
printf '%s' "$CLAUDE_CONFIG_DIR" >"$RECORD_DIR/config"
printf '{}' >"$HOME/.claude.json"
printf '{}' >"$CLAUDE_CONFIG_DIR/settings.json"
` + emit(recResult)
	out, e := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, body)}.Execute(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: "t", EnvironmentRefs: []string{"RECORD_DIR"}})
	if e != nil || len(out.ChangedFiles) != 0 {
		t.Fatalf("out=%+v err=%v", out, e)
	}
	home, _ := os.ReadFile(filepath.Join(records, "home"))
	config, _ := os.ReadFile(filepath.Join(records, "config"))
	if len(home) == 0 || within(d, string(home)) || within(d, string(config)) || !strings.HasPrefix(string(config), string(home)) {
		t.Fatalf("home=%q config=%q workspace=%q", home, config, d)
	}
	if _, err := os.Stat(string(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private home was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, ".claude.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("config file reached the workspace")
	}
}

// roadmap: F1.2b-AC4
func TestDetectMissingAndUnsupported(t *testing.T) {
	testenv.RequireDarwinOrLinux(t, "the Claude Code adapter")
	r, e := Adapter{Workspaces: testWorkspaces{}, Binary: filepath.Join(t.TempDir(), "absent")}.Detect(context.Background())
	if e != nil || r.Available {
		t.Fatalf("absent %+v %v", r, e)
	}
	if _, e = (Adapter{Workspaces: testWorkspaces{}, Binary: "relative/claude"}).Detect(context.Background()); e == nil {
		t.Fatal("relative binary accepted")
	}
	cases := map[string]string{
		"unexpected version": "echo unexpected",
		"old version":        "case \"$*\" in *--version*) echo '0.1.0 (Other)';; esac",
		"missing flags":      "case \"$*\" in *--version*) echo '2.1.332 (Claude Code)';; *) echo '--print --bare';; esac",
		"hang":               "exec /bin/sleep 30",
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "claude")
			if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			_, err := Adapter{Workspaces: testWorkspaces{}, Binary: p}.Detect(ctx)
			var typed *agent.ExecutionError
			if !errors.As(err, &typed) {
				t.Fatalf("unsupported CLI accepted: %v", err)
			}
		})
	}
}

// roadmap: F1.2b-AC7
func TestBadFinalResultNeverAccepted(t *testing.T) {
	oversized := "printf '%s' '{\"type\":\"result\",\"subtype\":\"success\",\"structured_output\":{\"summary\":\"'\n/usr/bin/head -c 1100000 /dev/zero | /usr/bin/tr '\\000' x\nprintf '%s\\n' '\",\"artifacts\":[]}}'\n"
	cases := map[string]string{
		"missing result":   emit(recInit),
		"oversized result": oversized,
		"error result":     emit(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"PROVIDER_SECRET"}`),
		"is_error flag":    emit(`{"type":"result","subtype":"success","is_error":true,"structured_output":{"summary":"x","artifacts":[]}}`),
		"no structured":    emit(`{"type":"result","subtype":"success","is_error":false,"result":"free text"}`),
		"blank summary":    emit(`{"type":"result","subtype":"success","structured_output":{"summary":" ","artifacts":[]}}`),
		"empty ref":        emit(`{"type":"result","subtype":"success","structured_output":{"summary":"x","artifacts":[{"ref":""}]}}`),
		"duplicate result": emit(recResult, recResult),
		"duplicate key":    emit(`{"type":"result","subtype":"success","structured_output":{"summary":"a","summary":"b","artifacts":[]}}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, body)}.Execute(context.Background(), agent.AgentExecutionInput{Workspace: repo(t), Task: "work"})
			var typed *agent.ExecutionError
			if err == nil || !errors.As(err, &typed) || typed.Kind != "claude_result" || out.Summary != "" || out.ChangedFiles != nil || out.Artifacts != nil {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			assertNoPrivate(t, err.Error())
		})
	}
}

func TestProcessFailuresAreSafe(t *testing.T) {
	for name, body := range map[string]string{"nonzero": emit(recResult) + `printf 'PROVIDER_SECRET' >&2; exit 7`} {
		t.Run(name, func(t *testing.T) {
			_, err := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, body)}.Execute(context.Background(), agent.AgentExecutionInput{Workspace: repo(t), Task: "work"})
			var typed *agent.ExecutionError
			if !errors.As(err, &typed) || typed.Kind != "claude_process_exit" {
				t.Fatalf("err=%v", err)
			}
			assertNoPrivate(t, err.Error())
		})
	}
}

// roadmap: F1.2b-AC8
func TestArgvAndEnvironment(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/env")
	d := repo(t)
	records := t.TempDir()
	t.Setenv("RECORD_DIR", records)
	t.Setenv("EXPLICIT_VALUE", "literal ; $(x) a=b")
	t.Setenv("SECRET_AMBIENT", "must not copy")
	body := `printf '%s\000' "$@" >"$RECORD_DIR/argv"
/usr/bin/env >"$RECORD_DIR/env"
` + emit(recResult)
	// Keep the original argv: the fixture drains stdin before body, so record in body.
	script := fake(t, body)
	b, _ := os.ReadFile(script)
	b = bytes.Replace(b, []byte("cat >/dev/null"), []byte("cat >\"$RECORD_DIR/stdin\""), 1)
	if err := os.WriteFile(script, b, 0700); err != nil {
		t.Fatal(err)
	}
	task := "quotes ' ; $(touch SHOULD_NOT_EXIST)\nline two"
	if _, err := (Adapter{Workspaces: testWorkspaces{}, Binary: script}).Execute(context.Background(), agent.AgentExecutionInput{Workspace: d, Task: task, EnvironmentRefs: []string{"RECORD_DIR", "EXPLICIT_VALUE"}, AllowedPaths: []string{"file.txt"}}); err != nil {
		t.Fatal(err)
	}
	argv, _ := os.ReadFile(filepath.Join(records, "argv"))
	for _, forbidden := range []string{"bypass", "dangerously", "--bg", "Bash", "--allow-dangerously"} {
		if bytes.Contains(bytes.ToLower(argv), bytes.ToLower([]byte(forbidden))) {
			t.Fatalf("argv contains %q: %q", forbidden, argv)
		}
	}
	for _, required := range []string{"--tools\x00Read,Edit,Write,Glob,Grep\x00", "--permission-mode\x00acceptEdits\x00--permission-prompts\x00none\x00", "--no-session-persistence\x00", "--strict-mcp-config\x00", "--bare\x00"} {
		if !bytes.Contains(argv, []byte(required)) {
			t.Fatalf("argv lacks %q: %q", required, argv)
		}
	}
	if bytes.Contains(argv, []byte("SHOULD_NOT_EXIST")) || bytes.Contains(argv, []byte("line two")) {
		t.Fatal("task reached argv")
	}
	stdin, _ := os.ReadFile(filepath.Join(records, "stdin"))
	if !strings.HasPrefix(string(stdin), task) || !strings.Contains(string(stdin), "file.txt") {
		t.Fatalf("stdin=%q", stdin)
	}
	assertEnvironment(t, filepath.Join(records, "env"))
}

func assertEnvironment(t *testing.T, path string) {
	t.Helper()
	data, _ := os.ReadFile(path)
	allowed := map[string]bool{"RECORD_DIR": true, "EXPLICIT_VALUE": true, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": true, "DISABLE_AUTOUPDATER": true, "HOME": true, "CLAUDE_CONFIG_DIR": true,
		// Variables a POSIX shell adds itself.
		"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, _, ok := strings.Cut(line, "=")
		if !ok {
			continue // continuation of a multi-line value
		}
		if !allowed[name] {
			t.Fatalf("unexpected environment variable %q", name)
		}
		seen[name] = true
	}
	for _, name := range []string{"RECORD_DIR", "EXPLICIT_VALUE", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "DISABLE_AUTOUPDATER", "HOME", "CLAUDE_CONFIG_DIR"} {
		if !seen[name] {
			t.Fatalf("missing environment variable %q", name)
		}
	}
}

func TestEnvironmentRefsAreValidated(t *testing.T) {
	for _, refs := range [][]string{{"MISSING_AGENTPLANE_VAR"}, {"INVALID-NAME"}, {"ALL=oops"}, {"HOME"}, {"CLAUDE_CONFIG_DIR"}} {
		if _, err := environment(refs); err == nil {
			t.Fatalf("refs accepted %v", refs)
		}
	}
}

// roadmap: F1.2b-AC9
func TestCorrectionFeedbackOnStdin(t *testing.T) {
	records := t.TempDir()
	t.Setenv("RECORD_DIR", records)
	script := fake(t, emit(recResult))
	b, _ := os.ReadFile(script)
	b = bytes.Replace(b, []byte("cat >/dev/null"), []byte("cat >\"$RECORD_DIR/stdin\""), 1)
	if err := os.WriteFile(script, b, 0700); err != nil {
		t.Fatal(err)
	}
	in := agent.AgentExecutionInput{Workspace: repo(t), Task: "fix it", EnvironmentRefs: []string{"RECORD_DIR"}, Feedback: &run.CorrectionFeedback{PreviousAttemptID: "attempt-1", IntendedScope: []string{"src/a.go"}}}
	if _, err := (Adapter{Workspaces: testWorkspaces{}, Binary: script}).Execute(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	stdin, _ := os.ReadFile(filepath.Join(records, "stdin"))
	for _, want := range []string{"fix it", "UNTRUSTED CHECK FEEDBACK", "attempt-1", "src/a.go"} {
		if !strings.Contains(string(stdin), want) {
			t.Fatalf("stdin lacks %q: %q", want, stdin)
		}
	}
}

func TestBadWorkspaceIsNodeScoped(t *testing.T) {
	a := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, `exit 99`)}
	executor := agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"claude": a}}
	_, err := executor.ExecuteNode(context.Background(), "work", graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"adapter": "claude", "workspace": t.TempDir(), "task": "edit"}}, nil)
	var typed *agent.ExecutionError
	if !errors.As(err, &typed) || typed.NodeID != "work" || typed.Kind != "claude_workspace" {
		t.Fatalf("error=%v", err)
	}
}
