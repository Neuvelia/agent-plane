package main

import (
	"agentplane/internal/events"
	"agentplane/internal/runtime"
	"agentplane/internal/testenv"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type progressBarrierWriter struct {
	mu   sync.Mutex
	b    bytes.Buffer
	seen chan struct{}
	once sync.Once
}

func (w *progressBarrierWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, e := w.b.Write(b)
	if strings.Contains(w.b.String(), "PUBLIC_PROGRESS") {
		w.once.Do(func() { close(w.seen) })
	}
	return n, e
}
func TestCLIWatchAgentProgressBeforeCompletion(t *testing.T) {
	workspace := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base"}} {
		testenv.RequireExecutable(t, "/usr/bin/git")
		c := exec.Command("/usr/bin/git", args...)
		c.Dir = workspace
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git %v %s", e, b)
		}
	}
	release := filepath.Join(t.TempDir(), "release")
	if b, e := exec.Command("/usr/bin/mkfifo", release).CombinedOutput(); e != nil {
		t.Fatalf("fifo %v %s", e, b)
	}
	binary := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
case "$*" in
 *--version*) echo 'codex-cli 0.163.0-alpha.10.2';exit;;
 *--help*) echo '--no-daemon --ask-for-approval --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message';exit;;
esac
while [ $# -gt 0 ];do case "$1" in --output-last-message) shift;result=$1;; esac;shift;done
/bin/cat >/dev/null
printf '%s\n' '{"type":"item.completed","item":{"id":"public","type":"agent_message","text":"PUBLIC_PROGRESS"}}'
/bin/cat '` + release + `' >/dev/null
printf '{"summary":"done","artifacts":[]}' >"$result"
`
	if e := os.WriteFile(binary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	graph := filepath.Join(t.TempDir(), "graph.yaml")
	if e := os.WriteFile(graph, []byte("schema_version: 0\nid: progress-barrier\ninputs: {}\nnodes:\n  work:\n    type: agent\n    config:\n      adapter: codex\n      workspace: "+workspace+"\n      task: edit\nedges: []\noutputs: {}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	out := &progressBarrierWriter{seen: make(chan struct{})}
	var errout bytes.Buffer
	done := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		done <- runCommand(ctx, []string{"run", graph, "--watch", "--adapter", "codex", "--codex-bin", binary}, out, &errout)
	}()
	select {
	case <-out.seen:
	case code := <-done:
		t.Fatalf("finished before progress: %d %s", code, &errout)
	}
	select {
	case code := <-done:
		t.Fatalf("provider passed barrier early: %d", code)
	default:
	}
	f, e := os.OpenFile(release, os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if code := <-done; code != 0 {
		t.Fatalf("code=%d %s", code, &errout)
	}
}

func TestNormalizedHumanAndJSONOmissionRendering(t *testing.T) {
	c := events.NewCollector("r", "g", "v")
	for range 5000 {
		c.Append(events.Event{Type: events.Status, Payload: events.StatusPayload{Code: "working"}})
	}
	c.Append(events.Event{Type: events.RunFinished, Payload: events.RunFinishedPayload{Status: "succeeded"}})
	result := runtime.RunResult{RunID: "r", State: runtime.RunSucceeded, Events: c.Snapshot()}
	var out, errout bytes.Buffer
	if code := writeJSON(&out, &errout, documentForRun(result, nil), 0); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), `"type":"progress_omitted"`) || !strings.Contains(out.String(), `"retained_events":904`) || !strings.Contains(out.String(), `"event_schema_version":9`) {
		t.Fatal("JSON omission missing")
	}
	out.Reset()
	errout.Reset()
	renderer := newEventRenderer(&out, &errout, true)
	for _, e := range result.Events {
		if err := renderer.Event(e); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(errout.String(), "Progress omitted: retained 904 events") || !strings.Contains(errout.String(), "Run succeeded") {
		t.Fatal("human omission missing")
	}
}
