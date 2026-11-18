package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentplane/internal/domain/events"
)

func TestWriterHoldsTheRunLockUntilClose(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w := s.NewWriter(nil)
	if err := w.Append(started(runA, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, runA, lockFile)
	if second, err := acquireRunLock(path, false); !errors.Is(err, ErrRunLocked) {
		if err == nil {
			if rerr := second.release(); rerr != nil {
				t.Fatal(rerr)
			}
		}
		t.Fatalf("a second handle must not take the lock of a live writer: %v", err)
	}
	if held, err := runLockHeld(path); err != nil || !held {
		t.Fatalf("probe: held=%v err=%v", held, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := acquireRunLock(path, false)
	if err != nil {
		t.Fatalf("lock must be free after Close: %v", err)
	}
	if err := again.release(); err != nil {
		t.Fatal(err)
	}
	if held, err := runLockHeld(path); err != nil || held {
		t.Fatalf("probe after release: held=%v err=%v", held, err)
	}
}

func TestLiveRunReadsRunningAndDeadRunReadsInterrupted(t *testing.T) {
	now := time.Now().UTC()
	t.Run("terminal event wins even while the lock is held", func(t *testing.T) {
		s := Store{Dir: t.TempDir()}
		w := s.NewWriter(nil)
		t.Cleanup(func() { closeWriter(t, w) })
		for _, e := range []events.Event{started(runA, now), finished(runA, 2, "failed", now)} {
			if err := w.Append(e); err != nil {
				t.Fatal(err)
			}
		}
		rec, err := s.Read(runA)
		if err != nil || rec.Summary.Status != "failed" || rec.Summary.InterruptedAtSequence != 0 {
			t.Fatalf("%v %+v", err, rec.Summary)
		}
	})
	t.Run("no terminal event and lock held is running", func(t *testing.T) {
		s := Store{Dir: t.TempDir()}
		w := s.NewWriter(nil)
		t.Cleanup(func() { closeWriter(t, w) })
		if err := w.Append(started(runA, now)); err != nil {
			t.Fatal(err)
		}
		rec, err := s.Read(runA)
		if err != nil || rec.Summary.Status != StatusRunning || rec.Summary.InterruptedAtSequence != 0 {
			t.Fatalf("%v %+v", err, rec.Summary)
		}
		list, err := s.List()
		if err != nil || len(list) != 1 || list[0].Status != StatusRunning {
			t.Fatalf("%v %+v", err, list)
		}
	})
	t.Run("no terminal event and lock free is interrupted at the last durable sequence", func(t *testing.T) {
		s := Store{Dir: t.TempDir()}
		write(t, s, runA, started(runA, now), ev(runA, 2, events.NodeReady, events.NodeReadyPayload{}, now), ev(runA, 5, events.NodeReady, events.NodeReadyPayload{}, now))
		rec, err := s.Read(runA)
		if err != nil || rec.Summary.Status != StatusInterrupted || rec.Summary.InterruptedAtSequence != 5 {
			t.Fatalf("%v %+v", err, rec.Summary)
		}
	})
	t.Run("a run without a lock file is a dead run", func(t *testing.T) {
		s := Store{Dir: t.TempDir()}
		write(t, s, runA, started(runA, now))
		if err := os.Remove(filepath.Join(s.Dir, runA, lockFile)); err != nil {
			t.Fatal(err)
		}
		rec, err := s.Read(runA)
		if err != nil || rec.Summary.Status != StatusInterrupted || rec.Summary.InterruptedAtSequence != 1 {
			t.Fatalf("%v %+v", err, rec.Summary)
		}
	})
}

func closeWriter(t *testing.T, w *Writer) {
	t.Helper()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRunLockIsReleasedWhenTheWriterIsAbandoned(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w := s.NewWriter(nil)
	if err := w.Append(started(runA, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(); err != nil || len(list) != 1 || list[0].Status != StatusRunning {
		t.Fatalf("%v %+v", err, list)
	}
	// Process death closes every descriptor; the writer is never closed here.
	if err := w.lock.f.Close(); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Status != StatusInterrupted || list[0].InterruptedAtSequence != 1 {
		t.Fatalf("%v %+v", err, list)
	}
	t.Cleanup(func() {
		if f, ok := w.file.(*os.File); ok {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		}
	})
}

func TestAWriterThatFailedReleasesItsLock(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir, fs: &recFS{dir: dir, failSync: 2}}
	w := s.NewWriter(nil)
	now := time.Now().UTC()
	if err := w.Append(started(runA, now)); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(ev(runA, 2, events.NodeReady, events.NodeReadyPayload{}, now)); err == nil {
		t.Fatal("fsync failure swallowed")
	}
	rec, err := s.Read(runA)
	if err != nil || rec.Summary.Status != StatusInterrupted {
		t.Fatalf("a failed writer must not look alive: %v %+v", err, rec.Summary)
	}
	if err := w.Close(); err == nil {
		t.Fatal("Close must report the latched error")
	}
}

func TestLockedRunWithNoValidFirstLineIsSkippedNotCorrupt(t *testing.T) {
	logs := map[string]*string{"no log yet": nil, "empty log": ptr(""), "partial first line": ptr(`{"event_schema_version":1,"type":"run_st`)}
	for name, log := range logs {
		t.Run(name, func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			dir := filepath.Join(s.Dir, runA)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			lock, err := acquireRunLock(filepath.Join(dir, lockFile), false)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, graphFile), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if log != nil {
				if err := os.WriteFile(filepath.Join(dir, eventsFile), []byte(*log), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Read(runA); !errors.Is(err, ErrRunStarting) {
				t.Fatalf("a starting run is not corrupt: %v", err)
			}
			if list, err := s.List(); err != nil || len(list) != 0 {
				t.Fatalf("a starting run must be skipped: %v %+v", err, list)
			}
			if err := lock.release(); err != nil {
				t.Fatal(err)
			}
			if list, err := s.List(); err != nil || len(list) != 1 || list[0].Status != StatusCorrupt {
				t.Fatalf("once the writer is gone the same directory is corrupt: %v %+v", err, list)
			}
		})
	}
}

func TestPartialTrailingLineOfALiveRunIsIgnoredNotTruncated(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w := s.NewWriter(nil)
	t.Cleanup(func() { closeWriter(t, w) })
	if err := w.Append(started(runA, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, runA, eventsFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(f, `{"event_schema_version":11,"type":"node_rea`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Read(runA)
	if err != nil || rec.Summary.Status != StatusRunning || rec.Summary.Truncated || rec.Summary.EventCount != 1 {
		t.Fatalf("%v %+v", err, rec.Summary)
	}
}

func TestListNeverSeesATornEventWhileAppending(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const total = 400
	now := time.Now().UTC()
	writerErr := make(chan error, 1)
	go func() {
		w := s.NewWriter([]byte("g"))
		seq := uint64(1)
		if err := w.Append(started(runA, now)); err != nil {
			writerErr <- err
			return
		}
		for i := 0; i < total; i++ {
			seq++
			e := ev(runA, seq, events.LogChunk, events.LogChunkPayload{Stream: events.Stdout, Data: []byte(fmt.Sprintf("chunk %d", i))}, now)
			e.NodeID, e.NodeRunID = "n", "nr"
			if i%50 == 0 {
				e = ev(runA, seq, events.NodeReady, events.NodeReadyPayload{}, now)
				e.NodeID, e.NodeRunID = "n", "nr"
			}
			if err := w.Append(e); err != nil {
				writerErr <- err
				return
			}
		}
		if err := w.Append(finished(runA, seq+1, "succeeded", now)); err != nil {
			writerErr <- err
			return
		}
		writerErr <- w.Close()
	}()
	for {
		rec, err := s.Read(runA)
		switch {
		case errors.Is(err, ErrRunStarting), errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			t.Fatalf("a reader must never see a torn event: %v", err)
		}
		for i := 1; i < len(rec.Events); i++ {
			if rec.Events[i].Sequence <= rec.Events[i-1].Sequence {
				t.Fatalf("sequence not increasing at %d", i)
			}
		}
		if rec.Summary.Truncated {
			t.Fatal("a live run must not report a truncated tail")
		}
		if rec.Summary.Status == "succeeded" {
			if len(rec.Events) != total+2 {
				t.Fatalf("terminal run has %d events, want %d", len(rec.Events), total+2)
			}
			break
		}
	}
	if err := <-writerErr; err != nil {
		t.Fatal(err)
	}
}

func TestSecondWriterRefusesAnExistingRunDirectory(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w := s.NewWriter(nil)
	t.Cleanup(func() { closeWriter(t, w) })
	if err := w.Append(started(runA, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	err := s.NewWriter(nil).Append(started(runA, time.Now().UTC()))
	if err == nil {
		t.Fatal("a second writer opened a live run")
	}
	if rec, rerr := s.Read(runA); rerr != nil || rec.Summary.EventCount != 1 {
		t.Fatalf("the live run must be untouched: %v %+v", rerr, rec.Summary)
	}
	_, lerr := acquireRunLock(filepath.Join(s.Dir, runA, lockFile), false)
	if !errors.Is(lerr, ErrRunLocked) {
		t.Fatalf("an already locked run.lock must report ErrRunLocked: %v", lerr)
	}
}
