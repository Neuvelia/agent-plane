//go:build darwin || linux

package desktop

import (
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDocumentRejectsFIFOReplacementBeforeOpen(t *testing.T) {
	store, filename := documentFixture(t)
	store.beforeOpen = func() error {
		if err := os.Remove(filename); err != nil {
			return err
		}
		return unix.Mkfifo(filename, 0600)
	}
	done := make(chan error, 1)
	go func() { _, err := store.Open("project-1", ".agents/graphs/local.yaml"); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInvalidDocumentPath) {
			t.Fatalf("FIFO replacement error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("open blocked on swapped FIFO")
	}
}
