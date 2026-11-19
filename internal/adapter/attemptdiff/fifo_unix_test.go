//go:build unix

package attemptdiff

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"agentplane/internal/domain/events"
)

// An ignored symlink to a FIFO blocks forever if followed and opened, so this
// proves ignored files are only ever lstat-ed.
func TestDiffIgnoredFileIsNeverOpened(t *testing.T) {
	dir := newRepo(t, map[string]string{".gitignore": "pipe\n"})
	got := compare(t, newRecorder(t), dir, nil, func() {
		fifo := filepath.Join(t.TempDir(), "fifo")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Skip("mkfifo unavailable")
		}
		if err := os.Symlink(fifo, filepath.Join(dir, "pipe")); err != nil {
			t.Skip("symlinks unavailable")
		}
	})
	f := byPath(got)["pipe"]
	if f.Change != events.DiffAdded || f.OmittedReason != events.DiffOmitIgnored {
		t.Fatalf("pipe = %+v", f)
	}
}
