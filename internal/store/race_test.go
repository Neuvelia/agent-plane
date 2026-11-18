package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// sharedHoldFS makes a reader's shared lock probe overlap the writer's lock
// acquisition: right after the run directory is created it creates run.lock and
// holds a shared lock on it, exactly what runLockHeld does for an instant.
type sharedHoldFS struct {
	osFS
	f    *os.File
	held chan struct{}
}

func (s *sharedHoldFS) Mkdir(path string) error {
	if err := s.osFS.Mkdir(path); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(path, lockFile), os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return err
	}
	if ok, err := tryLock(f, false, false); err != nil || !ok {
		return errors.New("test could not take the shared lock")
	}
	s.f = f
	close(s.held)
	return nil
}

func TestAReadersSharedProbeNeverMakesTheWriterFailToStart(t *testing.T) {
	fs := &sharedHoldFS{held: make(chan struct{})}
	s := Store{Dir: t.TempDir(), fs: fs}
	done := make(chan error, 1)
	go func() { done <- s.NewWriter(nil).Append(started(runA, time.Now().UTC())) }()
	<-fs.held
	// Give a writer that fails fast every chance to report it; one that waits
	// for the probe stays pending until the shared lock is released below.
	for i := 0; i < 200000; i++ {
		select {
		case err := <-done:
			t.Fatalf("writer returned while a probe held the lock: %v", err)
		default:
			runtime.Gosched()
		}
	}
	if err := unlock(fs.f); err != nil {
		t.Fatal(err)
	}
	if err := fs.f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("a reader's momentary probe made the writer fail: %v", err)
	}
}

// pausingFS stops the writer right after its directory was created, the
// moment a concurrent List used to see a half-built run.
type pausingFS struct {
	osFS
	entered, release chan struct{}
}

func (p *pausingFS) Mkdir(path string) error {
	if err := p.osFS.Mkdir(path); err != nil {
		return err
	}
	close(p.entered)
	<-p.release
	return nil
}

func TestListNeverShowsARunAsCorruptWhileItIsBeingCreated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows builds the run in place and cannot rename a directory with open files")
	}
	fs := &pausingFS{entered: make(chan struct{}), release: make(chan struct{})}
	s := Store{Dir: t.TempDir(), fs: fs}
	done := make(chan error, 1)
	go func() {
		w := s.NewWriter([]byte("g"))
		now := time.Now().UTC()
		if err := w.Append(started(runA, now)); err != nil {
			done <- err
			return
		}
		done <- w.Close()
	}()
	<-fs.entered
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("a run under creation must not be listed at all yet: %+v", list)
	}
	close(fs.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(); err != nil || len(list) != 1 || list[0].Status != StatusInterrupted {
		t.Fatalf("%v %+v", err, list)
	}
}
