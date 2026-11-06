package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
)

const pruneBatchSize = 128

func (s *Store) Pin(ctx context.Context, runID string, expectedRevision uint64, pinned bool) (uint64, error) {
	if runID == "" || expectedRevision > math.MaxInt64 {
		return 0, errors.New("invalid history pin request")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: begin pin: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	var currentRevision int64
	var currentPinned int
	if err := tx.QueryRowContext(ctx, "SELECT revision,pinned FROM runs WHERE run_id=?", runID).Scan(&currentRevision, &currentPinned); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	} else if err != nil {
		return 0, fmt.Errorf("%w: read pin target: %v", ErrUnavailable, err)
	}
	if currentRevision < 0 || uint64(currentRevision) != expectedRevision || currentRevision == math.MaxInt64 {
		return 0, ErrConflict
	}
	if (currentPinned != 0) == pinned {
		return uint64(currentRevision), tx.Commit()
	}
	newPinned := 0
	if pinned {
		newPinned = 1
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET pinned=?,revision=revision+1 WHERE run_id=?", newPinned, runID); err != nil {
		return 0, fmt.Errorf("%w: update pin: %v", ErrUnavailable, err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE store_meta SET list_generation=list_generation+1 WHERE id=1"); err != nil {
		return 0, fmt.Errorf("%w: advance history generation: %v", ErrUnavailable, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("%w: commit pin: %v", ErrUnavailable, err)
	}
	return uint64(currentRevision + 1), nil
}

type PruneOptions struct {
	Now           time.Time
	RetentionDays int
	KeepAll       bool
}

type PruneResult struct {
	RemovedRuns    int
	RemovedFiles   int
	SkippedActive  int
	SkippedPinned  int
	SkippedPending int
}

func (s *Store) Prune(ctx context.Context, options PruneOptions) (PruneResult, error) {
	if options.Now.IsZero() || (options.KeepAll && options.RetentionDays != 0) || (!options.KeepAll && options.RetentionDays <= 0) {
		return PruneResult{}, errors.New("invalid history retention request")
	}
	if options.KeepAll {
		return PruneResult{}, nil
	}
	cutoff := formatHistoryTime(options.Now.UTC().AddDate(0, 0, -options.RetentionDays))
	result := PruneResult{}
	var evidenceRoot *os.Root
	defer func() {
		if evidenceRoot != nil {
			_ = evidenceRoot.Close()
		}
	}()
	lastID := ""
	var batchErr error
	for {
		batch, nextID, done, err := s.pruneBatch(ctx, cutoff, lastID, &evidenceRoot)
		if err != nil {
			batchErr = err
			break
		}
		result.RemovedRuns += batch.RemovedRuns
		result.SkippedActive += batch.SkippedActive
		result.SkippedPinned += batch.SkippedPinned
		result.SkippedPending += batch.SkippedPending
		if done {
			break
		}
		lastID = nextID
	}
	// A prior invocation may have committed a batch before cancellation. Sweep
	// even when this retry finds no runs, so durable orphans are reclaimed.
	removed, err := s.sweepEvidenceCount(ctx)
	result.RemovedFiles = removed
	batchErr = errors.Join(batchErr, err)
	return result, batchErr
}

func (s *Store) pruneBatch(ctx context.Context, cutoff, after string, evidenceRoot **os.Root) (PruneResult, string, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PruneResult{}, "", false, fmt.Errorf("%w: begin prune: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT run_id,storage_state,pinned,pending_approval FROM runs WHERE COALESCE(ended_at,started_at)<? AND run_id>? ORDER BY run_id LIMIT ?", cutoff, after, pruneBatchSize)
	if err != nil {
		return PruneResult{}, "", false, fmt.Errorf("%w: find expired runs: %v", ErrUnavailable, err)
	}
	result := PruneResult{}
	eligible := make([]string, 0, pruneBatchSize)
	seen := 0
	nextID := after
	for rows.Next() {
		var id string
		var state StorageState
		var pinned, pending int
		if err := rows.Scan(&id, &state, &pinned, &pending); err != nil {
			rows.Close()
			return PruneResult{}, "", false, fmt.Errorf("%w: scan expired run: %v", ErrUnavailable, err)
		}
		seen++
		nextID = id
		switch {
		case state == Active:
			result.SkippedActive++
		case pinned != 0:
			result.SkippedPinned++
		case pending != 0:
			result.SkippedPending++
		case state == Complete || state == Interrupted:
			eligible = append(eligible, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return PruneResult{}, "", false, fmt.Errorf("%w: scan expired runs: %v", ErrUnavailable, err)
	}
	if err := rows.Close(); err != nil {
		return PruneResult{}, "", false, fmt.Errorf("%w: close expired runs: %v", ErrUnavailable, err)
	}
	// Stream references for each bounded run batch. Validate before cascading
	// deletes, without retaining file metadata for the entire retention set.
	for _, id := range eligible {
		files, err := tx.QueryContext(ctx, "SELECT relative_path FROM managed_evidence WHERE run_id=? AND availability='available'", id)
		if err != nil {
			return PruneResult{}, "", false, fmt.Errorf("%w: read managed references: %v", ErrUnavailable, err)
		}
		for files.Next() {
			var path string
			if err := files.Scan(&path); err != nil {
				files.Close()
				return PruneResult{}, "", false, fmt.Errorf("%w: scan managed reference: %v", ErrUnavailable, err)
			}
			name, valid := validEvidencePath(path)
			if !valid {
				files.Close()
				return PruneResult{}, "", false, fmt.Errorf("%w: invalid managed evidence path", ErrUnavailable)
			}
			if *evidenceRoot == nil {
				*evidenceRoot, err = s.evidenceRoot(false)
				if err != nil {
					files.Close()
					return PruneResult{}, "", false, err
				}
			}
			info, err := (*evidenceRoot).Lstat(name)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				files.Close()
				return PruneResult{}, "", false, fmt.Errorf("%w: unsafe managed evidence file", ErrUnavailable)
			}
		}
		if err := files.Err(); err != nil {
			files.Close()
			return PruneResult{}, "", false, fmt.Errorf("%w: read managed references: %v", ErrUnavailable, err)
		}
		if err := files.Close(); err != nil {
			return PruneResult{}, "", false, fmt.Errorf("%w: close managed references: %v", ErrUnavailable, err)
		}
	}
	for _, id := range eligible {
		if _, err := tx.ExecContext(ctx, "DELETE FROM runs WHERE run_id=?", id); err != nil {
			return PruneResult{}, "", false, fmt.Errorf("%w: remove expired run: %v", ErrUnavailable, err)
		}
		result.RemovedRuns++
	}
	if result.RemovedRuns > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE store_meta SET list_generation=list_generation+1 WHERE id=1"); err != nil {
			return PruneResult{}, "", false, fmt.Errorf("%w: advance history generation: %v", ErrUnavailable, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return PruneResult{}, "", false, fmt.Errorf("%w: commit prune: %v", ErrUnavailable, err)
	}
	return result, nextID, seen < pruneBatchSize, nil
}
