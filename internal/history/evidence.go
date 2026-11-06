package history

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxEvidenceFileBytes = 10 << 20
const maxEvidenceRunBytes = 100 << 20
const maxEvidenceChunkBytes = 1 << 20
const evidenceSweepBatchSize = 128

func validEvidenceOmissionReason(reason string) bool {
	switch reason {
	case "preimage_missing", "diff_unavailable", "redaction_failed", "size_limit", "capture_failed", "unsafe_path":
		return true
	default:
		return false
	}
}

// SafeEvidence is constructed only after the caller has projected and
// redacted the full content. The forbidden-value check prevents a known value
// from reaching a managed file through an erroneous caller.
type SafeEvidence struct {
	data   []byte
	digest string
}

func ParseSafeEvidence(data []byte, forbidden []string) (SafeEvidence, error) {
	if len(data) > maxEvidenceFileBytes {
		return SafeEvidence{}, errors.New("managed evidence exceeds 10 MiB")
	}
	for _, value := range forbidden {
		if value != "" && bytes.Contains(data, []byte(value)) {
			return SafeEvidence{}, errors.New("managed evidence contains a configured private value")
		}
	}
	copyData := append([]byte(nil), data...)
	sum := sha256.Sum256(copyData)
	return SafeEvidence{data: copyData, digest: hex.EncodeToString(sum[:])}, nil
}

func (value SafeEvidence) Bytes() []byte { return append([]byte(nil), value.data...) }

type EvidenceCapture struct {
	EvidenceID, RunID, AttemptID, NodeRunID, Kind string
	CreatedAt                                     time.Time
	Content                                       *SafeEvidence
	OmissionReason                                string
}

type EvidenceChunk struct {
	Availability   string
	OmissionReason string
	TotalLength    int64
	Digest         string
	Data           []byte
	NextOffset     int64
	End            bool
}

func (s *Store) evidenceRoot(create bool) (*os.Root, error) {
	state, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, fmt.Errorf("%w: open evidence state: %v", ErrUnavailable, err)
	}
	defer state.Close()
	info, err := state.Lstat("evidence")
	if errors.Is(err, os.ErrNotExist) && create {
		if err := state.Mkdir("evidence", 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: create evidence directory: %v", ErrUnavailable, err)
		}
		info, err = state.Lstat("evidence")
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: unsafe evidence directory", ErrUnavailable)
	}
	root, err := state.OpenRoot("evidence")
	if err != nil {
		return nil, fmt.Errorf("%w: pin evidence directory: %v", ErrUnavailable, err)
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("%w: evidence directory changed", ErrUnavailable)
	}
	return root, nil
}

func evidenceRelativePath(name string) string { return "evidence/" + name }

