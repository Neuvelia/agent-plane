//go:build windows

package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-process-supervisor" {
		os.Exit(Supervise(os.Stdin, os.Stdout, os.Stderr))
	}
	if len(os.Args) == 2 && os.Args[1] == "--internal-process-driver" {
		_, _ = Run(context.Background(), os.Args[0], Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedWindowsHelper$"},
			Env: []string{"AGENTPLANE_PROCESS_HELPER=leader"},
		}, os.Stdout, io.Discard)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestOwnedWindowsHelper(t *testing.T) {
	switch os.Getenv("AGENTPLANE_PROCESS_HELPER") {
	case "leader":
		child := exec.Command(os.Args[0], "-test.run=^TestOwnedWindowsHelper$")
		child.Env = []string{"AGENTPLANE_PROCESS_HELPER=grandchild"}
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(90)
		}
		_ = child.Wait()
	case "grandchild":
		fmt.Printf("ready %d\n", os.Getpid())
		for {
			time.Sleep(time.Hour)
		}
	case "marker":
		if err := os.WriteFile(os.Getenv("AGENTPLANE_PROCESS_MARKER"), []byte("ran"), 0600); err != nil {
			os.Exit(91)
		}
	case "exit-125":
		os.Exit(125)
	case "environment":
		fmt.Printf("%s|%s", os.Getenv("SHELL_CASE"), os.Getenv("SYSTEMROOT"))
	}
}

func TestRunWindowsDistinguishesStartFromExit125(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.exe")
	for _, tc := range []struct {
		spec   Spec
		reason string
	}{{Spec{Path: missing}, "executable is unavailable"}, {Spec{Path: os.Args[0], Dir: missing}, "working directory is unavailable"}} {
		got, err := Run(context.Background(), os.Args[0], tc.spec, io.Discard, io.Discard)
		var start *TargetStartError
		if !errors.As(err, &start) || start.Reason != tc.reason || got.ExitCode != -1 || !got.StartedAt.IsZero() {
			t.Fatalf("target start failure = %+v, %v", got, err)
		}
	}
	got, err := Run(context.Background(), os.Args[0], Spec{
		Path: os.Args[0], Args: []string{"-test.run=^TestOwnedWindowsHelper$"},
		Env: []string{"AGENTPLANE_PROCESS_HELPER=exit-125"},
	}, io.Discard, io.Discard)
	var start *TargetStartError
	if err == nil || errors.As(err, &start) || got.ExitCode != 125 || got.StartedAt.IsZero() {
		t.Fatalf("target exit 125 = %+v, %v", got, err)
	}
}

func TestWindowsTargetResolvesRelativeExecutableInSelectedCwd(t *testing.T) {
	dir := t.TempDir()
	name := "target.exe"
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "target-ran")
	got, err := Run(context.Background(), os.Args[0], Spec{
		Path: ".\\" + name, Dir: dir, Args: []string{"-test.run=^TestOwnedWindowsHelper$"},
		Env: []string{"AGENTPLANE_PROCESS_HELPER=marker", "AGENTPLANE_PROCESS_MARKER=" + marker},
	}, io.Discard, io.Discard)
	if err != nil || got.ExitCode != 0 {
		t.Fatalf("cwd relative target = %+v, %v", got, err)
	}
	if value, err := os.ReadFile(marker); err != nil || string(value) != "ran" {
		t.Fatalf("cwd target never ran: %q %v", value, err)
	}
}

func TestWindowsTargetEnvironmentUsesLastCaseInsensitiveValueAndSystemRoot(t *testing.T) {
	var output strings.Builder
	got, err := Run(context.Background(), os.Args[0], Spec{
		Path: os.Args[0], Args: []string{"-test.run=^TestOwnedWindowsHelper$"},
		Env: []string{"AGENTPLANE_PROCESS_HELPER=environment", "shell_case=parent", "SHELL_CASE=explicit"},
	}, &output, io.Discard)
	if err != nil || got.ExitCode != 0 || output.String() != "explicit|"+os.Getenv("SYSTEMROOT") {
		t.Fatalf("target environment = %+v, %v, %q", got, err, output.String())
	}
}

func TestTargetIsSuspendedBeforeWindowsJobAssignment(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "target-ran")
	created := make(chan windows.Handle, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	result := make(chan int, 1)
	go func() {
		result <- superviseWindows(Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedWindowsHelper$"},
			Env: []string{"AGENTPLANE_PROCESS_HELPER=marker", "AGENTPLANE_PROCESS_MARKER=" + marker},
		}, make(chan struct{}), io.Discard, io.Discard, func(process windows.Handle) {
			created <- process
			<-release
		})
	}()
	select {
	case process := <-created:
		state, err := windows.WaitForSingleObject(process, 0)
		if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
			t.Fatalf("target state before assignment: %d, %v", state, err)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target ran before Job assignment: %v", err)
		}
	case code := <-result:
		t.Fatalf("target failed before assignment barrier: %d", code)
	case <-time.After(4 * time.Second):
		t.Fatal("suspended creation barrier timed out")
	}
	close(release)
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("assigned target exit = %d", code)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("assigned target did not join")
	}
	if value, err := os.ReadFile(marker); err != nil || string(value) != "ran" {
		t.Fatalf("target did not resume after assignment: %q, %v", value, err)
	}
}

func TestWindowsSupervisorDoesNotKeepChildPipeHandlesOpen(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "target-ran")
	finished := make(chan int, 1)
	go func() {
		finished <- superviseWindows(Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedWindowsHelper$"},
			Env:   []string{"AGENTPLANE_PROCESS_HELPER=marker", "AGENTPLANE_PROCESS_MARKER=" + marker},
			Stdin: make([]byte, 1<<20),
		}, make(chan struct{}), io.Discard, io.Discard, nil)
	}()
	select {
	case code := <-finished:
		// The target deliberately does not read stdin, so a broken-pipe status
		// is allowed. The supervisor must still join rather than retain a read
		// handle and block its input/output copy workers forever.
		if code != 0 && code != 125 {
			t.Fatalf("unexpected supervisor status %d", code)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("supervisor retained child pipe handles after target exit")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("target never ran: %v", err)
	}
}

func windowsGrandchildHandle(t *testing.T, reader io.Reader) windows.Handle {
	t.Helper()
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "ready ") {
		t.Fatalf("missing grandchild barrier: %q %v", line, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "ready ")))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.TerminateProcess(handle, 125); _ = windows.CloseHandle(handle) })
	state, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("grandchild not live at barrier: state=%d err=%v", state, err)
	}
	return handle
}

func TestRunCancelsWindowsGrandchildBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read, write := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, os.Args[0], Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedWindowsHelper$"},
			Env: []string{"AGENTPLANE_PROCESS_HELPER=leader"},
		}, write, io.Discard)
		_ = write.Close()
		done <- err
	}()
	handle := windowsGrandchildHandle(t, read)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("owned process did not join after cancel")
	}
	state, err := windows.WaitForSingleObject(handle, 2000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("grandchild survived cancel: state=%d err=%v", state, err)
	}
}

func TestSupervisorKillsWindowsGrandchildAfterOwnerTermination(t *testing.T) {
	driver := exec.Command(os.Args[0], "--internal-process-driver")
	stdout, err := driver.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Start(); err != nil {
		t.Fatal(err)
	}
	handle := windowsGrandchildHandle(t, stdout)
	if err := driver.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = driver.Wait()
	state, err := windows.WaitForSingleObject(handle, 2000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("grandchild survived owner termination: state=%d err=%v", state, err)
	}
}
