package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/testenv"
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
	if rec.Summary.Status != "succeeded" || rec.Summary.EventCount != 3 || rec.Summary.GraphID != "g" || rec.Summary.GraphVersion != "sha256-v0:x" || string(rec.Graph) != "schema_version: 0\n" {
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

func TestRecordedValuesRoundTripAndOlderSchemaRunsStayReadable(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	text := "hi"
	input := ev(runA, 2, events.ValueRecorded, events.ValueRecordedPayload{Port: "inputs.job", Type: "string", SizeBytes: 2, SHA256: "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4", Value: &text}, now)
	capped := ev(runA, 3, events.ValueRecorded, events.ValueRecordedPayload{NodeRunID: "nr", Port: "out", Type: "json", SizeBytes: 99, SHA256: strings.Repeat("a", 64), Truncated: true}, now)
	capped.NodeID, capped.NodeRunID = "n", "nr"
	in := []events.Event{started(runA, now), input, capped, finished(runA, 4, "succeeded", now)}
	write(t, s, runA, in...)
	rec, err := s.Read(runA)
	if err != nil || len(rec.Events) != len(in) {
		t.Fatalf("%v %d", err, len(rec.Events))
	}
	for i := range in {
		a, _ := json.Marshal(in[i])
		b, _ := json.Marshal(rec.Events[i])
		if string(a) != string(b) {
			t.Fatalf("event %d changed:\n%s\n%s", i, a, b)
		}
	}
	// A run written by an older engine (schema 9) is read without values.
	path := filepath.Join(s.Dir, runB)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"event_schema_version":9,"type":"run_started","timestamp":"2026-11-14T09:00:00Z","sequence":1,"run_id":"` + runB + `","graph_id":"g","graph_version":"v9","payload":{"node_count":0}}` + "\n" +
		`{"event_schema_version":9,"type":"run_finished","timestamp":"2026-11-14T09:00:01Z","sequence":2,"run_id":"` + runB + `","graph_id":"g","graph_version":"v9","payload":{"status":"succeeded","duration_ms":1,"node_counts":{"succeeded":0,"failed":0,"skipped":0,"cancelled":0}}}` + "\n"
	os.WriteFile(filepath.Join(path, "events.jsonl"), []byte(old), 0o600)
	os.WriteFile(filepath.Join(path, "graph.yaml"), []byte("schema_version: 0\n"), 0o600)
	rec, err = s.Read(runB)
	if err != nil || rec.Summary.Status != "succeeded" || rec.Summary.GraphVersion != "v9" {
		t.Fatalf("schema 9 run: %+v %v", rec.Summary, err)
	}
}

func TestPermissionsAreOwnerOnly(t *testing.T) {
	testenv.RequireUnixPermissions(t)
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
	fmt.Fprintf(f, `{"event_schema_version":%d,"type":"node_start`, events.SchemaVersion) // killed mid-write
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

// recordingFile counts writes and syncs so durability behaviour is observable.
type recordingFile struct {
	ops      []string
	writeErr error
	syncErr  error
}

func (f *recordingFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.ops = append(f.ops, "write")
	return len(p), nil
}
func (f *recordingFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	f.ops = append(f.ops, "sync")
	return nil
}
func (f *recordingFile) Close() error { f.ops = append(f.ops, "close"); return nil }

func TestLifecycleEventsAreSyncedAndProgressEventsAreNot(t *testing.T) {
	f := &recordingFile{}
	w := &Writer{file: f}
	now := time.Now().UTC()
	log := ev(runA, 2, events.LogChunk, events.LogChunkPayload{Stream: events.Stdout, Data: []byte("x")}, now)
	log.NodeID, log.NodeRunID = "n", "nr"
	for _, e := range []events.Event{started(runA, now), log, log, finished(runA, 3, "succeeded", now)} {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	// lifecycle: write+sync; two log chunks: write only; final lifecycle: write+sync.
	if got := strings.Join(f.ops, ","); got != "write,sync,write,write,write,sync" {
		t.Fatalf("durability sequence = %s", got)
	}
}

func TestWriterReportsAndLatchesWriteAndSyncFailures(t *testing.T) {
	now := time.Now().UTC()
	for name, f := range map[string]*recordingFile{"write": {writeErr: os.ErrClosed}, "sync": {syncErr: os.ErrClosed}} {
		t.Run(name, func(t *testing.T) {
			w := &Writer{file: f}
			if err := w.Append(started(runA, now)); err == nil {
				t.Fatal("a failed " + name + " was swallowed")
			}
			calls := len(f.ops)
			if err := w.Append(finished(runA, 2, "succeeded", now)); err == nil || len(f.ops) != calls {
				t.Fatalf("after a failure the writer must stay failed and write nothing more (ops %v, err %v)", f.ops, err)
			}
		})
	}
	if err := (&Writer{file: &recordingFile{}}).Append(events.Event{}); err == nil {
		t.Fatal("an unencodable event must be an error")
	}
}

func TestReaderRejectsLogsThatDoNotStartWithRunStartedOrContinueAfterRunFinished(t *testing.T) {
	now := time.Now().UTC()
	for name, evs := range map[string][]events.Event{
		"first event is not run_started": {ev(runA, 1, events.NodeReady, events.NodeReadyPayload{}, now), finished(runA, 2, "succeeded", now)},
		"events after run_finished":      {started(runA, now), finished(runA, 2, "succeeded", now), finished(runA, 3, "failed", now)},
	} {
		t.Run(name, func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			write(t, s, runA, evs...)
			if _, err := s.Read(runA); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestReaderRejectsAnUnsupportedSchemaVersion(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	write(t, s, runA, started(runA, time.Now().UTC()))
	path := filepath.Join(s.Dir, runA, "events.jsonl")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(data), fmt.Sprintf(`"event_schema_version":%d`, events.SchemaVersion), `"event_schema_version":99`, 1)), 0o600)
	_, err := s.Read(runA)
	var unsupported *UnsupportedSchemaError
	if !errors.As(err, &unsupported) || unsupported.Version != 99 {
		t.Fatalf("an event from a future schema must be a typed error, got %v", err)
	}
}

func TestEmptyOrHeaderOnlyLogIsCorruptNotAnInterruptedRun(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "torn first line": fmt.Sprintf(`{"event_schema_version":%d,"type":"run_st`, events.SchemaVersion)} {
		t.Run(name, func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			dir := filepath.Join(s.Dir, runA)
			os.MkdirAll(dir, 0o700)
			os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte("x"), 0o600)
			os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(content), 0o600)
			if _, err := s.Read(runA); err == nil {
				t.Fatal("a log with no readable event was presented as a run")
			}
			if list, _ := s.List(); len(list) != 1 || list[0].Status != StatusCorrupt {
				t.Fatalf("%+v", list)
			}
		})
	}
}

