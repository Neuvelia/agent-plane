package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
)

func TestReaderConsumesOnlyNewlineTerminatedLines(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	write(t, s, runA, started(runA, now))
	valid, err := json.Marshal(ev(runA, 2, events.NodeReady, events.NodeReadyPayload{}, now))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, runA, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(valid); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Read(runA)
	if err != nil || rec.Summary.EventCount != 1 || !rec.Summary.Truncated || rec.Summary.LastSequence != 1 {
		t.Fatalf("a valid line without a newline is not an event: %v %+v", err, rec.Summary)
	}
}

// futureSchemaRun stores a run and rewrites its schema version to a newer one.
func futureSchemaRun(t *testing.T, s Store, run string) (path string, before []byte) {
	t.Helper()
	now := time.Now().UTC()
	write(t, s, run, started(run, now), finished(run, 2, "succeeded", now))
	path = filepath.Join(s.Dir, run, "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	future := strings.ReplaceAll(string(data), fmt.Sprintf(`"event_schema_version":%d`, events.SchemaVersion), fmt.Sprintf(`"event_schema_version":%d`, events.SchemaVersion+1))
	if err := os.WriteFile(path, []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	// The edit keeps the log's size, so the snapshot of the schema-11 write would
	// still match it. A newer build writes its own snapshot, which carries its
	// schema and is not trusted (TestSnapshotOfANewerSchemaIsNotTrusted); drop the
	// stale one to model a run this build has never summarized.
	if err := os.Remove(filepath.Join(s.Dir, run, snapshotFile)); err != nil {
		t.Fatal(err)
	}
	return path, []byte(future)
}

func TestUnsupportedSchemaIsTypedAndListedUnsupported(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	path, before := futureSchemaRun(t, s, runA)
	now := time.Now().UTC()
	write(t, s, runB, started(runB, now), finished(runB, 2, "succeeded", now))
	_, err := s.Read(runA)
	var unsupported *UnsupportedSchemaError
	if !errors.As(err, &unsupported) || unsupported.Version != events.SchemaVersion+1 {
		t.Fatalf("want *UnsupportedSchemaError for schema %d, got %v", events.SchemaVersion+1, err)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %+v", err, list)
	}
	byID := map[string]Summary{list[0].RunID: list[0], list[1].RunID: list[1]}
	got := byID[runA]
	if got.Status != StatusUnsupported || got.GraphID != "g" || got.StartedAt.IsZero() || !strings.Contains(got.Error, fmt.Sprint(events.SchemaVersion+1)) {
		t.Fatalf("unsupported run summary: %+v", got)
	}
	if byID[runB].Status != "succeeded" {
		t.Fatalf("a supported run must not be affected: %+v", byID[runB])
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("an unsupported run must never be modified (%v)", err)
	}
}

// Fixtures under testdata/schemas/vN.jsonl are hand-written minimal logs
// (run_started, node_started, node_evidence from schema 2, node_finished,
// run_finished) stamped with schema N. Tests never rewrite them; a schema bump
// must add the next fixture or TestSchemaFixturesRoundTrip fails.
func TestSchemaFixturesRoundTrip(t *testing.T) {
	for n := 1; n <= events.SchemaVersion; n++ {
		data, err := os.ReadFile(filepath.Join("testdata", "schemas", fmt.Sprintf("v%d.jsonl", n)))
		if err != nil {
			t.Fatalf("schema %d has no fixture: %v", n, err)
		}
		s := Store{Dir: t.TempDir()}
		storeFixture(t, s, data)
		rec, err := s.Read(runA)
		if err != nil || rec.Summary.Status != "succeeded" || rec.Summary.MinSchema != n || rec.Summary.MaxSchema != n {
			t.Fatalf("schema %d fixture: %v %+v", n, err, rec.Summary)
		}
		if rec.Summary.LastSequence != uint64(rec.Summary.EventCount) {
			t.Fatalf("schema %d fixture: last sequence %d for %d events", n, rec.Summary.LastSequence, rec.Summary.EventCount)
		}
	}
	current, err := os.ReadFile(filepath.Join("testdata", "schemas", fmt.Sprintf("v%d.jsonl", events.SchemaVersion)))
	if err != nil {
		t.Fatal(err)
	}
	s := Store{Dir: t.TempDir()}
	future := strings.ReplaceAll(string(current), fmt.Sprintf(`"event_schema_version":%d`, events.SchemaVersion), fmt.Sprintf(`"event_schema_version":%d`, events.SchemaVersion+1))
	storeFixture(t, s, []byte(future))
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Status != StatusUnsupported {
		t.Fatalf("a schema newer than current must be unsupported: %v %+v", err, list)
	}
}

func storeFixture(t *testing.T, s Store, log []byte) {
	t.Helper()
	dir := filepath.Join(s.Dir, runA)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte("schema_version: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), log, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryCarriesRunStartedProvenance(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	write(t, s, runA,
		ev(runA, 1, events.RunStarted, events.RunStartedPayload{NodeCount: 1, RepoName: "r", RepoBranch: "main", RepoCommit: "abc", RuntimeVersion: "1.2"}, now),
		finished(runA, 2, "succeeded", now))
	rec, err := s.Read(runA)
	if err != nil {
		t.Fatal(err)
	}
	got := rec.Summary
	if got.RepoName != "r" || got.RepoBranch != "main" || got.RepoCommit != "abc" || got.RuntimeVersion != "1.2" {
		t.Fatalf("summary = %#v", got)
	}
	// A log written before the fields existed decodes with them empty.
	write(t, s, runB, started(runB, now), finished(runB, 2, "succeeded", now))
	old, err := s.Read(runB)
	if err != nil || old.Summary.RepoBranch != "" || old.Summary.RuntimeVersion != "" {
		t.Fatalf("old summary = %#v, %v", old.Summary, err)
	}
}
