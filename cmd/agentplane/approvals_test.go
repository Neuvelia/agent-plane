package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

// orderedLog records prompt output and the test's own steps in one sequence.
type orderedLog struct {
	mu    sync.Mutex
	lines []string
	wrote chan string
}

func newOrderedLog() *orderedLog { return &orderedLog{wrote: make(chan string, 64)} }
func (l *orderedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.lines = append(l.lines, string(p))
	l.mu.Unlock()
	select {
	case l.wrote <- string(p):
	default:
	}
	return len(p), nil
}
func (l *orderedLog) mark(s string) { l.Write([]byte(s)) }
func (l *orderedLog) waitFor(t *testing.T, want string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case s := <-l.wrote:
			if strings.Contains(s, want) {
				return
			}
		case <-timeout:
			t.Fatalf("never saw %q", want)
		}
	}
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}
func (l *orderedLog) index(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, s := range l.lines {
		if strings.Contains(s, sub) {
			return i
		}
	}
	return -1
}

// Two human nodes can ask in the same batch. One person answers one question at
// a time, and each answer must be attributed to the node it was typed for.
func TestInteractivePromptsAreSerializedAndAttributedToTheRightNode(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	log := newOrderedLog()
	p := cliDecisions{prompt: bufio.NewReader(pr), out: log, turn: make(chan struct{}, 1)}
	type answer struct {
		node string
		d    runtime.Decision
	}
	answers := make(chan answer, 2)
	for _, node := range []string{"alpha", "beta"} {
		go func() {
			d, err := p.Decide(context.Background(), runtime.ApprovalRequest{NodeID: node, Prompt: "ok " + node + "?"})
			if err != nil {
				t.Errorf("%s: %v", node, err)
			}
			answers <- answer{node, d}
		}()
	}
	// Whichever node asked first owns the terminal until it is answered.
	log.waitFor(t, "Approve? ")
	first := "alpha"
	if log.index(`"beta"`) >= 0 && (log.index(`"alpha"`) < 0 || log.index(`"beta"`) < log.index(`"alpha"`)) {
		first = "beta"
	}
	second := map[string]string{"alpha": "beta", "beta": "alpha"}[first]
	log.mark("TEST-ANSWERS-FIRST")
	pw.Write([]byte("y\n"))
	got1 := recv(t, answers, "the first answer")
	log.waitFor(t, "Approve? ")
	log.mark("TEST-ANSWERS-SECOND")
	pw.Write([]byte("n\n"))
	got2 := recv(t, answers, "the second answer")
	if got1.node != first || !got1.d.Approve || got2.node != second || got2.d.Approve {
		t.Fatalf("answers went to the wrong nodes: %+v %+v (first asker %s)", got1, got2, first)
	}
	if log.index(`"`+second+`"`) < log.index("TEST-ANSWERS-FIRST") {
		t.Fatalf("the second node was prompted before the first was answered: %q", log.lines)
	}
}

func TestInteractivePromptStopsWhenCancelledAndQueuedPromptsDoNotStartAReader(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	log := newOrderedLog()
	p := cliDecisions{prompt: bufio.NewReader(pr), out: log, turn: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := p.Decide(ctx, runtime.ApprovalRequest{NodeID: "alpha"})
		first <- err
	}()
	log.waitFor(t, "Approve? ")
	cancel()
	if err := recv(t, first, "the cancelled prompt"); err != context.Canceled {
		t.Fatalf("first: %v", err)
	}
	if _, err := p.Decide(ctx, runtime.ApprovalRequest{NodeID: "beta"}); err != context.Canceled {
		t.Fatalf("a prompt queued after cancellation must not start: %v", err)
	}
	if log.index(`"beta"`) >= 0 {
		t.Fatal("a cancelled run prompted for another node")
	}
}

// With the turn free and the context already cancelled, both select cases are
// ready and Go picks one at random. The post-acquire check must make the result
// the same either way: no prompt is printed and no reader starts. Repeating the
// race makes a missing check fail with overwhelming probability.
func TestCancelledContextNeverPromptsEvenWhenTheTurnIsFree(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	for i := 0; i < 200; i++ {
		log := newOrderedLog()
		p := cliDecisions{prompt: bufio.NewReader(pr), out: log, turn: make(chan struct{}, 1)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.Decide(ctx, runtime.ApprovalRequest{NodeID: "alpha"}); err != context.Canceled {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if log.index("Approval needed") >= 0 || log.index("Approve?") >= 0 {
			t.Fatalf("iteration %d: a cancelled run printed a prompt", i)
		}
	}
}

func TestOnlyARealTerminalIsInteractive(t *testing.T) {
	for _, dev := range []string{os.DevNull, "/dev/zero"} {
		f, err := os.Open(dev)
		if err != nil {
			t.Skipf("%s: %v", dev, err)
		}
		old := os.Stdin
		os.Stdin = f
		got := stdinInteractive()
		os.Stdin = old
		f.Close()
		if got {
			t.Fatalf("%s must not count as a terminal", dev)
		}
	}
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	if stdinInteractive() {
		t.Fatal("a pipe is not a terminal")
	}
}

func TestApproveFlagErrorListsTheGraphsHumanNodes(t *testing.T) {
	def := &graph.Definition{Nodes: map[string]graph.NodeDefinition{"zeta": {Type: "human"}, "alpha": {Type: "human"}, "work": {Type: "shell"}}}
	_, err := decisionProvider(def, inputFlags{"work"}, nil, false, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "alpha, zeta") {
		t.Fatalf("%v", err)
	}
}