func validEvidencePath(path string) (string, bool) {
	if !strings.HasPrefix(path, "evidence/") || len(path) != len("evidence/")+32 {
		return "", false
	}
	name := strings.TrimPrefix(path, "evidence/")
	if filepath.Base(name) != name {
		return "", false
	}
	if _, err := hex.DecodeString(name); err != nil {
		return "", false
	}
	return name, true
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (s *Store) sweepEvidence(ctx context.Context) error {
	_, err := s.sweepEvidenceCount(ctx)
	return err
}

// walkEvidenceEntries never asks the directory reader to materialize the
// entire directory, including when the directory contains many orphan files.
func walkEvidenceEntries(read func(int) ([]os.DirEntry, error), visit func(os.DirEntry) error) error {
	for {
		entries, err := read(evidenceSweepBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, entry := range entries {
			if visitErr := visit(entry); visitErr != nil {
				return visitErr
			}
		}
		if errors.Is(err, io.EOF) || len(entries) == 0 {
			return nil
		}
	}
}

func (s *Store) sweepEvidenceCount(ctx context.Context) (int, error) {
	state, err := os.OpenRoot(s.dir)
	if err != nil {
		return 0, fmt.Errorf("%w: open state for evidence sweep: %v", ErrUnavailable, err)
	}
	_, err = state.Lstat("evidence")
	state.Close()
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: inspect evidence directory: %v", ErrUnavailable, err)
	}
	root, err := s.evidenceRoot(false)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	removed := 0
	for {
		directory, err := root.Open(".")
		if err != nil {
			return removed, fmt.Errorf("%w: enumerate evidence directory: %v", ErrUnavailable, err)
		}
		passRemoved := 0
		walkErr := walkEvidenceEntries(directory.ReadDir, func(entry os.DirEntry) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := entry.Name()
			if _, valid := validEvidencePath(evidenceRelativePath(name)); !valid {
				return nil
			}
			info, err := root.Lstat(name)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return fmt.Errorf("%w: unsafe evidence sweep candidate", ErrUnavailable)
			}
			var references int
			if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM managed_evidence WHERE relative_path=?", evidenceRelativePath(name)).Scan(&references); err != nil {
				return fmt.Errorf("%w: check evidence references: %v", ErrUnavailable, err)
			}
			if references == 0 {
				if err := root.Remove(name); err != nil {
					return fmt.Errorf("%w: remove orphan evidence: %v", ErrUnavailable, err)
				}
				removed++
				passRemoved++
			}
			return nil
		})
		closeErr := directory.Close()
		if passRemoved > 0 {
			if err := syncRoot(root); err != nil {
				return removed, fmt.Errorf("%w: sync evidence sweep: %v", ErrUnavailable, err)
			}
		}
		if walkErr != nil {
			return removed, fmt.Errorf("%w: enumerate evidence files: %v", ErrUnavailable, walkErr)
		}
		if closeErr != nil {
			return removed, fmt.Errorf("%w: close evidence directory: %v", ErrUnavailable, closeErr)
		}
		if passRemoved == 0 {
			return removed, nil
		}
	}
}

