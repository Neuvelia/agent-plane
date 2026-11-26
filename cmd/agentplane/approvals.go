package main

import (
	"agentplane/internal/domain/run"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"

	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
)

// cliDecisions answers human nodes from --approve/--reject flags first and,
// on an interactive terminal, by prompting. It never decides unlisted nodes
// without a person: preflight fails instead (ADR-0017).
type cliDecisions struct {
	flags  map[string]bool // node ID -> approve
	prompt *bufio.Reader   // nil when not interactive
	out    io.Writer
	// turn serializes interactive prompts: nodes in one batch may ask at the
	// same time, but a person answers one question at a time, and one
	// terminal reader must never be shared between concurrent readers.
	turn chan struct{}
}

//nolint:gocognit,gocyclo // legacy size, split in stage 3c of docs/audits/2026-11-16-clean-architecture-refactor-plan.md
func (c cliDecisions) Decide(ctx context.Context, req run.ApprovalRequest) (run.Decision, error) {
	if approve, ok := c.flags[req.NodeID]; ok {
		return run.Decision{Approve: approve, Comment: "decided by command-line flag"}, nil
	}
	if c.prompt == nil || c.turn == nil {
		return run.Decision{}, errors.New("no interactive terminal")
	}
	select {
	case c.turn <- struct{}{}:
		defer func() { <-c.turn }()
	case <-ctx.Done():
		return run.Decision{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return run.Decision{}, err
	}
	fmt.Fprintf(c.out, "\nApproval needed for node %q\n", req.NodeID)
	if req.Prompt != "" {
		fmt.Fprintf(c.out, "  %s\n", req.Prompt)
	}
	for _, item := range req.Context {
		fmt.Fprintf(c.out, "  %s: %s\n", item.Name, item.Value)
	}
	answers := make(chan string, 1)
	go func() {
		defer close(answers)
		for {
			fmt.Fprint(c.out, "Approve? [y/n] ")
			line, err := c.prompt.ReadString('\n')
			answer := strings.ToLower(strings.TrimSpace(line))
			if answer == "y" || answer == "yes" || answer == "n" || answer == "no" {
				answers <- answer
				return
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		// The reader goroutine may stay blocked in ReadString until the process
		// exits. That is safe: ctx is already cancelled, and every later Decide
		// re-checks ctx after taking its turn, so no second reader ever starts
		// on the same terminal.
		return run.Decision{}, ctx.Err()
	case answer, ok := <-answers:
		if !ok {
			return run.Decision{}, errors.New("terminal closed before a decision")
		}
		return run.Decision{Approve: strings.HasPrefix(answer, "y"), Comment: "decided interactively"}, nil
	}
}

// stdinInteractive is true only for a real terminal. Character devices such as
// /dev/null or /dev/zero are not terminals and must not be prompted.
func stdinInteractive() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// decisionProvider returns nil when some human node could not be answered, so
// the runtime reports the missing provider before any node executes.
func decisionProvider(def *graph.Definition, approve, reject inputFlags, interactive bool, stdin io.Reader, out io.Writer) (runtime.DecisionProvider, error) {
	flags := map[string]bool{}
	human := map[string]bool{}
	for id, n := range def.Nodes {
		if n.Type == "human" {
			human[id] = true
		}
	}
	ids := make([]string, 0, len(human))
	for id := range human {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// Flag text is quoted only when it is a human node id: anything else may be
	// a value typed in the wrong place (F3.8 criterion 18).
	for _, group := range []struct {
		flag    string
		names   inputFlags
		approve bool
	}{{"--approve", approve, true}, {"--reject", reject, false}} {
		for position, id := range group.names {
			if !human[id] {
				return nil, fmt.Errorf("%s #%d does not name a human node (human nodes in this graph: %s)", group.flag, position+1, listOrNone(ids))
			}
			if _, dup := flags[id]; dup {
				return nil, fmt.Errorf("node %q has both or repeated --approve/--reject flags", id)
			}
			flags[id] = group.approve
		}
	}
	if len(human) == 0 {
		return nil, nil
	}
	provider := cliDecisions{flags: flags, out: out}
	if interactive {
		provider.prompt = bufio.NewReader(stdin)
		provider.turn = make(chan struct{}, 1)
		return provider, nil
	}
	for _, id := range ids {
		if _, ok := flags[id]; !ok {
			return nil, nil
		}
	}
	return provider, nil
}

func listOrNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}
