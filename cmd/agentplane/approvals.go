package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

// cliDecisions answers human nodes from --approve/--reject flags first and,
// on an interactive terminal, by prompting. It never decides unlisted nodes
// without a person: preflight fails instead (ADR-0017).
type cliDecisions struct {
	flags  map[string]bool // node ID -> approve
	prompt *bufio.Reader   // nil when not interactive
	out    io.Writer
}

func (c cliDecisions) Decide(ctx context.Context, req runtime.ApprovalRequest) (runtime.Decision, error) {
	if approve, ok := c.flags[req.NodeID]; ok {
		return runtime.Decision{Approve: approve, Comment: "decided by command-line flag"}, nil
	}
	if c.prompt == nil {
		return runtime.Decision{}, errors.New("no interactive terminal")
	}
	fmt.Fprintf(c.out, "\nApproval needed for node %q\n", req.NodeID)
	if req.Prompt != "" {
		fmt.Fprintf(c.out, "  %s\n", req.Prompt)
	}
	for _, item := range req.Context {
		fmt.Fprintf(c.out, "  %s: %s\n", item.Name, item.Value)
	}
	lines := make(chan string, 1)
	go func() {
		for {
			fmt.Fprint(c.out, "Approve? [y/n] ")
			line, err := c.prompt.ReadString('\n')
			if err != nil && line == "" {
				close(lines)
				return
			}
			answer := strings.ToLower(strings.TrimSpace(line))
			if answer == "y" || answer == "yes" || answer == "n" || answer == "no" {
				lines <- answer
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		return runtime.Decision{}, ctx.Err()
	case answer, ok := <-lines:
		if !ok {
			return runtime.Decision{}, errors.New("terminal closed before a decision")
		}
		return runtime.Decision{Approve: strings.HasPrefix(answer, "y"), Comment: "decided interactively"}, nil
	}
}

// stdinInteractive is true for a terminal. /dev/null is a character device
// but nobody can answer there, so it is excluded explicitly.
func stdinInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(info, null) {
		return false
	}
	return true
}

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
	for _, group := range []struct {
		names   inputFlags
		approve bool
	}{{approve, true}, {reject, false}} {
		for _, id := range group.names {
			if !human[id] {
				return nil, fmt.Errorf("--approve/--reject name %q is not a human node", id)
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
		return provider, nil
	}
	for id := range human {
		if _, ok := flags[id]; !ok {
			return nil, nil
		}
	}
	return provider, nil
}
