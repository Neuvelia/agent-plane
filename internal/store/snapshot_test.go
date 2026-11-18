package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/testenv"
)

func snapshotPath(s Store, run string) string { return filepath.Join(s.Dir, run, snapshotFile) }

func finishedRun(t *testing.T, s Store, run string, at time.Time) {
	t.Helper()
	write(t, s, run, started(run, at), finished(run, 2, "succeeded", at))
}

func readSnapshot(t *testing.T, s Store, run string) snapshotRecord {
	t.Helper()
	data, err := os.ReadFile(snapshotPath(s, run))
	if err != nil {
		t.Fatal(err)
	}
	var rec snapshotRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func rewriteSnapshot(t *testing.T, s Store, run string, edit func(*snapshotRecord)) {
	t.Helper()
	rec := readSnapshot(t, s, run)
	edit(&rec)
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshotPath(s, run), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// parseCounter counts the logs List had to parse.
func parseCounter(s *Store) *[]string {
	var parsed []string
	s.parsed = func(run string) { parsed = append(parsed, run) }
	return &parsed
}

func TestWriterWritesTheSnapshotAtTheTerminalEvent(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	finishedRun(t, s, runA, time.Now().UTC())
	rec := readSnapshot(t, s, runA)
	log, err := os.Stat(filepath.Join(s.Dir, runA, eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	if rec.RunID != runA || rec.Status != "succeeded" || rec.EventCount != 2 || rec.LastSequence != 2 || rec.LogBytes != log.Size() || rec.MaxSchema != events.SchemaVersion {
		t.Fatalf("snapshot %+v for a %d byte log", rec, log.Size())
	}
	if info, err := os.Stat(snapshotPath(s, runA)); err != nil || (testenv.HasUnixPermissions() && info.Mode().Perm() != fileMode) {
		t.Fatalf("snapshot mode: %v %v", info, err)
	}
}

func TestNoSnapshotExistsBeforeTheTerminalEvent(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w := s.NewWriter([]byte("schema_version: 0\n"))
	if err := w.Append(started(runA, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshotPath(s, runA)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a run without a terminal event must have no snapshot: %v", err)
	}
	closeWriter(t, w)
}

func TestListParsesNoLogWhenTheSnapshotIsValid(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	finishedRun(t, s, runA, now)
	finishedRun(t, s, runB, now.Add(time.Second))
	parsed := parseCounter(&s)
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].RunID != runB {
		t.Fatalf("%v %+v", err, list)
	}
	if len(*parsed) != 0 {
		t.Fatalf("List parsed %v although both snapshots are valid", *parsed)
	}
}

func TestStaleOrDeletedSnapshotNeverChangesListOutput(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	finishedRun(t, s, runA, now)
	finishedRun(t, s, runB, now.Add(time.Second))
	want, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	damage := map[string]func(run string){
		"deleted": func(run string) {
			if err := os.Remove(snapshotPath(s, run)); err != nil {
				t.Fatal(err)
			}
		},
		"truncated": func(run string) {
			data, _ := os.ReadFile(snapshotPath(s, run))
			if err := os.WriteFile(snapshotPath(s, run), data[:len(data)/2], 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"garbage": func(run string) {
			if err := os.WriteFile(snapshotPath(s, run), []byte("not json at all"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		// A stale snapshot describes a log that has since changed, so its content
		// differs from the log's and only the size check can tell them apart.
		"stale: log size differs": func(run string) {
			rewriteSnapshot(t, s, run, func(r *snapshotRecord) { r.LogBytes++; r.Status = "failed"; r.EventCount = 99 })
		},
		"belongs to another run": func(run string) {
			rewriteSnapshot(t, s, run, func(r *snapshotRecord) { r.RunID = runB + "x"; r.GraphID = "other" })
		},
		"claims a live status": func(run string) {
			rewriteSnapshot(t, s, run, func(r *snapshotRecord) { r.Status = StatusRunning })
		},
		"oversized": func(run string) {
			if err := os.WriteFile(snapshotPath(s, run), []byte(strings.Repeat(" ", maxSnapshotBytes+1)), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, apply := range damage {
		for _, run := range []string{runA, runB} {
			apply(run)
		}
		got, err := s.List()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: List changed:\n got %+v\nwant %+v (%v)", name, got, want, err)
		}
	}
}

func TestSnapshotIsIgnoredWhileTheRunIsUnterminated(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	write(t, s, runA, started(runA, time.Now().UTC()))
	log, err := os.Stat(filepath.Join(s.Dir, runA, eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	// A snapshot that matches the log's size but does not describe a finished run.
	forged := snapshotRecord{Summary: Summary{RunID: runA, GraphID: "g", Status: StatusInterrupted, EventCount: 1, LastSequence: 1}, LogBytes: log.Size()}
	data, _ := json.Marshal(forged)
	if err := os.WriteFile(snapshotPath(s, runA), data, 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseCounter(&s)
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Status != StatusInterrupted || list[0].InterruptedAtSequence != 1 {
		t.Fatalf("%v %+v", err, list)
	}
	if len(*parsed) != 1 {
		t.Fatalf("an unterminated run must be parsed, parsed %v", *parsed)
	}
}

func TestSnapshotOfANewerSchemaIsNotTrusted(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	finishedRun(t, s, runA, time.Now().UTC())
	rewriteSnapshot(t, s, runA, func(r *snapshotRecord) { r.MaxSchema = events.SchemaVersion + 1 })
	parsed := parseCounter(&s)
	if _, err := s.List(); err != nil || len(*parsed) != 1 {
		t.Fatalf("a snapshot from a newer schema must send List to the log: %v parsed=%v", err, *parsed)
	}
}

// snapshotFS wraps the real filesystem with a record of snapshot operations and
// optional failures of them.
type snapshotFS struct {
	osFS
	ops        []string
	failWrite  error
	failRename error
}

func (f *snapshotFS) WriteFileSync(path string, data []byte) error {
	if strings.Contains(filepath.Base(path), snapshotFile) {
		f.ops = append(f.ops, "write-sync-snapshot-temp")
		if f.failWrite != nil {
			return f.failWrite
		}
	}
	return f.osFS.WriteFileSync(path, data)
}

func (f *snapshotFS) Rename(oldPath, newPath string) error {
	if filepath.Base(newPath) == snapshotFile {
		f.ops = append(f.ops, "rename-snapshot")
		if f.failRename != nil {
			return f.failRename
		}
	}
	return f.osFS.Rename(oldPath, newPath)
}

func TestSnapshotIsWrittenToATempFileSyncedThenRenamed(t *testing.T) {
	fsys := &snapshotFS{}
	s := Store{Dir: t.TempDir(), fs: fsys}
	finishedRun(t, s, runA, time.Now().UTC())
	if got := strings.Join(fsys.ops, ","); got != "write-sync-snapshot-temp,rename-snapshot" {
		t.Fatalf("snapshot operations %q", got)
	}
	matches, _ := filepath.Glob(filepath.Join(s.Dir, runA, "*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("a temporary file was left behind: %v", matches)
	}
}

func TestSnapshotFailureIsReportedNotFatal(t *testing.T) {
	for name, fsys := range map[string]*snapshotFS{
		"write":  {failWrite: errors.New("injected disk full")},
		"rename": {failRename: errors.New("injected sharing violation")},
	} {
		t.Run(name, func(t *testing.T) {
			s := Store{Dir: t.TempDir(), fs: fsys}
			w := s.NewWriter([]byte("schema_version: 0\n"))
			now := time.Now().UTC()
			if err := w.Append(started(runA, now)); err != nil {
				t.Fatal(err)
			}
			if err := w.Append(finished(runA, 2, "succeeded", now)); err != nil {
				t.Fatalf("a snapshot failure must not fail the run: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			var snapshotErr *SnapshotWriteError
			if err := w.SnapshotError(); !errors.As(err, &snapshotErr) || snapshotErr.RunID != runA {
				t.Fatalf("want a typed *SnapshotWriteError, got %v", err)
			}
			if _, err := os.Stat(snapshotPath(s, runA)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("no snapshot may exist after a failed write: %v", err)
			}
			if matches, _ := filepath.Glob(filepath.Join(s.Dir, runA, "*.tmp")); len(matches) != 0 {
				t.Fatalf("temporary file left behind: %v", matches)
			}
			list, err := Store{Dir: s.Dir}.List()
			if err != nil || len(list) != 1 || list[0].Status != "succeeded" {
				t.Fatalf("the log alone must list the run: %v %+v", err, list)
			}
		})
	}
}

func TestListRewritesAStaleSnapshotBestEffortAndSkipsWhileLocked(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w := s.NewWriter([]byte("schema_version: 0\n"))
	now := time.Now().UTC()
	for _, e := range []events.Event{started(runA, now), finished(runA, 2, "succeeded", now)} {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	// The writer still holds the run lock, so a List must not write its snapshot.
	if err := os.Remove(snapshotPath(s, runA)); err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(); err != nil || len(list) != 1 || list[0].Status != "succeeded" {
		t.Fatalf("%v %+v", err, list)
	}
	if _, err := os.Stat(snapshotPath(s, runA)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("List rewrote a snapshot while the writer held the lock: %v", err)
	}
	closeWriter(t, w)
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	if rec := readSnapshot(t, s, runA); rec.Status != "succeeded" || rec.EventCount != 2 {
		t.Fatalf("List must rebuild a missing snapshot once no writer holds the run: %+v", rec)
	}
}

func TestListWithAnUnwritableStoreStillListsAndWarns(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	finishedRun(t, s, runA, time.Now().UTC())
	if err := os.Remove(snapshotPath(s, runA)); err != nil {
		t.Fatal(err)
	}
	warned := map[string]error{}
	readOnly := Store{Dir: s.Dir, fs: &snapshotFS{failWrite: errors.New("injected: read-only file system")}, SnapshotWarn: func(run string, err error) { warned[run] = err }}
	list, err := readOnly.List()
	if err != nil || len(list) != 1 || list[0].Status != "succeeded" {
		t.Fatalf("%v %+v", err, list)
	}
	var snapshotErr *SnapshotWriteError
	if !errors.As(warned[runA], &snapshotErr) {
		t.Fatalf("the failed rewrite must be reported through SnapshotWarn: %v", warned)
	}
	if _, err := os.Stat(snapshotPath(s, runA)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nothing may be written: %v", err)
	}
}

func TestReadOnlyStoreStillListsWithoutRewriting(t *testing.T) {
	testenv.RequireUnixPermissions(t)
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not restrict root")
	}
	s := Store{Dir: t.TempDir()}
	finishedRun(t, s, runA, time.Now().UTC())
	if err := os.Remove(snapshotPath(s, runA)); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(s.Dir, runA)
	if err := os.Chmod(runDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(runDir, dirMode) })
	warned := 0
	s.SnapshotWarn = func(string, error) { warned++ }
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Status != "succeeded" || warned != 1 {
		t.Fatalf("%v %+v warned=%d", err, list, warned)
	}
	if _, err := os.Stat(snapshotPath(s, runA)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a read-only store must not be written: %v", err)
	}
}
