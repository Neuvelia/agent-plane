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
	"time"

	"agentplane/internal/domain/events"
)

// Read loads one run. A final line without a newline is never an event; it is
// reported through Summary.Truncated while earlier events stay readable.
// Corruption anywhere else is an error because later events could not be
// trusted to follow it, and an event from a newer schema is a typed
// *UnsupportedSchemaError.
func (s Store) Read(runID string) (Record, error) {
	if !ValidRunID(runID) {
		return Record{}, fmt.Errorf("invalid run ID %q", runID)
	}
	files, err := openRunFiles(filepath.Join(s.Dir, runID))
	if err != nil {
		return Record{}, err
	}
	defer files.log.Close()
	rec := Record{Graph: files.graph, Summary: Summary{RunID: runID, Status: StatusInterrupted}}
	finished, err := scanLog(&rec, files.log, files.live)
	if err != nil {
		return Record{}, err
	}
	if len(rec.Events) == 0 {
		if files.live {
			return Record{}, ErrRunStarting
		}
		// Nothing readable was ever recorded: not a run that can be shown.
		return Record{}, fmt.Errorf("corrupt event log %s: no readable events", runID)
	}
	rec.Summary.EventCount = len(rec.Events)
	if !finished {
		if files.live {
			rec.Summary.Status = StatusRunning
		} else {
			rec.Summary.InterruptedAtSequence = rec.Summary.LastSequence
		}
	}
	return rec, nil
}

// runFiles is what Read needs from a run directory: the graph snapshot, the
// open log, and whether a live writer still holds the run lock.
type runFiles struct {
	graph []byte
	log   *os.File
	live  bool
}

// openRunFiles opens both files, then probes the lock, then lets the caller
// read: a run that is visible has its lock held by the writer, and the writer
// releases it only after its final sync, so "not held" guarantees the log read
// afterwards is complete.
func openRunFiles(dir string) (runFiles, error) {
	graph, graphErr := os.ReadFile(filepath.Join(dir, graphFile))
	var f *os.File
	var logErr error
	if graphErr == nil {
		f, logErr = os.Open(filepath.Join(dir, eventsFile))
	}
	live, err := runLockHeld(filepath.Join(dir, lockFile))
	if err != nil && !errors.Is(err, ErrLockUnsupported) {
		return runFiles{}, closeAfter(f, err)
	}
	switch {
	case graphErr != nil && live && errors.Is(graphErr, os.ErrNotExist),
		logErr != nil && live && errors.Is(logErr, os.ErrNotExist):
		return runFiles{}, closeAfter(f, ErrRunStarting)
	case graphErr != nil:
		return runFiles{}, fmt.Errorf("read graph snapshot: %w", graphErr)
	case logErr != nil:
		return runFiles{}, fmt.Errorf("read event log: %w", logErr)
	}
	return runFiles{graph: graph, log: f, live: live}, nil
}

// scanLog appends every event of the log to rec and reports whether a
// run_finished event was seen. Only a newline-terminated line is an event: a
// final line without one may still be growing or torn, whether or not it
// happens to parse.
func scanLog(rec *Record, log io.Reader, live bool) (finished bool, err error) {
	reader := bufio.NewReader(log)
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && readErr != io.EOF {
			return false, fmt.Errorf("read event log: %w", readErr)
		}
		if len(line) == 0 || line[len(line)-1] != '\n' {
			// A live writer's partial line is simply not written yet.
			if !live && len(bytes.TrimSpace(line)) > 0 {
				rec.Summary.Truncated = true
			}
			return finished, nil
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		e, err := decodeNextEvent(rec, line, finished)
		if err != nil {
			return false, err
		}
		rec.Events = append(rec.Events, e)
		finished = rec.Summary.observe(e) || finished
	}
}

// decodeNextEvent decodes the next line and checks that it may follow the events
// already read: same run, rising sequence, run_started first, nothing after
// run_finished. Unmarshal also validates the event envelope and payload,
// including the schema version, so an unsupported record is an error here.
func decodeNextEvent(rec *Record, line []byte, finished bool) (events.Event, error) {
	runID := rec.Summary.RunID
	var e events.Event
	err := json.Unmarshal(line, &e)
	if err == nil && (string(e.RunID) != runID || e.Sequence <= rec.Summary.LastSequence) {
		err = errors.New("event out of order or from another run")
	} else if err != nil {
		if v := peekSchemaVersion(line); v > events.SchemaVersion {
			return events.Event{}, fmt.Errorf("event log %s: %w", runID, &UnsupportedSchemaError{Version: v})
		}
	}
	if err != nil {
		return events.Event{}, fmt.Errorf("corrupt event log %s: %w", runID, err)
	}
	if len(rec.Events) == 0 && e.Type != events.RunStarted {
		return events.Event{}, fmt.Errorf("corrupt event log %s: first event is %q, not run_started", runID, e.Type)
	}
	if finished {
		return events.Event{}, fmt.Errorf("corrupt event log %s: events after run_finished", runID)
	}
	return e, nil
}

// observe folds one accepted event into the summary and reports whether it was
// the terminal run_finished event.
func (s *Summary) observe(e events.Event) (terminal bool) {
	if s.GraphID == "" {
		s.GraphID, s.GraphVersion, s.StartedAt = e.GraphID, e.GraphVersion, e.Timestamp
	}
	if p, ok := e.Payload.(events.RunFinishedPayload); ok {
		s.Status = p.Status
		finishedAt := e.Timestamp
		s.FinishedAt = &finishedAt
		terminal = true
	}
	s.LastSequence = e.Sequence
	if s.MinSchema == 0 || e.SchemaVersion < s.MinSchema {
		s.MinSchema = e.SchemaVersion
	}
	if e.SchemaVersion > s.MaxSchema {
		s.MaxSchema = e.SchemaVersion
	}
	return terminal
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

// closeAfter closes f when it was opened and keeps err as the result.
func closeAfter(f *os.File, err error) error {
	if f == nil {
		return err
	}
	return errors.Join(err, f.Close())
}
