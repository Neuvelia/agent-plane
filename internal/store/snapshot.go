package store

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"agentplane/internal/domain/events"
)

// The per-run summary snapshot (F3.2 requirement 5) is `<run>/summary.json`:
// the run's Summary plus the size of the log it describes. It is an
// optimisation and never authoritative. The log wins on any disagreement, a
// missing or damaged snapshot only costs a parse, and nothing relies on the
// rename that publishes it being atomic across a crash (requirement 8).
const (
	snapshotFile = "summary.json"
	// maxSnapshotBytes bounds what List will read from an untrusted file.
	maxSnapshotBytes = 64 << 10
)

// snapshotRecord is the stored form: a Summary with the log size it was
// computed from, which is how staleness is detected without reading the log.
type snapshotRecord struct {
	Summary
	LogBytes int64 `json:"log_bytes"`
}

// SnapshotWriteError reports that a run's summary snapshot could not be
// written. It is never fatal: the snapshot is rebuildable from the log.
type SnapshotWriteError struct {
	RunID string
	Err   error
}

func (e *SnapshotWriteError) Error() string {
	return fmt.Sprintf("write summary snapshot of run %s: %v", e.RunID, e.Err)
}

func (e *SnapshotWriteError) Unwrap() error { return e.Err }

// SnapshotError reports the outcome of writing this run's snapshot at its
// terminal event; nil before that event and when it succeeded. The run result
// is unaffected by it.
func (w *Writer) SnapshotError() error { return w.snapshotErr }

// writeSnapshot publishes the snapshot of the log written so far. It runs after
// the terminal event's sync, while the writer still holds the run lock.
func (w *Writer) writeSnapshot() error {
	if w.runDir == "" {
		return nil // this writer created no run directory, so there is nothing to describe
	}
	return writeSnapshotFile(w.store.ops(), w.runDir, w.summary, w.logBytes)
}

// snapshotTempPath names a temporary file in dir that no other writer shares:
// concurrent listings in one server rewrite the same stale snapshot, and a
// shared name would let one writer's cleanup delete the other's file.
func snapshotTempPath(dir string) (string, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("name temporary file: %w", err)
	}
	return filepath.Join(dir, fmt.Sprintf("%s.%d.%x.tmp", snapshotFile, os.Getpid(), random)), nil
}

// writeSnapshotFile writes the snapshot to a uniquely named temporary file in
// the run directory, syncs it, and renames it into place.
func writeSnapshotFile(fs fsOps, dir string, summary Summary, logBytes int64) error {
	data, err := json.Marshal(snapshotRecord{Summary: summary, LogBytes: logBytes})
	if err != nil {
		return &SnapshotWriteError{RunID: summary.RunID, Err: fmt.Errorf("encode: %w", err)}
	}
	temp, err := snapshotTempPath(dir)
	if err != nil {
		return &SnapshotWriteError{RunID: summary.RunID, Err: err}
	}
	err = fs.WriteFileSync(temp, append(data, '\n'))
	if err == nil {
		err = fs.Rename(temp, filepath.Join(dir, snapshotFile))
	}
	if err == nil {
		return nil
	}
	if removeErr := os.Remove(temp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		err = errors.Join(err, removeErr)
	}
	return &SnapshotWriteError{RunID: summary.RunID, Err: err}
}

// terminalStatus reports whether status is a run_finished status, as opposed to
// the states a reader derives for a log with no terminal event.
func terminalStatus(status string) bool {
	switch status {
	case "", StatusRunning, StatusInterrupted, StatusCorrupt, StatusUnsupported:
		return false
	}
	return true
}

// trustedSnapshot returns the run's snapshot only when it can be believed
// without reading the log: it belongs to this run, describes a finished and
// complete log, and its recorded size equals the log's current size. Anything
// else, including an unreadable or oversized file, means "parse the log".
func (s Store) trustedSnapshot(runID string) (Summary, bool) {
	dir := filepath.Join(s.Dir, runID)
	file, err := os.Open(filepath.Join(dir, snapshotFile))
	if err != nil {
		return Summary{}, false
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSnapshotBytes+1))
	if closeErr := file.Close(); err != nil || closeErr != nil || len(data) > maxSnapshotBytes {
		return Summary{}, false
	}
	var record snapshotRecord
	if json.Unmarshal(data, &record) != nil {
		return Summary{}, false
	}
	info, err := os.Lstat(filepath.Join(dir, eventsFile))
	if err != nil || !info.Mode().IsRegular() || record.LogBytes != info.Size() {
		return Summary{}, false
	}
	if record.RunID != runID || !snapshotWorthy(record.Summary) {
		return Summary{}, false
	}
	return record.Summary, true
}

// snapshotWorthy reports whether a summary describes a finished, complete,
// readable log, the only kind a snapshot may stand in for.
func snapshotWorthy(summary Summary) bool {
	return terminalStatus(summary.Status) && !summary.Truncated && summary.Error == "" &&
		summary.EventCount > 0 && summary.LastSequence > 0 && summary.GraphID != "" && summary.FinishedAt != nil &&
		summary.MaxSchema <= events.SchemaVersion
}

// refreshSnapshot rewrites a stale or missing snapshot from a parsed log. It is
// best effort: it is skipped while a writer holds the run lock so it cannot race
// the writer's own snapshot, and a failure (a read-only store, a reader holding
// the file on Windows) is reported through SnapshotWarn and never fails List.
func (s Store) refreshSnapshot(runID string, summary Summary) {
	if !snapshotWorthy(summary) {
		return
	}
	dir := filepath.Join(s.Dir, runID)
	live, err := runLockHeld(filepath.Join(dir, lockFile))
	if err != nil && !errors.Is(err, ErrLockUnsupported) {
		s.warnSnapshot(runID, err)
		return
	}
	if live {
		return
	}
	info, err := os.Lstat(filepath.Join(dir, eventsFile))
	if err != nil {
		s.warnSnapshot(runID, err)
		return
	}
	if err := writeSnapshotFile(s.ops(), dir, summary, info.Size()); err != nil {
		s.warnSnapshot(runID, err)
	}
}

func (s Store) warnSnapshot(runID string, err error) {
	if s.SnapshotWarn != nil {
		s.SnapshotWarn(runID, err)
	}
}
