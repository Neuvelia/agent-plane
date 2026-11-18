//go:build darwin || linux

package capability

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCapabilityProbeBoundedTree(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pid")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// The fake git reports a background grandchild through the FIFO and blocks,
	// so the test proceeds only once the tree exists.
	bin := fakeBinary(t, "/bin/sleep 300 &\necho $! > "+fifo+"\nwait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Tool, 1)
	go func() {
		done <- Detector{GOOS: "linux", GitPath: bin, Timeout: time.Minute}.Detect(ctx).Tools[0]
	}()
	read := make(chan string, 1)
	go func() {
		b, err := os.ReadFile(fifo)
		if err != nil {
			read <- ""
			return
		}
		read <- strings.TrimSpace(string(b))
	}()
	var pid int
	select {
	case s := <-read:
		var err error
		if pid, err = strconv.Atoi(s); err != nil {
			t.Fatalf("fake git reported pid %q", s)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("fake git never started its grandchild")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case got := <-done:
		if got.Status != StatusError || got.Reason != ReasonProbeFailed {
			t.Fatalf("got %+v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("probe did not return")
	}
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatalf("grandchild %d survived the cancelled probe", pid)
	}
}
