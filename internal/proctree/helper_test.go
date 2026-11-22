package proctree

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const helperEnv = "AGENTPLANE_PROCTREE_HELPER"

// extraModes lets platform test files add helper modes that need
// platform-specific syscalls.
var extraModes = map[string]func(args []string){}

// TestHelperProcess is the re-executed test binary used as a controllable
// child. Readiness is signalled by a line on stdout, never by sleeping.
func TestHelperProcess(_ *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(90)
	}
	switch args[0] {
	case "hold":
		fmt.Printf("ready %d\n", os.Getpid())
		block()
	case "exit-on-term":
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM)
		fmt.Printf("ready %d\n", os.Getpid())
		<-c
		os.Exit(0)
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		fmt.Printf("ready %d\n", os.Getpid())
		block()
	case "spawn", "spawn-inherit":
		child := helperCmd("hold")
		child.Stderr = os.Stderr
		var line string
		if args[0] == "spawn" {
			out, err := child.StdoutPipe()
			if err != nil {
				os.Exit(91)
			}
			if err := child.Start(); err != nil {
				os.Exit(92)
			}
			l, err := bufio.NewReader(out).ReadString('\n')
			if err != nil {
				os.Exit(93)
			}
			line = l
		} else {
			child.Stdout = os.Stdout
			if err := child.Start(); err != nil {
				os.Exit(92)
			}
			line = fmt.Sprintf("ready %d\n", child.Process.Pid)
		}
		fmt.Printf("pids %d %s", os.Getpid(), strings.TrimPrefix(line, "ready "))
		block()
	case "exit":
		os.Exit(0)
	default:
		if f, ok := extraModes[args[0]]; ok {
			f(args[1:])
			return
		}
		os.Exit(94)
	}
}

func block() {
	for {
		time.Sleep(time.Hour)
	}
}

func helperCmd(mode string, args ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperProcess$", "--", mode}, args...)...)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	return cmd
}

// startHelper starts mode through a pipe and returns the tree plus the reader
// positioned after startup. The pipe is held open by the child, so tests read
// exact lines instead of waiting for EOF.
func startHelper(t *testing.T, mode string, args ...string) (*Tree, *bufio.Reader) {
	t.Helper()
	cmd := helperCmd(mode, args...)
	return startCmd(t, cmd, nil)
}

func startCmd(t *testing.T, cmd *exec.Cmd, prepare Prepare) (*Tree, *bufio.Reader) {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = pw
	tree, err := StartWith(cmd, prepare)
	pw.Close()
	if err != nil {
		pr.Close()
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		tree.Terminate(context.Background(), Policy{Grace: time.Millisecond})
		tree.Wait()
		pr.Close()
	})
	return tree, bufio.NewReader(pr)
}

func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("reading helper line: %v", err)
	}
	if line == "" {
		t.Fatal("helper closed its pipe before signalling readiness")
	}
	return strings.TrimSpace(line)
}

// readPIDs parses "pids <leader> <child>" written by the spawn mode.
func readPIDs(t *testing.T, r *bufio.Reader) (leader, child int) {
	t.Helper()
	f := strings.Fields(readLine(t, r))
	if len(f) != 3 || f[0] != "pids" {
		t.Fatalf("unexpected spawn line %q", f)
	}
	var err error
	if leader, err = strconv.Atoi(f[1]); err != nil {
		t.Fatal(err)
	}
	if child, err = strconv.Atoi(f[2]); err != nil {
		t.Fatal(err)
	}
	return leader, child
}

func readReadyPID(t *testing.T, r *bufio.Reader) int {
	t.Helper()
	f := strings.Fields(readLine(t, r))
	if len(f) != 2 || f[0] != "ready" {
		t.Fatalf("unexpected ready line %q", f)
	}
	pid, err := strconv.Atoi(f[1])
	if err != nil {
		t.Fatal(err)
	}
	return pid
}
