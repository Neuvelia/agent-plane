package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/events"
)

const runA = "11111111-1111-4111-8111-111111111111"
const runB = "22222222-2222-4222-8222-222222222222"

func ev(run string, seq uint64, typ events.Type, p events.Payload, at time.Time) events.Event {
	return events.Event{SchemaVersion: events.SchemaVersion, Type: typ, Timestamp: at, Sequence: seq, RunID: events.RunID(run), GraphID: "g", GraphVersion: "sha256-v0:x", Payload: p}
}

func started(run string, at time.Time) events.Event {
	return ev(run, 1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}, at)
}
func finished(run string, seq uint64, status string, at time.Time) events.Event {
	return ev(run, seq, events.RunFinished, events.RunFinishedPayload{Status: status}, at)
}

func write(t *testing.T, s Store, run string, evs ...events.Event) {
	t.Helper()
	w := s.NewWriter([]byte("schema_version: 0\n"))
	for _, e := range evs {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripPreservesEventsAndGraph(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	log := ev(runA, 2, events.LogChunk, events.LogChunkPayload{Stream: events.Stdout, Data: []byte{0, 1, 255}}, now)
	log.NodeID, log.NodeRunID = "n", "nr"
	in := []events.Event{started(runA, now), log, finished(runA, 3, "succeeded", now)}
	write(t, s, runA, in...)
	rec, err := s.Read(runA)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Summary.Status != "succeeded" || rec.Summary.EventCount != 3 || rec.Summary.GraphID != "g" || string(rec.Graph) != "schema_version: 0\n" {
		t.Fatalf("%+v", rec.Summary)
	}
	for i := range in {
		a, _ := json.Marshal(in[i])
		b, _ := json.Marshal(rec.Events[i])
		if string(a) != string(b) {
			t.Fatalf("event %d changed:\n%s\n%s", i, a, b)
		}
	}
}

func TestPermissionsAreOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runs")
	s := Store{Dir: dir}
	write(t, s, runA, started(runA, time.Now().UTC()))
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, runA): 0o700, filepath.Join(dir, runA, "events.jsonl"): 0o600, filepath.Join(dir, runA, "graph.yaml"): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: %v %v", path, info.Mode().Perm(), err)
		}
	}
}

func TestRunWithoutTerminalEventIsInterruptedAndEarlierEventsRead(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	write(t, s, runA, started(runA, time.Now().UTC()))
	rec, err := s.Read(runA)
	if err != nil || rec.Summary.Status != StatusInterrupted || rec.Summary.EventCount != 1 {
		t.Fatalf("%v %+v", err, rec.Summary)
	}
}

func TestTruncatedTailIsReportedNotFatal(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	write(t, s, runA, started(runA, now), ev(runA, 2, events.NodeReady, events.NodeReadyPayload{}, now))
	path := filepath.Join(s.Dir, runA, "events.jsonl")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"event_schema_version":9,"type":"node_start`) // killed mid-write
	f.Close()
	rec, err := s.Read(runA)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Summary.Truncated || rec.Summary.EventCount != 2 {
		t.Fatalf("%+v", rec.Summary)
	}
}

func TestCorruptionInTheMiddleIsAnError(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	write(t, s, runA, started(runA, now), finished(runA, 2, "succeeded", now))
	path := filepath.Join(s.Dir, runA, "events.jsonl")
	data, _ := os.ReadFile(path)
	lines := strings.SplitN(string(data), "\n", 2)
	os.WriteFile(path, []byte("garbage\n"+lines[1]), 0o600)
	if _, err := s.Read(runA); err == nil {
		t.Fatal("mid-log corruption must not be silently skipped")
	}
}

func TestOutOfOrderOrForeignEventsAreRejected(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	write(t, s, runA, started(runA, now), ev(runA, 1, events.NodeReady, events.NodeReadyPayload{}, now), finished(runA, 3, "succeeded", now))
	if _, err := s.Read(runA); err == nil {
		t.Fatal("non-monotonic sequence accepted")
	}
}

func TestRunIDValidationBlocksPathTraversal(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, id := range []string{"", "..", "../x", runA + "/..", "a/b", "ABCDEF01-2345-4678-89AB-CDEF01234567", runA + "x"} {
		if ValidRunID(id) {
			t.Fatalf("%q accepted", id)
		}
		if _, err := s.Read(id); err == nil {
			t.Fatalf("Read(%q) succeeded", id)
		}
	}
	w := s.NewWriter(nil)
	bad := started("../escape", time.Now().UTC())
	if err := w.Append(bad); err == nil {
		t.Fatal("writer accepted a hostile run ID")
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 0 {
		t.Fatalf("hostile run created %v", entries)
	}
}

func TestListIsNewestFirstAndSkipsForeignEntries(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	write(t, s, runA, started(runA, old), finished(runA, 2, "failed", old))
	write(t, s, runB, started(runB, newer), finished(runB, 2, "succeeded", newer))
	os.Mkdir(filepath.Join(s.Dir, "not-a-run"), 0o700)
	os.WriteFile(filepath.Join(s.Dir, "stray.txt"), []byte("x"), 0o600)
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].RunID != runB || list[1].Status != "failed" {
		t.Fatalf("%v %+v", err, list)
	}
	if missing, err := (Store{Dir: filepath.Join(s.Dir, "nope")}).List(); err != nil || len(missing) != 0 {
		t.Fatalf("missing store must list empty: %v %v", missing, err)
	}
}

func TestSecondWriterCannotOverwriteAnExistingRun(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	write(t, s, runA, started(runA, time.Now().UTC()))
	if err := s.NewWriter(nil).Append(started(runA, time.Now().UTC())); err == nil {
		t.Fatal("append-only log was reopened for the same run ID")
	}
}

func TestCheckFailsForUnwritableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("x"), 0o600)
	if err := (Store{Dir: filepath.Join(file, "sub")}).Check(); err == nil {
		t.Fatal("a store under a regular file must fail its check")
	}
}
