// Package store persists runs as an append-only JSONL event log plus a copy of
// the executed graph (ADR-0019). It adds no data beyond what the emitter has
// already redacted, and it has no index: listing scans the directory.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"agentplane/internal/domain/events"
)

const (
	eventsFile = "events.jsonl"
	graphFile  = "graph.yaml"
	// creatingPrefix names a run directory under construction; it is not a
	// valid run ID, so listing and reading ignore it.
	creatingPrefix = ".creating-"
	dirMode        = 0o700
	fileMode       = 0o600
)

// Status values reported for a stored run. A log without a terminal event is
// "running" while a writer holds its run lock and "interrupted" otherwise.
const (
	StatusRunning     = "running"
	StatusInterrupted = "interrupted"
	StatusCorrupt     = "corrupt"
	// StatusUnsupported marks a run written with a newer event schema than this
	// build decodes; its files are never modified.
	StatusUnsupported = "unsupported"
)

// ErrRunStarting means a live writer has created the run but has not yet made
// its first event durable; the run cannot be shown yet and is not corrupt.
var ErrRunStarting = errors.New("run is starting: no durable event yet")

// UnsupportedSchemaError reports an event from a schema newer than this build.
type UnsupportedSchemaError struct{ Version int }

func (e *UnsupportedSchemaError) Error() string {
	return fmt.Sprintf("unsupported event schema version %d (this build reads up to %d); upgrade agentplane to read this run", e.Version, events.SchemaVersion)
}

var runIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ValidRunID reports whether id can name a run directory. Run IDs come from
// untrusted request paths and directory listings, so nothing else is accepted.
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }

// Store is a directory of run directories.
type Store struct {
	Dir string
	// SnapshotWarn, when set, receives the failure of a best-effort snapshot
	// rewrite made while listing (an unwritable store, a sharing violation). A
	// snapshot is rebuildable, so such a failure never fails the listing; nil
	// means the caller does not want to hear about it.
	SnapshotWarn func(runID string, err error)
	fs           fsOps
	parsed       func(runID string) // test hook: called for every log List parses
}

// fsOps is the set of filesystem operations a run creation performs, in the
// order the writer issues them; a seam for fault injection and order tests.
type fsOps interface {
	MkdirAll(path string) error
	Mkdir(path string) error
	WriteFileSync(path string, data []byte) error
	CreateLog(path string) (logFile, error)
	SyncDir(path string) error
	Rename(oldPath, newPath string) error
}

func (s Store) ops() fsOps {
	if s.fs != nil {
		return s.fs
	}
	return osFS{}
}

type osFS struct{}

func (osFS) MkdirAll(path string) error { return os.MkdirAll(path, dirMode) }
func (osFS) Mkdir(path string) error    { return os.Mkdir(path, dirMode) }
func (osFS) SyncDir(path string) error  { return syncDir(path) }

func (osFS) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

func (osFS) WriteFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (osFS) CreateLog(path string) (logFile, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, fileMode)
}

