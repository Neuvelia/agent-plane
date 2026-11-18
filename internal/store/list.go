package store

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

// List returns runs newest first. Foreign entries are skipped and a run that
// cannot be read is listed as corrupt with the reason, never dropped. A
// terminal run whose summary snapshot is valid is listed from that one small
// file without parsing its log (F3.2 requirement 5).
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
		if summary, listed := s.listEntry(entry.Name()); listed {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].RunID < out[j].RunID
	})
	return out, nil
}

// listEntry summarizes one run for List. The snapshot is only an optimisation:
// whenever it is not trusted the log decides, and a correct snapshot is written
// back best effort. listed is false for a run that is still being created.
func (s Store) listEntry(runID string) (summary Summary, listed bool) {
	if summary, ok := s.trustedSnapshot(runID); ok {
		return summary, true
	}
	summary, listed = s.summaryFromLog(runID)
	if listed {
		s.refreshSnapshot(runID, summary)
	}
	return summary, listed
}

// summarize returns a run's summary the way List does but never writes: the
// trusted snapshot, else the log. Prune classifies runs with it.
func (s Store) summarize(runID string) (summary Summary, listed bool) {
	if summary, ok := s.trustedSnapshot(runID); ok {
		return summary, true
	}
	return s.summaryFromLog(runID)
}

// summaryFromLog parses the log. A run that cannot be read is reported as
// corrupt (or unsupported) with the reason, never dropped.
func (s Store) summaryFromLog(runID string) (summary Summary, listed bool) {
	if s.parsed != nil {
		s.parsed(runID)
	}
	record, err := s.Read(runID)
	if err == nil {
		return record.Summary, true
	}
	if errors.Is(err, ErrRunStarting) {
		return Summary{}, false
	}
	var unsupported *UnsupportedSchemaError
	if errors.As(err, &unsupported) {
		return s.unsupportedSummary(runID, err), true
	}
	return Summary{RunID: runID, Status: StatusCorrupt, Error: err.Error()}, true
}
