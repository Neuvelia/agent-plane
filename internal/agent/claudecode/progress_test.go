package claudecode

import (
	"context"
	"strings"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
)

const testRoot = "/ws"

func feed(p *progress, records ...string) {
	for _, r := range records {
		for i := 0; i < len(r); {
			n := min(i+997, len(r))
			_, _ = p.Write([]byte(r[i:n]))
			i = n
		}
		_, _ = p.Write([]byte("\n"))
	}
}

func newCollected() (*progress, *collector) {
	c := &collector{}
	return newProgress(testWorkspaces{}, testRoot, c.add, nil), c
}

// roadmap: F1.2b-AC6
func TestPrivateRecordsNeverProjected(t *testing.T) {
	p, c := newCollected()
	feed(p,
		`{broken`,
		`{"type":"assistant","type":"evil"}`,
		`{"Type":"assistant","message":{"content":[{"type":"text","text":"PRIVATE_ALIAS"}]}}`,
		recInit, recThink, recReadTU, recReadTR,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"PRIVATE_COMMAND"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b1","is_error":true,"content":"PRIVATE_OUTPUT"}]}}`,
		`{"type":"user","message":{"content":"PRIVATE_TASK_ECHO"}}`,
		`{"type":"stream_event","event":{"delta":{"text":"PRIVATE_DELTA"}}}`,
		`{"type":"rate_limit_event","message":"PRIVATE_LIMIT"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"x","name":"bad name with spaces PRIVATE_NAME","input":{}}]}}`,
		recText, recResult)
	if err := p.finish(); err != nil {
		t.Fatal(err)
	}
	want := []events.Type{events.Status, events.ToolCallStarted, events.ToolCallFinished, events.CommandStarted, events.CommandFinished, events.TextDelta, events.Status}
	if got := c.types(); !equalTypes(got, want) {
		t.Fatalf("events=%v", got)
	}
	cmd := c.got[4].Payload.(events.CommandPayload)
	if cmd.CommandID != "b1" || cmd.Status != "failed" || cmd.ExitCode != nil {
		t.Fatalf("command=%+v", cmd)
	}
	assertNoPrivate(t, c.json(t))
}

func equalTypes(a, b []events.Type) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// roadmap: F1.2b-AC7
func TestOversizedOptionalRecordIsSkipped(t *testing.T) {
	p, c := newCollected()
	huge := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_read","content":"` + strings.Repeat("x", maxRecord+50) + `"}]}}`
	feed(p, recInit, recReadTU, huge, recText, recResult)
	if err := p.finish(); err != nil {
		t.Fatal(err)
	}
	if p.result == nil || p.result.Summary != "done" || len(p.pending) != 0 {
		t.Fatalf("result=%+v pending=%d", p.result, len(p.pending))
	}
	// The skipped tool_result leaves the call unfinished; later records still project.
	want := []events.Type{events.Status, events.ToolCallStarted, events.TextDelta, events.Status}
	if got := c.types(); !equalTypes(got, want) {
		t.Fatalf("events=%v", got)
	}
}

func TestOversizedRecordAfterResultIsRejected(t *testing.T) {
	p, _ := newCollected()
	feed(p, recResult, strings.Repeat("x", maxRecord+1))
	if err := p.finish(); err == nil {
		t.Fatal("ambiguous oversized record after result accepted")
	}
}

func TestOversizedResultFailsClosed(t *testing.T) {
	p, _ := newCollected()
	feed(p, `{"type":"result","subtype":"success","structured_output":{"summary":"`+strings.Repeat("x", maxRecord)+`","artifacts":[]}}`)
	err := p.finish()
	var typed *agent.ExecutionError
	if err == nil || !asExecution(err, &typed) || typed.Kind != "claude_result" || p.result != nil {
		t.Fatalf("err=%v result=%+v", err, p.result)
	}
}

func asExecution(err error, target **agent.ExecutionError) bool {
	typed, ok := err.(*agent.ExecutionError) //nolint:errorlint // fail returns this exact type
	*target = typed
	return ok
}

func TestUnterminatedFinalResultIsRead(t *testing.T) {
	p, _ := newCollected()
	_, _ = p.Write([]byte(recResult))
	if err := p.finish(); err != nil || p.result == nil {
		t.Fatalf("err=%v result=%+v", err, p.result)
	}
}

func TestFailureResultCancelsAndNeverLeaksMessage(t *testing.T) {
	cancelled := false
	p := newProgress(testWorkspaces{}, testRoot, nil, func() { cancelled = true })
	feed(p, `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"PROVIDER_SECRET"}`)
	err := p.finish()
	if err == nil || !cancelled {
		t.Fatalf("err=%v cancelled=%v", err, cancelled)
	}
	assertNoPrivate(t, err.Error())
}

func TestFileChangedOnlyForConfirmedSuccessfulPaths(t *testing.T) {
	p, c := newCollected()
	tool := func(id, name, field, path string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":{"` + field + `":"` + path + `"}}]}}`
	}
	result := func(id, flag string) string {
		return `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","is_error":` + flag + `}]}}`
	}
	feed(p,
		tool("a", "Edit", "file_path", "/ws/a.go"), result("a", "false"),
		tool("b", "Write", "file_path", "b.go"), result("b", "false"),
		tool("c", "Edit", "file_path", "/ws/c.go"), result("c", "true"),
		tool("d", "Edit", "file_path", "/etc/passwd"), result("d", "false"),
		tool("e", "Edit", "file_path", "/ws/.git/config"), result("e", "false"),
		tool("f", "NotebookEdit", "notebook_path", "/ws/n.ipynb"), result("f", "false"),
		tool("g", "Edit", "wrong_field", "/ws/g.go"), result("g", "false"))
	var paths []string
	for _, e := range c.got {
		if v, ok := e.Payload.(events.FileChangedPayload); ok {
			paths = append(paths, v.Path)
		}
	}
	if strings.Join(paths, ",") != "a.go,b.go,n.ipynb" {
		t.Fatalf("paths=%v", paths)
	}
}

func TestToolTableIsBounded(t *testing.T) {
	p, _ := newCollected()
	for i := range maxTrackedTools + 10 {
		id := strings.Repeat("i", 3) + string(rune('A'+i%26)) + strings.Repeat("z", i/26)
		feed(p, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"`+id+`","name":"Read","input":{}}]}}`)
	}
	if len(p.tools) > maxTrackedTools {
		t.Fatalf("tools=%d", len(p.tools))
	}
}

func TestExecuteWithoutObserverStillValidatesResult(t *testing.T) {
	out, err := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, emit(recInit, recText, recResult))}.Execute(context.Background(), agent.AgentExecutionInput{Workspace: repo(t), Task: "t"})
	if err != nil || out.Summary != "done" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
