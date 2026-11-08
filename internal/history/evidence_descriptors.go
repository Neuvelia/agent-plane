package history

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// EvidenceDescriptor contains no managed path. Redacted means available bytes
// are a sanitized, partial observation rather than a byte-exact file image.
type EvidenceDescriptor struct {
	EvidenceID     string    `json:"evidence_id"`
	RunID          string    `json:"run_id"`
	AttemptID      string    `json:"attempt_id,omitempty"`
	NodeRunID      string    `json:"node_run_id,omitempty"`
	Kind           string    `json:"kind"`
	SourcePath     string    `json:"source_path,omitempty"`
	CaptureGroup   string    `json:"capture_group,omitempty"`
	Availability   string    `json:"availability"`
	OmissionReason string    `json:"omission_reason,omitempty"`
	ByteLength     *int64    `json:"byte_length,omitempty"`
	Digest         string    `json:"digest,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	Redacted       bool      `json:"redacted"`
}

type EvidenceDescriptorPage struct {
	Descriptors []EvidenceDescriptor `json:"descriptors"`
	NextCursor  string               `json:"next_cursor,omitempty"`
	Generation  uint64               `json:"generation"`
	Complete    bool                 `json:"complete"`
}

type evidenceCursor struct {
	RunID      string `json:"run_id"`
	AttemptID  string `json:"attempt_id"`
	Generation uint64 `json:"generation"`
	CreatedAt  string `json:"created_at"`
	EvidenceID string `json:"evidence_id"`
}

func parseEvidenceCursor(raw, runID, attemptID string, generation uint64) (evidenceCursor, error) {
	if raw == "" {
		return evidenceCursor{}, nil
	}
	if len(raw) > 4096 {
		return evidenceCursor{}, ErrStaleCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return evidenceCursor{}, ErrStaleCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cursor evidenceCursor
	if err = decoder.Decode(&cursor); err != nil || cursor.RunID != runID || cursor.AttemptID != attemptID || cursor.Generation != generation || cursor.CreatedAt == "" || cursor.EvidenceID == "" {
		return evidenceCursor{}, ErrStaleCursor
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return evidenceCursor{}, ErrStaleCursor
	}
	if _, err = time.Parse(historyTimeLayout, cursor.CreatedAt); err != nil {
		return evidenceCursor{}, ErrStaleCursor
	}
	return cursor, nil
}

func encodeEvidenceCursor(cursor evidenceCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

// EvidenceDescriptors uses one snapshot transaction. A capture increments
// the persisted generation, invalidating a cursor rather than silently
// omitting newly inserted records whose timestamp sorts before it.
func (s *Store) EvidenceDescriptors(ctx context.Context, runID, attemptID, cursor string, limit int) (EvidenceDescriptorPage, error) {
	if s == nil || !validHistoryID(runID) || attemptID != "" && !validHistoryID(attemptID) || limit < 1 || limit > 256 || len(cursor) > 4096 {
		return EvidenceDescriptorPage{}, errors.New("invalid evidence descriptor page request")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return EvidenceDescriptorPage{}, fmt.Errorf("%w: begin evidence descriptor page: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	var generation int64
	if err = tx.QueryRowContext(ctx, "SELECT list_generation FROM store_meta WHERE id=1").Scan(&generation); err != nil || generation < 0 {
		return EvidenceDescriptorPage{}, fmt.Errorf("%w: read evidence generation", ErrUnavailable)
	}
	var state StorageState
	if err = tx.QueryRowContext(ctx, "SELECT storage_state FROM runs WHERE run_id=?", runID).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return EvidenceDescriptorPage{}, ErrNotFound
	} else if err != nil {
		return EvidenceDescriptorPage{}, fmt.Errorf("%w: read evidence run: %v", ErrUnavailable, err)
	}
	parsed, err := parseEvidenceCursor(cursor, runID, attemptID, uint64(generation))
	if err != nil {
		return EvidenceDescriptorPage{}, err
	}
	query := `SELECT evidence_id,run_id,attempt_id,node_run_id,kind,availability,omission_reason,byte_length,digest,created_at,redacted,source_path,capture_group FROM managed_evidence WHERE run_id=?`
	args := []any{runID}
	if attemptID != "" {
		query += " AND attempt_id=?"
		args = append(args, attemptID)
	}
	if cursor != "" {
		query += " AND (created_at>? OR (created_at=? AND evidence_id>?))"
		args = append(args, parsed.CreatedAt, parsed.CreatedAt, parsed.EvidenceID)
	}
	query += " ORDER BY created_at,evidence_id LIMIT ?"
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return EvidenceDescriptorPage{}, fmt.Errorf("%w: query evidence descriptors: %v", ErrUnavailable, err)
	}
	page := EvidenceDescriptorPage{Descriptors: make([]EvidenceDescriptor, 0, limit), Generation: uint64(generation)}
	for rows.Next() {
		if len(page.Descriptors) == limit {
			last := page.Descriptors[len(page.Descriptors)-1]
			page.NextCursor = encodeEvidenceCursor(evidenceCursor{RunID: runID, AttemptID: attemptID, Generation: uint64(generation), CreatedAt: formatHistoryTime(last.CreatedAt), EvidenceID: last.EvidenceID})
			break
		}
		var item EvidenceDescriptor
		var attempt, node, reason, digest sql.NullString
		var sourcePath, captureGroup sql.NullString
		var length sql.NullInt64
		var created string
		var redacted int
		if err = rows.Scan(&item.EvidenceID, &item.RunID, &attempt, &node, &item.Kind, &item.Availability, &reason, &length, &digest, &created, &redacted, &sourcePath, &captureGroup); err != nil {
			rows.Close()
			return EvidenceDescriptorPage{}, fmt.Errorf("%w: scan evidence descriptor: %v", ErrUnavailable, err)
		}
		if attempt.Valid {
			item.AttemptID = attempt.String
		}
		if node.Valid {
			item.NodeRunID = node.String
		}
		if sourcePath.Valid {
			item.SourcePath = sourcePath.String
		}
		if captureGroup.Valid {
			item.CaptureGroup = captureGroup.String
		}
		if reason.Valid {
			item.OmissionReason = reason.String
		}
		if digest.Valid {
			item.Digest = digest.String
		}
		if length.Valid {
			value := length.Int64
			item.ByteLength = &value
		}
		item.Redacted = redacted == 1
		item.CreatedAt, err = time.Parse(historyTimeLayout, created)
		if err != nil || !validHistoryID(item.EvidenceID) || item.RunID != runID ||
			(attempt.Valid && !validHistoryID(item.AttemptID)) ||
			(!attempt.Valid && !validHistoryID(item.NodeRunID)) ||
			(item.NodeRunID != "" && !validHistoryID(item.NodeRunID)) || item.Kind == "" || len(item.Kind) > 64 || redacted < 0 || redacted > 1 || !validEvidenceSourcePath(item.SourcePath) || len(item.CaptureGroup) > 256 {
			rows.Close()
			return EvidenceDescriptorPage{}, fmt.Errorf("%w: invalid evidence descriptor", ErrUnavailable)
		}
		if item.Availability == "available" {
			if item.OmissionReason != "" || item.ByteLength == nil || *item.ByteLength < 0 || *item.ByteLength > maxEvidenceFileBytes || len(item.Digest) != 64 {
				rows.Close()
				return EvidenceDescriptorPage{}, fmt.Errorf("%w: invalid available evidence descriptor", ErrUnavailable)
			}
			if _, err = hex.DecodeString(item.Digest); err != nil {
				rows.Close()
				return EvidenceDescriptorPage{}, fmt.Errorf("%w: invalid evidence digest", ErrUnavailable)
			}
		} else if item.Availability == "unavailable" {
			if !validEvidenceOmissionReason(item.OmissionReason) || item.ByteLength != nil || item.Digest != "" || item.Redacted {
				rows.Close()
				return EvidenceDescriptorPage{}, fmt.Errorf("%w: invalid unavailable evidence descriptor", ErrUnavailable)
			}
		} else {
			rows.Close()
			return EvidenceDescriptorPage{}, fmt.Errorf("%w: invalid evidence availability", ErrUnavailable)
		}
		page.Descriptors = append(page.Descriptors, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return EvidenceDescriptorPage{}, fmt.Errorf("%w: iterate evidence descriptors: %v", ErrUnavailable, err)
	}
	rows.Close()
	var incomplete int
	completenessQuery := "SELECT EXISTS(SELECT 1 FROM managed_evidence WHERE run_id=? AND (availability='unavailable' OR redacted=1)"
	completenessArgs := []any{runID}
	if attemptID != "" {
		completenessQuery += " AND attempt_id=?"
		completenessArgs = append(completenessArgs, attemptID)
	}
	completenessQuery += ")"
	if err = tx.QueryRowContext(ctx, completenessQuery, completenessArgs...).Scan(&incomplete); err != nil {
		return EvidenceDescriptorPage{}, fmt.Errorf("%w: read evidence completeness: %v", ErrUnavailable, err)
	}
	page.Complete = state == Complete && incomplete == 0
	if err = tx.Commit(); err != nil {
		return EvidenceDescriptorPage{}, fmt.Errorf("%w: finish evidence descriptor page: %v", ErrUnavailable, err)
	}
	// A descriptor is never trusted merely because its database row contains a
	// plausible digest; validate the full immutable managed file before use.
	for _, item := range page.Descriptors {
		if item.Availability == "available" {
			if _, err = s.Evidence(ctx, item.EvidenceID, 0, 1); err != nil {
				return EvidenceDescriptorPage{}, err
			}
		}
	}
	return page, nil
}
