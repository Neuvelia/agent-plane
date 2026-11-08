//go:build darwin || linux

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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--internal-process-supervisor" {
		os.Exit(Supervise(os.Stdin, os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "--internal-process-driver" {
		_, _ = Run(context.Background(), os.Args[0], Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedProcessHelper$"},
			Env: []string{"AGENTPLANE_PROCESS_HELPER=leader"},
		}, os.Stdout, io.Discard)
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "--print-path" {
		fmt.Print(os.Getenv("PATH"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestOwnedProcessHelper(t *testing.T) {
	switch os.Getenv("AGENTPLANE_PROCESS_HELPER") {
	case "leader", "leader-escape":
		child := exec.Command(os.Args[0], "-test.run=^TestOwnedProcessHelper$")
		mode := "grandchild"
		if os.Getenv("AGENTPLANE_PROCESS_HELPER") == "leader-escape" {
			mode = "escaped-grandchild"
		}
		child.Env = []string{"AGENTPLANE_PROCESS_HELPER=" + mode}
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
	case "escaped-grandchild":
		if _, err := syscall.Setsid(); err != nil {
			os.Exit(91)
		}
		fmt.Printf("ready %d\n", os.Getpid())
		for {
			time.Sleep(time.Hour)
		}
	case "extra-files":
		left, errLeft := io.ReadAll(os.NewFile(3, "left"))
		right, errRight := io.ReadAll(os.NewFile(4, "right"))
		if errLeft != nil || errRight != nil {
			os.Exit(92)
		}
		fmt.Printf("%s|%s", left, right)
	case "exit-125":
		os.Exit(125)
	case "signal":
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		os.Exit(94)
	}
}

func TestRunDistinguishesStartErrorTargetExit125AndSignal(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
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
	for _, tc := range []struct {
		mode string
		code int
	}{{"exit-125", 125}, {"signal", -1}} {
		got, err := Run(context.Background(), os.Args[0], Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedProcessHelper$"},
			Env: []string{"AGENTPLANE_PROCESS_HELPER=" + tc.mode},
		}, io.Discard, io.Discard)
		var start *TargetStartError
		if err == nil || errors.As(err, &start) || got.ExitCode != tc.code || got.StartedAt.IsZero() {
			t.Fatalf("target %s = %+v, %v", tc.mode, got, err)
		}
	}
}

func TestRunJoinFailureIsNotOrdinaryCancellation(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "stubborn-pid")
	script := filepath.Join(t.TempDir(), "stubborn-supervisor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$$\" > '"+marker+"'\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { _, err := Run(ctx, script, Spec{Path: os.Args[0]}, io.Discard, io.Discard); done <- err }()
	deadline := time.After(8 * time.Second)
	var pid int
	for pid == 0 {
		if raw, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(string(raw))
		}
		select {
		case <-deadline:
			t.Fatal("stubborn supervisor did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case err := <-done:
		var join *JoinFailure
		if !errors.As(err, &join) || errors.Is(err, context.Canceled) {
			t.Fatalf("unverified cleanup was ordinary cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation timeout never returned")
	}
}

func TestAbandonedTargetWaitTimeoutDoesNotCertifyCleanup(t *testing.T) {
	blockedWait := make(chan error)
	timedOut := make(chan time.Time, 1)
	timedOut <- time.Now()
	status := targetStatus{Started: true, Code: -1, Failure: "supervisor"}
	if code := finishAbandonedTarget(blockedWait, timedOut, &status); code != 125 || status.Failure != "supervisor" {
		t.Fatalf("unreaped target was marked cancelled: code=%d status=%+v", code, status)
	}
	reaped := make(chan error, 1)
	reaped <- nil
	status = targetStatus{Started: true, Code: -1, Failure: "supervisor"}
	if code := finishAbandonedTarget(reaped, make(chan time.Time), &status); code != 125 || status.Failure != "cancelled" {
		t.Fatalf("reaped target was not marked cancelled: code=%d status=%+v", code, status)
	}
}

func TestRunMissingSupervisorFrameIsOwnershipFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "silent-supervisor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := Run(context.Background(), script, Spec{Path: os.Args[0]}, io.Discard, io.Discard)
	var supervisor *SupervisorFailure
	if !errors.As(err, &supervisor) || got.ExitCode != -1 {
		t.Fatalf("missing supervisor status = %+v, %v", got, err)
	}
}

func TestRunMalformedCancellationFrameIsNotVerifiedCancellation(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ready")
	script := filepath.Join(t.TempDir(), "malformed-supervisor")
	body := "#!/bin/sh\nprintf ready > '" + marker + "'\ncat >/dev/null\nprintf '%s' '{\"started\":true,\"code\":-1,\"failure\":\"cancelled\"' > \"/dev/fd/$AGENTPLANE_SUPERVISOR_STATUS_FD\"\nexit 125\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Run(ctx, script, Spec{Path: os.Args[0]}, io.Discard, io.Discard); done <- err }()
	deadline := time.After(8 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("malformed supervisor did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		var join *JoinFailure
		if !errors.As(err, &join) || errors.Is(err, context.Canceled) {
			t.Fatalf("malformed cancellation accepted: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("malformed cancellation did not return")
	}
}

func TestRunDoesNotInjectSupervisorEnvironment(t *testing.T) {
	var output strings.Builder
	_, err := Run(context.Background(), os.Args[0], Spec{
		Path: os.Args[0], Args: []string{"--print-path"},
	}, &output, io.Discard)
	if err != nil || output.Len() != 0 {
		t.Fatalf("supervisor environment leaked: output=%q err=%v", output.String(), err)
	}
}

func TestRunPassesOnlyExplicitExtraFilesAsThreeAndFour(t *testing.T) {
	leftRead, leftWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer leftRead.Close()
	rightRead, rightWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rightRead.Close()
	_, _ = leftWrite.WriteString("left-data")
	_, _ = rightWrite.WriteString("right-data")
	leftWrite.Close()
	rightWrite.Close()
	var output strings.Builder
	_, err = Run(context.Background(), os.Args[0], Spec{
		Path: os.Args[0], Args: []string{"-test.run=^TestOwnedProcessHelper$"},
		Env:        []string{"AGENTPLANE_PROCESS_HELPER=extra-files"},
		ExtraFiles: []*os.File{leftRead, rightRead},
	}, &output, io.Discard)
	if err != nil || !strings.Contains(output.String(), "left-data|right-data") {
		t.Fatalf("extra file result: %q, %v", output.String(), err)
	}
}

// This test records the process-group limitation as a native release blocker.
// The escaped process is killed by test cleanup, never left running on the host.
func TestSetsidEscapeRemainsReleaseBlocker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read, write := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, os.Args[0], Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedProcessHelper$"},
			Env: []string{"AGENTPLANE_PROCESS_HELPER=leader-escape"},
		}, write, io.Discard)
		write.Close()
		done <- err
	}()
	line, err := bufio.NewReader(read).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "ready ") {
		t.Fatalf("missing escaped grandchild barrier: %q %v", line, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "ready ")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("escaped grandchild not live at barrier: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("supervisor did not join after escape")
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("escaped grandchild unexpectedly owned: %v", err)
	}
}

func TestRunCancelsGrandchildBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read, write := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, os.Args[0], Spec{
			Path: os.Args[0], Args: []string{"-test.run=^TestOwnedProcessHelper$"},
			Env: []string{"AGENTPLANE_PROCESS_HELPER=leader"},
		}, write, io.Discard)
		write.Close()
		done <- err
	}()
	line, err := bufio.NewReader(read).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "ready ") {
		t.Fatalf("missing grandchild barrier: %q %v", line, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "ready ")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("grandchild was not live at barrier: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("owned process did not join after cancel")
	}
	if err := terminated(pid); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorKillsGrandchildAfterOwnerSIGKILL(t *testing.T) {
	driver := exec.Command(os.Args[0], "--internal-process-driver")
	stdout, err := driver.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "ready ") {
		_ = driver.Process.Kill()
		t.Fatalf("missing grandchild barrier: %q %v", line, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "ready ")))
	if err != nil {
		_ = driver.Process.Kill()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(pid, 0); err != nil {
		_ = driver.Process.Kill()
		t.Fatalf("grandchild was not live at barrier: %v", err)
	}
	if err := driver.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = driver.Wait()
	if err := terminated(pid); err != nil {
		t.Fatal(err)
	}
}

func terminated(pid int) error {
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if runtime.GOOS == "linux" {
			// Minimal container images may ship BusyBox ps without -p. Linux
			// procfs reports a reparented zombie even when kill(pid, 0) succeeds.
			stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if err == nil {
				close := strings.LastIndexByte(string(stat), ')')
				if close >= 0 && len(stat) > close+2 && stat[close+2] == 'Z' {
					return nil
				}
			}
		}
		b, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(b)), "Z") {
			return nil
		}
		select {
		case <-deadline:
			return fmt.Errorf("grandchild %d remains live", pid)
		case <-ticker.C:
		}
	}
}