func TestListReportsUnreadableRunsAsCorruptWithTheReason(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	write(t, s, runA, started(runA, time.Now().UTC()))
	os.Remove(filepath.Join(s.Dir, runA, "graph.yaml"))
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Status != StatusCorrupt || list[0].Error == "" || list[0].RunID != runA {
		t.Fatalf("%v %+v", err, list)
	}
}

// Workspace isolation facts (event schema 10, F3.1) are lifecycle-grade: they
// round trip through the JSONL store unchanged and are fsynced like other
// non-progress facts.
func TestWorkspaceFactsRoundTripAndAreSynced(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	const unit = "refs/agentplane/" + runA + "/units/worker-0123456789abcdef"
	const tip = "refs/agentplane/" + runA + "/integrated"
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	node := func(e events.Event) events.Event { e.NodeID, e.NodeRunID = "worker", "nr"; return e }
	attempt := func(e events.Event) events.Event { e = node(e); e.AttemptID = "att"; return e }
	in := []events.Event{
		started(runA, now),
		node(ev(runA, 2, events.WorkspacePrepared, events.WorkspacePreparedPayload{BaseSHA: sha, Mode: events.WorkspaceModeWorktree, Path: "/w", SourceRoot: "/s"}, now)),
		attempt(ev(runA, 3, events.WorkspaceIntegrated, events.WorkspaceIntegratedPayload{UnitRef: unit, UnitCommit: sha, IntegratedRef: tip, IntegratedCommit: sha, ChangedPaths: 2}, now)),
		attempt(ev(runA, 4, events.WorkspaceConflict, events.WorkspaceConflictPayload{UnitRef: unit, UnitCommit: sha, TipCommit: sha, Paths: []string{"a.txt"}}, now)),
		node(ev(runA, 5, events.WorkspaceReleased, events.WorkspaceReleasedPayload{Action: events.WorkspaceRemoved, Reason: events.ReleaseIntegrated, Path: "/w"}, now)),
		ev(runA, 6, events.SourceWorkspaceChanged, events.SourceWorkspaceChangedPayload{Paths: []string{}, Indeterminate: true}, now),
		finished(runA, 7, "succeeded", now),
	}
	s := Store{Dir: t.TempDir()}
	write(t, s, runA, in...)
	rec, err := s.Read(runA)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Events) != len(in) {
		t.Fatalf("read %d events, wrote %d", len(rec.Events), len(in))
	}
	for i := range in {
		a, _ := json.Marshal(in[i])
		b, _ := json.Marshal(rec.Events[i])
		if string(a) != string(b) {
			t.Fatalf("event %d changed:\n%s\n%s", i, a, b)
		}
	}

	f := &recordingFile{}
	w := &Writer{file: f}
	for _, e := range in[:3] {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(f.ops, ","); got != "write,sync,write,sync,write,sync" {
		t.Fatalf("workspace facts must be fsynced like other lifecycle facts: %s", got)
	}
}

// recFS records the creation sequence and can wrap the log with faults.
type recFS struct {
	osFS
	dir      string
	ops      []string
	failSync int // fail the Nth Sync of the log (1-based); 0 never
	short    bool
}

