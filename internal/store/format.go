package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	formatFile = "store.json"
	// CurrentFormat is the store layout this build writes. A store without
	// store.json is format 0, the ADR-0019 layout, and stays fully readable.
	CurrentFormat = 1
)

// Retention bounds are explicit and local; zero means keep everything.
type Retention struct {
	MaxRuns       int   `json:"max_runs,omitempty"`
	MaxAgeDays    int   `json:"max_age_days,omitempty"`
	MaxTotalBytes int64 `json:"max_total_bytes,omitempty"`
}

// Format is the content of <store>/store.json. Unknown fields are tolerated so
// an older binary reads a store marked by a compatible newer one.
type Format struct {
	StoreFormat int        `json:"store_format"`
	Retention   *Retention `json:"retention,omitempty"`
}

// FormatError reports a store written by a newer build. Such a store can be
// listed and read but never written.
type FormatError struct{ Found, Supported int }

func (e *FormatError) Error() string {
	return fmt.Sprintf("store format %d is newer than this build supports (%d); the store is read-only here, upgrade agentplane to write to it", e.Found, e.Supported)
}

// ReadFormat loads store.json. A missing file or store directory is format 0.
func (s Store) ReadFormat() (Format, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, formatFile))
	if errors.Is(err, os.ErrNotExist) {
		return Format{}, nil
	}
	if err != nil {
		return Format{}, fmt.Errorf("read %s: %w", formatFile, err)
	}
	var f Format
	if err := json.Unmarshal(data, &f); err != nil {
		return Format{}, fmt.Errorf("parse %s: %w", formatFile, err)
	}
	if f.StoreFormat < 0 {
		return Format{}, fmt.Errorf("invalid %s: store_format must not be negative", formatFile)
	}
	if r := f.Retention; r != nil && (r.MaxRuns < 0 || r.MaxAgeDays < 0 || r.MaxTotalBytes < 0) {
		return Format{}, fmt.Errorf("invalid %s: retention values must not be negative", formatFile)
	}
	if f.StoreFormat > CurrentFormat {
		return f, &FormatError{Found: f.StoreFormat, Supported: CurrentFormat}
	}
	return f, nil
}

// ensureFormat refuses a newer store and creates store.json on the first write.
// The file appears atomically (temp file, sync, rename), so concurrent first
// writers all write identical content and a reader never sees a partial file.
func (s Store) ensureFormat() error {
	_, err := s.ReadFormat()
	if err != nil {
		return err
	}
	if _, serr := os.Stat(filepath.Join(s.Dir, formatFile)); serr == nil {
		return nil
	} else if !errors.Is(serr, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", formatFile, serr)
	}
	name, err := s.writeFormatTemp()
	if err == nil {
		err = os.Rename(name, filepath.Join(s.Dir, formatFile))
	}
	if err == nil {
		return nil
	}
	if name != "" {
		if rerr := os.Remove(name); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			err = errors.Join(err, rerr)
		}
	}
	// A concurrent first writer may have won the race with identical content.
	if _, serr := os.Stat(filepath.Join(s.Dir, formatFile)); serr == nil {
		return nil
	}
	return fmt.Errorf("create %s: %w", formatFile, err)
}

// writeFormatTemp writes a synced, owner-only temp file and returns its name,
// which is set even when a later step fails so the caller can remove it.
func (s Store) writeFormatTemp() (string, error) {
	tmp, err := os.CreateTemp(s.Dir, ".store-*.tmp")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_, err = fmt.Fprintf(tmp, "{\"store_format\":%d}\n", CurrentFormat)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, fileMode)
	}
	return name, err
}