// Summary describes one stored run without loading its whole log.
type Summary struct {
	RunID   string `json:"run_id"`
	GraphID string `json:"graph_id"`
	// GraphVersion is the semantic version hash of the executed graph, taken
	// from the first event; empty for an unreadable run.
	GraphVersion string    `json:"graph_version,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	Status       string    `json:"status"`
	EventCount   int       `json:"event_count"`
	Truncated    bool      `json:"truncated,omitempty"`
	// LastSequence is the sequence of the last durable event; MinSchema and
	// MaxSchema bound the event schema versions present in the log.
	LastSequence uint64 `json:"last_sequence,omitempty"`
	MinSchema    int    `json:"min_schema,omitempty"`
	MaxSchema    int    `json:"max_schema,omitempty"`
	// InterruptedAtSequence is the last durable sequence of a run that has no
	// terminal event and no live writer.
	InterruptedAtSequence uint64 `json:"interrupted_at_sequence,omitempty"`
	// Error explains a run that cannot be read or was stopped; Status is
	// "corrupt" for an unreadable log.
	Error string `json:"error,omitempty"`
}

// Record is a fully read run.
type Record struct {
	Summary Summary
	Graph   []byte
	Events  []events.Event
}

// Writer appends one run's events. The run directory is created from the first
// event's run ID so the store never invents an identifier of its own.
type Writer struct {
	store Store
	graph []byte
	file  logFile
	lock  *runLock
	err   error
	// summary and logBytes describe the log written so far; they become the
	// run's snapshot at the terminal event. snapshotErr is that write's outcome.
	summary     Summary
	logBytes    int64
	snapshotErr error
}

// logFile is the part of *os.File the writer needs; a seam for fault injection.
type logFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// NewWriter returns a Writer that will persist graph (the exact definition
// bytes that were executed) next to the log. Nothing touches disk until the
// first Append.
func (s Store) NewWriter(graph []byte) *Writer {
	return &Writer{store: s, graph: append([]byte(nil), graph...)}
}

// Append writes one event as one line. Lifecycle facts are fsynced before it
// returns; log and progress chunks are written but synced with the next
// lifecycle fact, so a killed process never loses a lifecycle fact it
// acknowledged but may lose trailing log chunks (ADR-0019).
func (w *Writer) Append(e events.Event) error {
	if w.err != nil {
		return w.err
	}
	if w.file == nil {
		if err := w.open(e); err != nil {
			return w.fail(err)
		}
	}
	line, err := json.Marshal(e)
	if err != nil {
		return w.fail(fmt.Errorf("encode event: %w", err))
	}
	line = append(line, '\n')
	n, err := w.file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return w.fail(fmt.Errorf("write event log: %w", err))
	}
	w.logBytes += int64(len(line))
	w.summary.EventCount++
	terminal := w.summary.observe(e)
	if !events.IsProgress(e.Type) {
		if err := w.file.Sync(); err != nil {
			return w.fail(fmt.Errorf("sync event log: %w", err))
		}
	}
	if terminal {
		w.snapshotErr = w.writeSnapshot()
	}
	return nil
}

// fail latches err and releases the run lock: a writer that can never append
// again must not make its run look live.
func (w *Writer) fail(err error) error {
	w.err = err
	if lerr := w.lock.release(); lerr != nil {
		w.err = errors.Join(err, lerr)
	}
	return w.err
}

func (w *Writer) open(first events.Event) error {
	id := string(first.RunID)
	if !ValidRunID(id) {
		return fmt.Errorf("refusing to store run with invalid ID %q", id)
	}
	w.summary = Summary{RunID: id}
	fs := w.store.ops()
	if err := fs.MkdirAll(w.store.Dir); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	if err := w.store.ensureFormat(); err != nil {
		return err
	}
	dir := filepath.Join(w.store.Dir, id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("create run directory: %w", os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat run directory: %w", err)
	}
	build := dir
	if publishByRename {
		build = filepath.Join(w.store.Dir, creatingPrefix+id)
	}
	if err := fs.Mkdir(build); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	f, err := w.populate(fs, build, id)
	if err == nil && build != dir {
		if err = fs.Rename(build, dir); err != nil {
			err = fmt.Errorf("publish run directory: %w", err)
		}
	}
	if err == nil {
		if err = fs.SyncDir(w.store.Dir); err != nil {
			err = fmt.Errorf("sync store directory: %w", err)
		}
	}
	if err != nil {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
		if build != dir {
			err = errors.Join(err, os.RemoveAll(build))
		}
		return err
	}
	w.file = f
	return nil
}

// populate fills a run directory that no reader can list yet: the lock first, so
// that nothing is ever visible without a holder, then graph.yaml and the log.
func (w *Writer) populate(fs fsOps, dir, id string) (logFile, error) {
	lock, err := acquireRunLock(filepath.Join(dir, lockFile), true)
	if err != nil {
		return nil, fmt.Errorf("lock run %s: %w", id, err)
	}
	w.lock = lock
	if err := fs.WriteFileSync(filepath.Join(dir, graphFile), w.graph); err != nil {
		return nil, fmt.Errorf("write graph snapshot: %w", err)
	}
	f, err := fs.CreateLog(filepath.Join(dir, eventsFile))
	if err != nil {
		return nil, fmt.Errorf("create event log: %w", err)
	}
	if err := fs.SyncDir(dir); err != nil {
		return f, fmt.Errorf("sync run directory: %w", err)
	}
	return f, nil
}

// Durability scope: a lifecycle event is fsynced before Append returns, so it
// survives the process being killed. Creation is ordered graph.yaml (synced),
// event log, run directory, then the store directory, so after power loss a run
// directory is either absent or has its graph snapshot. Where a directory can be
// renamed while files in it are open (not Windows) the run is built under a
// hidden name and renamed into place, so a lister never sees a half-created
// run; on Windows it is built in place and a locked run without a first event
// is listed as starting, not corrupt. Directory sync is a no-op on Windows
// (syncdir_windows.go); the log itself still is synced.

// Close flushes and closes the log. It is safe to call without any Append.
func (w *Writer) Close() error {
	if w.file == nil {
		return w.err
	}
	err := w.file.Sync()
	if cerr := w.file.Close(); err == nil {
		err = cerr
	}
	w.file = nil
	if lerr := w.lock.release(); err == nil {
		err = lerr
	}
	if w.err != nil {
		return w.err
	}
	return err
}

// Check fails early when the directory cannot hold runs, so an unwritable
// store is a preflight error rather than a mid-run failure.
func (s Store) Check() error {
	if err := os.MkdirAll(s.Dir, dirMode); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	if _, err := s.ReadFormat(); err != nil {
		return err
	}
	probe, err := os.CreateTemp(s.Dir, ".probe-*")
	if err != nil {
		return fmt.Errorf("store directory is not writable: %w", err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close store probe: %w", err)
	}
	return os.Remove(name)
}
