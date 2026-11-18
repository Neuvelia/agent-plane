// Package store persists runs as an append-only JSONL event log plus a copy of
// the executed graph (ADR-0019). It adds no data beyond what the emitter has
// already redacted, and it has no index: listing scans the directory.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"agentplane/internal/domain/events"
)

const (
	eventsFile = "events.jsonl"
	graphFile  = "graph.yaml"
	dirMode    = 0o700
	fileMode   = 0o600
)

// Status values reported for a stored run. A log without a terminal event is
// "interrupted" for a store reader; a live server overlays "running" itself.
const (
	StatusInterrupted = "interrupted"
	StatusCorrupt     = "corrupt"
	// StatusUnsupported marks a run written with a newer event schema than this
	// build decodes; its files are never modified.
	StatusUnsupported = "unsupported"
)

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
	fs  fsOps
}

// fsOps is the set of filesystem operations a run creation performs, in the
// order the writer issues them; a seam for fault injection and order tests.
type fsOps interface {
	MkdirAll(path string) error
	Mkdir(path string) error
	WriteFileSync(path string, data []byte) error
	CreateLog(path string) (logFile, error)
	SyncDir(path string) error
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
	err   error
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
			w.err = err
			return err
		}
	}
	line, err := json.Marshal(e)
	if err != nil {
		w.err = fmt.Errorf("encode event: %w", err)
		return w.err
	}
	line = append(line, '\n')
	n, err := w.file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = fmt.Errorf("write event log: %w", err)
		return w.err
	}
	if !events.IsProgress(e.Type) {
		if err := w.file.Sync(); err != nil {
			w.err = fmt.Errorf("sync event log: %w", err)
			return w.err
		}
	}
	return nil
}

func (w *Writer) open(first events.Event) error {
	id := string(first.RunID)
	if !ValidRunID(id) {
		return fmt.Errorf("refusing to store run with invalid ID %q", id)
	}
	fs := w.store.ops()
	if err := fs.MkdirAll(w.store.Dir); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	if err := w.store.ensureFormat(); err != nil {
		return err
	}
	dir := filepath.Join(w.store.Dir, id)
	if err := fs.Mkdir(dir); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	if err := fs.WriteFileSync(filepath.Join(dir, graphFile), w.graph); err != nil {
		return fmt.Errorf("write graph snapshot: %w", err)
	}
	f, err := fs.CreateLog(filepath.Join(dir, eventsFile))
	if err != nil {
		return fmt.Errorf("create event log: %w", err)
	}
	for _, d := range []string{dir, w.store.Dir} {
		if err := fs.SyncDir(d); err != nil {
			if cerr := f.Close(); cerr != nil {
				err = errors.Join(err, cerr)
			}
			return fmt.Errorf("sync run directory entry: %w", err)
		}
	}
	w.file = f
	return nil
}

// Durability scope: a lifecycle event is fsynced before Append returns, so it
// survives the process being killed. Creation is ordered graph.yaml (synced),
// event log, then the run and store directories, so after power loss a run
// directory is either absent or has its graph snapshot. On Windows directory
// entries cannot be synced (syncdir_windows.go); the log itself still is.

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

// List returns runs newest first. Unreadable or foreign entries are skipped.
func (s Store) List() ([]Summary, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read store directory: %w", err)
	}
	out := []Summary{}
	for _, entry := range entries {
		if !entry.IsDir() || !ValidRunID(entry.Name()) {
			continue
		}
		record, err := s.Read(entry.Name())
		if err != nil {
			// Never drop a run silently: an unreadable one is listed as corrupt.
			var unsupported *UnsupportedSchemaError
			if errors.As(err, &unsupported) {
				out = append(out, s.unsupportedSummary(entry.Name(), err))
				continue
			}
			out = append(out, Summary{RunID: entry.Name(), Status: StatusCorrupt, Error: err.Error()})
			continue
		}
		out = append(out, record.Summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].RunID < out[j].RunID
	})
	return out, nil
}

