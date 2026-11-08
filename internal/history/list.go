package history

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrStaleCursor = errors.New("history list cursor is stale or does not match filters")

type ListQuery struct {
	ProjectID, GraphID         string
	StorageState               StorageState
	StartedFrom, StartedBefore *time.Time
	Limit                      int
	Cursor                     string
}

type RunSummary struct {
	RunID, ProjectID, GraphID string
	StartedAt                 time.Time
	EndedAt                   *time.Time
	StorageState              StorageState
	RuntimeState              string
	Pinned                    bool
	Revision                  uint64
	Completeness              SafeJSON
}

type ListPage struct {
	Runs       []RunSummary
	NextCursor string
	Generation uint64
}

type listCursor struct {
	Generation uint64 `json:"generation"`
	Filters    string `json:"filters"`
	StartedAt  string `json:"started_at"`
	RunID      string `json:"run_id"`
}

func (q ListQuery) filterDigest() string {
	from, before := "", ""
	if q.StartedFrom != nil {
		from = formatHistoryTime(*q.StartedFrom)
	}
	if q.StartedBefore != nil {
		before = formatHistoryTime(*q.StartedBefore)
	}
	data, _ := json.Marshal([]string{q.ProjectID, q.GraphID, string(q.StorageState), from, before})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func decodeListCursor(value string) (listCursor, error) {
	if len(value) > 4096 {
		return listCursor{}, ErrStaleCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, ErrStaleCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cursor listCursor
	if err := decoder.Decode(&cursor); err != nil {
		return listCursor{}, ErrStaleCursor
	}
	if cursor.Filters == "" || cursor.StartedAt == "" || cursor.RunID == "" || decoder.More() {
		return listCursor{}, ErrStaleCursor
	}
	return cursor, nil
}

func (s *Store) List(ctx context.Context, query ListQuery) (ListPage, error) {
	if query.Limit < 1 || query.Limit > 256 {
		return ListPage{}, errors.New("history list limit must be 1–256")
	}
	if query.StorageState != "" && query.StorageState != Active && query.StorageState != Complete && query.StorageState != Interrupted {
		return ListPage{}, errors.New("invalid history storage state filter")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ListPage{}, fmt.Errorf("%w: begin history list: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	var generation int64
	if err := tx.QueryRowContext(ctx, "SELECT list_generation FROM store_meta WHERE id=1").Scan(&generation); err != nil {
		return ListPage{}, fmt.Errorf("%w: read list generation: %v", ErrUnavailable, err)
	}
	page := ListPage{Runs: []RunSummary{}, Generation: uint64(generation)}
	pageBytes := 4096
	var cursor listCursor
	if query.Cursor != "" {
		cursor, err = decodeListCursor(query.Cursor)
		if err != nil || cursor.Generation != page.Generation || cursor.Filters != query.filterDigest() {
			return ListPage{}, ErrStaleCursor
		}
	}
	var sqlText strings.Builder
	sqlText.WriteString("SELECT run_id,project_id,graph_id,started_at,ended_at,storage_state,runtime_state,pinned,revision,safe_result_json IS NOT NULL,result_digest IS NOT NULL,completeness_json,completeness_digest FROM runs WHERE 1=1")
	args := make([]any, 0, 12)
	if query.ProjectID != "" {
		sqlText.WriteString(" AND project_id=?")
		args = append(args, query.ProjectID)
	}
	if query.GraphID != "" {
		sqlText.WriteString(" AND graph_id=?")
		args = append(args, query.GraphID)
	}
	if query.StorageState != "" {
		sqlText.WriteString(" AND storage_state=?")
		args = append(args, query.StorageState)
	}
	if query.StartedFrom != nil {
		sqlText.WriteString(" AND started_at>=?")
		args = append(args, formatHistoryTime(*query.StartedFrom))
	}
	if query.StartedBefore != nil {
		sqlText.WriteString(" AND started_at<?")
		args = append(args, formatHistoryTime(*query.StartedBefore))
	}
	if query.Cursor != "" {
		sqlText.WriteString(" AND (started_at<? OR (started_at=? AND run_id<?))")
		args = append(args, cursor.StartedAt, cursor.StartedAt, cursor.RunID)
	}
	sqlText.WriteString(" ORDER BY started_at DESC,run_id DESC LIMIT ?")
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, sqlText.String(), args...)
	if err != nil {
		return ListPage{}, fmt.Errorf("%w: query history list: %v", ErrUnavailable, err)
	}
	for rows.Next() {
		var item RunSummary
		var started string
		var ended, runtimeState sql.NullString
		var pinned int
		var revision int64
		var hasResult, hasResultDigest int
		var completenessData []byte
		var completenessDigest string
		if err := rows.Scan(&item.RunID, &item.ProjectID, &item.GraphID, &started, &ended, &item.StorageState, &runtimeState, &pinned, &revision, &hasResult, &hasResultDigest, &completenessData, &completenessDigest); err != nil {
			rows.Close()
			return ListPage{}, fmt.Errorf("%w: scan history list: %v", ErrUnavailable, err)
		}
		item.Completeness, err = storedProjection(completenessData, completenessDigest)
		if err != nil {
			rows.Close()
			return ListPage{}, err
		}
		if item.StorageState == Complete && (!ended.Valid || !runtimeState.Valid || runtimeState.String == "" || hasResult == 0 || hasResultDigest == 0) {
			rows.Close()
			return ListPage{}, fmt.Errorf("%w: incomplete terminal run record", ErrUnavailable)
		}
		item.StartedAt, err = time.Parse(time.RFC3339Nano, started)
		if err != nil {
			rows.Close()
			return ListPage{}, fmt.Errorf("%w: invalid stored start time", ErrUnavailable)
		}
		if ended.Valid {
			end, e := time.Parse(time.RFC3339Nano, ended.String)
			if e != nil {
				rows.Close()
				return ListPage{}, fmt.Errorf("%w: invalid stored end time", ErrUnavailable)
			}
			item.EndedAt = &end
		}
		if runtimeState.Valid {
			item.RuntimeState = runtimeState.String
		}
		if !validHistoryID(item.RunID) || !validHistoryID(item.ProjectID) || !validHistoryID(item.GraphID) || len(item.RuntimeState) > maxHistoryStatusBytes {
			rows.Close()
			return ListPage{}, ErrPageTooLarge
		}
		item.Pinned = pinned != 0
		item.Revision = uint64(revision)
		if len(page.Runs) < query.Limit {
			encoded, err := json.Marshal(item)
			if err != nil || pageBytes+len(encoded)+len(completenessData)+len(completenessDigest)+1 > maxRunResponseBytes {
				rows.Close()
				return ListPage{}, ErrPageTooLarge
			}
			pageBytes += len(encoded) + len(completenessData) + len(completenessDigest) + 1
		}
		page.Runs = append(page.Runs, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ListPage{}, fmt.Errorf("%w: read history list: %v", ErrUnavailable, err)
	}
	if err := rows.Close(); err != nil {
		return ListPage{}, fmt.Errorf("%w: close history list: %v", ErrUnavailable, err)
	}
	if len(page.Runs) > query.Limit {
		page.Runs = page.Runs[:query.Limit]
		last := page.Runs[len(page.Runs)-1]
		data, _ := json.Marshal(listCursor{Generation: page.Generation, Filters: query.filterDigest(), StartedAt: formatHistoryTime(last.StartedAt), RunID: last.RunID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	if err := tx.Commit(); err != nil {
		return ListPage{}, fmt.Errorf("%w: finish history list: %v", ErrUnavailable, err)
	}
	return page, nil
}