func (s *Store) CaptureEvidence(ctx context.Context, record EvidenceCapture) error {
	if record.EvidenceID == "" || record.RunID == "" || record.AttemptID == "" || record.Kind == "" || record.CreatedAt.IsZero() ||
		len(record.EvidenceID) > 256 || len(record.RunID) > 256 || len(record.AttemptID) > 256 || len(record.NodeRunID) > 256 || len(record.Kind) > 64 {
		return errors.New("invalid managed evidence identity")
	}
	available := record.Content != nil
	if available {
		if len(record.Content.data) > maxEvidenceFileBytes || record.Content.digest == "" || record.OmissionReason != "" {
			return errors.New("invalid available managed evidence")
		}
	} else if !validEvidenceOmissionReason(record.OmissionReason) {
		return errors.New("unavailable evidence requires an approved reason code")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin evidence capture: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	var state StorageState
	if err := tx.QueryRowContext(ctx, "SELECT storage_state FROM runs WHERE run_id=?", record.RunID).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("%w: read capture run: %v", ErrUnavailable, err)
	}
	if state != Active {
		return ErrConflict
	}
	var relativePath any
	var length any
	var digest any
	var reason any
	availability := "unavailable"
	var root *os.Root
	var name string
	committed := false
	if available {
		var used int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(byte_length),0) FROM managed_evidence WHERE run_id=? AND availability='available'", record.RunID).Scan(&used); err != nil {
			return fmt.Errorf("%w: read evidence budget: %v", ErrUnavailable, err)
		}
		if used < 0 || used > maxEvidenceRunBytes-int64(len(record.Content.data)) {
			return fmt.Errorf("%w: managed evidence run budget exceeded", ErrUnavailable)
		}
		root, err = s.evidenceRoot(true)
		if err != nil {
			return err
		}
		defer root.Close()
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return fmt.Errorf("%w: create evidence name: %v", ErrUnavailable, err)
		}
		name = hex.EncodeToString(random[:])
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("%w: create evidence file: %v", ErrUnavailable, err)
		}
		defer func() {
			if !committed {
				_ = root.Remove(name)
				_ = syncRoot(root)
			}
		}()
		if _, err := io.Copy(file, bytes.NewReader(record.Content.data)); err != nil {
			file.Close()
			return fmt.Errorf("%w: write evidence file: %v", ErrUnavailable, err)
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return fmt.Errorf("%w: sync evidence file: %v", ErrUnavailable, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("%w: close evidence file: %v", ErrUnavailable, err)
		}
		if err := syncRoot(root); err != nil {
			return fmt.Errorf("%w: sync evidence directory: %v", ErrUnavailable, err)
		}
		availability = "available"
		relativePath = evidenceRelativePath(name)
		length = len(record.Content.data)
		digest = record.Content.digest
	} else {
		reason = record.OmissionReason
	}
	var nodeID any
	if record.NodeRunID != "" {
		nodeID = record.NodeRunID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO managed_evidence(evidence_id,run_id,attempt_id,node_run_id,kind,availability,created_at,relative_path,byte_length,digest,omission_reason)
	VALUES(?,?,?,?,?,?,?,?,?,?,?)`, record.EvidenceID, record.RunID, record.AttemptID, nodeID, record.Kind, availability, formatHistoryTime(record.CreatedAt), relativePath, length, digest, reason); err != nil {
		return fmt.Errorf("%w: insert evidence reference: %v", ErrUnavailable, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit evidence reference: %v", ErrUnavailable, err)
	}
	committed = true
	return nil
}

func (s *Store) Evidence(ctx context.Context, evidenceID string, offset int64, limit int) (EvidenceChunk, error) {
	if evidenceID == "" || offset < 0 || limit < 1 || limit > maxEvidenceChunkBytes {
		return EvidenceChunk{}, errors.New("invalid evidence chunk request")
	}
	var availability string
	var path, digest, reason sql.NullString
	var length sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT availability,relative_path,byte_length,digest,omission_reason FROM managed_evidence WHERE evidence_id=?", evidenceID).Scan(&availability, &path, &length, &digest, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return EvidenceChunk{}, ErrNotFound
	}
	if err != nil {
		return EvidenceChunk{}, fmt.Errorf("%w: read evidence reference: %v", ErrUnavailable, err)
	}
	if availability == "unavailable" {
		if !reason.Valid || !validEvidenceOmissionReason(reason.String) || path.Valid || length.Valid || digest.Valid {
			return EvidenceChunk{}, fmt.Errorf("%w: invalid unavailable evidence reference", ErrUnavailable)
		}
		return EvidenceChunk{Availability: availability, OmissionReason: reason.String, End: true}, nil
	}
	if availability != "available" || !path.Valid || !length.Valid || length.Int64 < 0 || length.Int64 > maxEvidenceFileBytes || !digest.Valid || reason.Valid {
		return EvidenceChunk{}, fmt.Errorf("%w: invalid evidence reference", ErrUnavailable)
	}
	name, ok := validEvidencePath(path.String)
	if !ok {
		return EvidenceChunk{}, fmt.Errorf("%w: evidence path escapes managed directory", ErrUnavailable)
	}
	root, err := s.evidenceRoot(false)
	if err != nil {
		return EvidenceChunk{}, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != length.Int64 {
		return EvidenceChunk{}, fmt.Errorf("%w: unsafe or changed evidence file", ErrUnavailable)
	}
	file, err := root.Open(name)
	if err != nil {
		return EvidenceChunk{}, fmt.Errorf("%w: open evidence file: %v", ErrUnavailable, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return EvidenceChunk{}, fmt.Errorf("%w: evidence file changed", ErrUnavailable)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxEvidenceFileBytes+1))
	if err != nil || int64(len(data)) != length.Int64 {
		return EvidenceChunk{}, fmt.Errorf("%w: evidence length changed", ErrUnavailable)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest.String {
		return EvidenceChunk{}, fmt.Errorf("%w: evidence digest mismatch", ErrUnavailable)
	}
	if offset > int64(len(data)) {
		return EvidenceChunk{}, errors.New("evidence offset exceeds content length")
	}
	end := offset + int64(limit)
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return EvidenceChunk{Availability: availability, TotalLength: int64(len(data)), Digest: digest.String,
		Data: append([]byte(nil), data[offset:end]...), NextOffset: end, End: end == int64(len(data))}, nil
}