func (r *recFS) Mkdir(path string) error {
	r.ops = append(r.ops, "mkdir-run")
	return r.osFS.Mkdir(path)
}

func (r *recFS) WriteFileSync(path string, data []byte) error {
	r.ops = append(r.ops, "write-sync-"+filepath.Base(path))
	return r.osFS.WriteFileSync(path, data)
}

func (r *recFS) CreateLog(path string) (logFile, error) {
	r.ops = append(r.ops, "create-log")
	f, err := r.osFS.CreateLog(path)
	if err != nil {
		return nil, err
	}
	return &faultLog{File: f.(*os.File), failSync: r.failSync, short: r.short}, nil
}

func (r *recFS) SyncDir(path string) error {
	if path == r.dir {
		r.ops = append(r.ops, "sync-store-dir")
	} else {
		r.ops = append(r.ops, "sync-run-dir")
	}
	return r.osFS.SyncDir(path)
}

// faultLog drops unsynced bytes when a sync fails, as a crash would.
type faultLog struct {
	*os.File
	failSync, syncs int
	short           bool
	size, durable   int64
}

func (f *faultLog) Write(p []byte) (int, error) {
	if f.short && len(p) > 1 {
		n, err := f.File.Write(p[:len(p)/2])
		f.size += int64(n)
		return n, err
	}
	n, err := f.File.Write(p)
	f.size += int64(n)
	return n, err
}

func (f *faultLog) Sync() error {
	f.syncs++
	if f.failSync != 0 && f.syncs == f.failSync {
		if err := f.Truncate(f.durable); err != nil {
			return err
		}
		return os.ErrClosed
	}
	f.durable = f.size
	return f.File.Sync()
}

func TestCreationOrderSyncsGraphBeforeLogThenDirectories(t *testing.T) {
	dir := t.TempDir()
	rec := &recFS{dir: dir}
	s := Store{Dir: dir, fs: rec}
	write(t, s, runA, started(runA, time.Now().UTC()))
	want := "mkdir-run,write-sync-graph.yaml,create-log,sync-run-dir,sync-store-dir"
	if got := strings.Join(rec.ops, ","); got != want {
		t.Fatalf("creation sequence = %s, want %s", got, want)
	}
}

func TestRunDirectoryWithoutAValidFirstLineIsNeverHalfReadable(t *testing.T) {
	for name, log := range map[string]*string{"graph only": nil, "empty log": ptr(""), "partial line": ptr(`{"event_schema_version":1,"type":"run_st`)} {
		t.Run(name, func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			dir := filepath.Join(s.Dir, runA)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if log != nil {
				if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(*log), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Read(runA); err == nil {
				t.Fatal("Read accepted a run without a valid first line")
			}
			list, err := s.List()
			if err != nil || len(list) != 1 || list[0].Status != StatusCorrupt {
				t.Fatalf("%v %+v", err, list)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestInjectedFsyncFailureStopsTheRunAndLeavesAReadablePrefix(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir, fs: &recFS{dir: dir, failSync: 3}}
	now := time.Now().UTC()
	w := s.NewWriter([]byte("x"))
	for _, e := range []events.Event{started(runA, now), ev(runA, 2, events.NodeReady, events.NodeReadyPayload{}, now)} {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	third := ev(runA, 3, events.NodeReady, events.NodeReadyPayload{}, now)
	err := w.Append(third)
	if err == nil {
		t.Fatal("a failed fsync was swallowed")
	}
	if again := w.Append(finished(runA, 4, "succeeded", now)); !errors.Is(again, err) {
		t.Fatalf("later Append must return the latched error, got %v", again)
	}
	if cerr := w.Close(); cerr == nil {
		t.Fatal("Close must keep reporting the latched error")
	}
	rec, rerr := s.Read(runA)
	if rerr != nil || rec.Summary.EventCount != 2 || rec.Summary.Status != StatusInterrupted {
		t.Fatalf("%v %+v", rerr, rec.Summary)
	}
}

func TestShortWriteStopsTheRunAndLeavesAReadablePrefix(t *testing.T) {
	dir := t.TempDir()
	rec := &recFS{dir: dir}
	s := Store{Dir: dir, fs: rec}
	now := time.Now().UTC()
	w := s.NewWriter([]byte("x"))
	if err := w.Append(started(runA, now)); err != nil {
		t.Fatal(err)
	}
	f := w.file.(*faultLog)
	f.short = true
	err := w.Append(ev(runA, 2, events.NodeReady, events.NodeReadyPayload{}, now))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write must surface io.ErrShortWrite, got %v", err)
	}
	if again := w.Append(finished(runA, 3, "succeeded", now)); again == nil {
		t.Fatal("the writer must stay failed after a short write")
	}
	if err := w.Close(); err == nil {
		t.Fatal("Close must report the latched error")
	}
	got, rerr := s.Read(runA)
	if rerr != nil || got.Summary.EventCount != 1 || !got.Summary.Truncated {
		t.Fatalf("%v %+v", rerr, got.Summary)
	}
}
