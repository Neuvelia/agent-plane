package history

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type OutcomePage struct {
	Nodes             []NodeOutcome
	Attempts          []AttemptOutcome
	NextNodeCursor    string
	NextAttemptCursor string
}

type outcomeCursor struct {
	RunID string `json:"run_id"`
	Kind  string `json:"kind"`
	After string `json:"after"`
}

func parseOutcomeCursor(value, runID, kind string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 4096 {
		return "", ErrStaleCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", ErrStaleCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cursor outcomeCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.RunID != runID || cursor.Kind != kind || cursor.After == "" {
		return "", ErrStaleCursor
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", ErrStaleCursor
	}
	return cursor.After, nil
}

func makeOutcomeCursor(runID, kind, after string) string {
	data, _ := json.Marshal(outcomeCursor{RunID: runID, Kind: kind, After: after})
	return base64.RawURLEncoding.EncodeToString(data)
}

func encodedOutcomeBytes(id, parent, status string, projection SafeJSON) (int, error) {
	encoded, err := json.Marshal(struct {
		ID         string          `json:"id"`
		Parent     string          `json:"parent"`
		Status     string          `json:"status"`
		Projection json.RawMessage `json:"projection"`
		Digest     string          `json:"digest"`
	}{id, parent, status, projection.data, projection.digest})
	return len(encoded), err
}

// Outcomes pages the immutable indexes derived during final ingestion. The
// cursors bind both the run and outcome kind; a caller cannot reuse one as the
// other or for another run.
func (s *Store) Outcomes(ctx context.Context, runID string, limit int, nodeCursor, attemptCursor string) (OutcomePage, error) {
	if runID == "" || limit < 1 || limit > 256 {
		return OutcomePage{}, errors.New("invalid outcome page request")
	}
	nodeAfter, err := parseOutcomeCursor(nodeCursor, runID, "node")
	if err != nil {
		return OutcomePage{}, err
	}
	attemptAfter, err := parseOutcomeCursor(attemptCursor, runID, "attempt")
	if err != nil {
		return OutcomePage{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return OutcomePage{}, fmt.Errorf("%w: begin outcomes page: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	var state StorageState
	if err := tx.QueryRowContext(ctx, "SELECT storage_state FROM runs WHERE run_id=?", runID).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return OutcomePage{}, ErrNotFound
	} else if err != nil {
		return OutcomePage{}, fmt.Errorf("%w: read outcomes target: %v", ErrUnavailable, err)
	}
	page := OutcomePage{Nodes: []NodeOutcome{}, Attempts: []AttemptOutcome{}}
	pageBytes := 1024
	if nodeCursor != "" || attemptCursor == "" {
		if err := readNodeOutcomes(ctx, tx, runID, nodeAfter, limit, &page, &pageBytes); err != nil {
			return OutcomePage{}, err
		}
	}
	if attemptCursor != "" || nodeCursor == "" {
		if err := readAttemptOutcomes(ctx, tx, runID, attemptAfter, limit, &page, &pageBytes); err != nil {
			return OutcomePage{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return OutcomePage{}, fmt.Errorf("%w: finish outcomes page: %v", ErrUnavailable, err)
	}
	return page, nil
}

func readNodeOutcomes(ctx context.Context, tx *sql.Tx, runID, after string, limit int, page *OutcomePage, pageBytes *int) error {
	rows, err := tx.QueryContext(ctx, "SELECT node_run_id,node_id,status,safe_json,digest FROM node_outcomes WHERE run_id=? AND node_run_id>? ORDER BY node_run_id LIMIT ?", runID, after, limit+1)
	if err != nil {
		return fmt.Errorf("%w: read node outcomes: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		if len(page.Nodes) == limit {
			page.NextNodeCursor = makeOutcomeCursor(runID, "node", page.Nodes[len(page.Nodes)-1].NodeRunID)
			break
		}
		var item NodeOutcome
		var data []byte
		var digest string
		if err := rows.Scan(&item.NodeRunID, &item.NodeID, &item.Status, &data, &digest); err != nil {
			return fmt.Errorf("%w: scan node outcome: %v", ErrUnavailable, err)
		}
		item.Projection, err = storedProjection(data, digest)
		if err != nil {
			return err
		}
		encoded, err := encodedOutcomeBytes(item.NodeRunID, item.NodeID, item.Status, item.Projection)
		if err != nil || *pageBytes+encoded+1 > maxEventPageBytes {
			return ErrPageTooLarge
		}
		*pageBytes += encoded + 1
		page.Nodes = append(page.Nodes, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: iterate node outcomes: %v", ErrUnavailable, err)
	}
	return nil
}

func readAttemptOutcomes(ctx context.Context, tx *sql.Tx, runID, after string, limit int, page *OutcomePage, pageBytes *int) error {
	rows, err := tx.QueryContext(ctx, "SELECT attempt_id,node_run_id,status,safe_json,digest FROM attempt_outcomes WHERE run_id=? AND attempt_id>? ORDER BY attempt_id LIMIT ?", runID, after, limit+1)
	if err != nil {
		return fmt.Errorf("%w: read attempt outcomes: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		if len(page.Attempts) == limit {
			page.NextAttemptCursor = makeOutcomeCursor(runID, "attempt", page.Attempts[len(page.Attempts)-1].AttemptID)
			break
		}
		var item AttemptOutcome
		var data []byte
		var digest string
		if err := rows.Scan(&item.AttemptID, &item.NodeRunID, &item.Status, &data, &digest); err != nil {
			return fmt.Errorf("%w: scan attempt outcome: %v", ErrUnavailable, err)
		}
		item.Projection, err = storedProjection(data, digest)
		if err != nil {
			return err
		}
		encoded, err := encodedOutcomeBytes(item.AttemptID, item.NodeRunID, item.Status, item.Projection)
		if err != nil || *pageBytes+encoded+1 > maxEventPageBytes {
			return ErrPageTooLarge
		}
		*pageBytes += encoded + 1
		page.Attempts = append(page.Attempts, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: iterate attempt outcomes: %v", ErrUnavailable, err)
	}
	return nil
}