// Read loads one run. A final line without a newline is never an event; it is
// reported through Summary.Truncated while earlier events stay readable.
// Corruption anywhere else is an error because later events could not be
// trusted to follow it, and an event from a newer schema is a typed
// *UnsupportedSchemaError.
func (s Store) Read(runID string) (Record, error) {
	if !ValidRunID(runID) {
		return Record{}, fmt.Errorf("invalid run ID %q", runID)
	}
	dir := filepath.Join(s.Dir, runID)
	graph, err := os.ReadFile(filepath.Join(dir, graphFile))
	if err != nil {
		return Record{}, fmt.Errorf("read graph snapshot: %w", err)
	}
	f, err := os.Open(filepath.Join(dir, eventsFile))
	if err != nil {
		return Record{}, fmt.Errorf("read event log: %w", err)
	}
	defer f.Close()
	rec := Record{Graph: graph, Summary: Summary{RunID: runID, Status: StatusInterrupted}}
	reader := bufio.NewReader(f)
	var lastSequence uint64
	finished := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && readErr != io.EOF {
			return Record{}, fmt.Errorf("read event log: %w", readErr)
		}
		// Only a newline-terminated line is an event: a final line without one
		// may still be growing or torn, whether or not it happens to parse.
		if len(line) == 0 || line[len(line)-1] != '\n' {
			if len(bytes.TrimSpace(line)) > 0 {
				rec.Summary.Truncated = true
			}
			break
		}
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var e events.Event
			// Unmarshal also validates the event envelope and payload, including
			// the schema version, so an unsupported record is an error here.
			if err := json.Unmarshal(line, &e); err != nil || string(e.RunID) != runID || e.Sequence <= lastSequence {
				if err != nil {
					if v := peekSchemaVersion(line); v > events.SchemaVersion {
						return Record{}, fmt.Errorf("event log %s: %w", runID, &UnsupportedSchemaError{Version: v})
					}
				} else {
					err = errors.New("event out of order or from another run")
				}
				return Record{}, fmt.Errorf("corrupt event log %s: %w", runID, err)
			}
			if len(rec.Events) == 0 && e.Type != events.RunStarted {
				return Record{}, fmt.Errorf("corrupt event log %s: first event is %q, not run_started", runID, e.Type)
			}
			if finished {
				return Record{}, fmt.Errorf("corrupt event log %s: events after run_finished", runID)
			}
			lastSequence = e.Sequence
			rec.Events = append(rec.Events, e)
			if rec.Summary.GraphID == "" {
				rec.Summary.GraphID = e.GraphID
				rec.Summary.GraphVersion = e.GraphVersion
				rec.Summary.StartedAt = e.Timestamp
			}
			if p, ok := e.Payload.(events.RunFinishedPayload); ok {
				rec.Summary.Status = p.Status
				finished = true
			}
			rec.Summary.LastSequence = e.Sequence
			if rec.Summary.MinSchema == 0 || e.SchemaVersion < rec.Summary.MinSchema {
				rec.Summary.MinSchema = e.SchemaVersion
			}
			if e.SchemaVersion > rec.Summary.MaxSchema {
				rec.Summary.MaxSchema = e.SchemaVersion
			}
		}
	}
	if len(rec.Events) == 0 {
		// Nothing readable was ever recorded: not a run that can be shown.
		return Record{}, fmt.Errorf("corrupt event log %s: no readable events", runID)
	}
	rec.Summary.EventCount = len(rec.Events)
	return rec, nil
}

func peekSchemaVersion(line []byte) int {
	var v struct {
		Version int `json:"event_schema_version"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return 0
	}
	return v.Version
}

// unsupportedSummary lists a run this build cannot decode using only the
// identifying fields of its first line, so it still sorts and displays.
func (s Store) unsupportedSummary(runID string, cause error) Summary {
	sum := Summary{RunID: runID, Status: StatusUnsupported, Error: cause.Error()}
	f, err := os.Open(filepath.Join(s.Dir, runID, eventsFile))
	if err != nil {
		return sum
	}
	line, err := bufio.NewReader(io.LimitReader(f, 1<<16)).ReadBytes('\n')
	if cerr := f.Close(); cerr != nil || (err != nil && !errors.Is(err, io.EOF)) {
		return sum
	}
	var header struct {
		GraphID      string    `json:"graph_id"`
		GraphVersion string    `json:"graph_version"`
		Timestamp    time.Time `json:"timestamp"`
	}
	if json.Unmarshal(line, &header) == nil {
		sum.GraphID, sum.GraphVersion, sum.StartedAt = header.GraphID, header.GraphVersion, header.Timestamp
	}
	return sum
}
