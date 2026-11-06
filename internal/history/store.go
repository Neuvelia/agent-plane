package history

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1
const maxProjectionBytes = 8 << 20
const maxIndexedProjectionBytes = 3 << 20
const maxEventPageBytes = 7 << 20
const maxRunResponseBytes = 6 << 20
const maxHistoryIDBytes = 256
const maxHistoryStatusBytes = 64
const historyTimeLayout = "2006-01-02T15:04:05.000000000Z"

func formatHistoryTime(value time.Time) string {
	return value.UTC().Format(historyTimeLayout)
}

func validHistoryID(value string) bool { return value != "" && len(value) <= maxHistoryIDBytes }

func validEventOmissionReason(reason string) bool {
	return reason == "sensitive_identity" || reason == "not_retained"
}

func runProjectionFits(graph, launch, result, completeness SafeJSON, metadataBytes int) bool {
	return len(graph.data)+len(launch.data)+len(result.data)+len(completeness.data)+metadataBytes+4096 <= maxRunResponseBytes
}

var (
	ErrUnavailable  = errors.New("history store unavailable")
	ErrNewerSchema  = errors.New("history schema is newer than this application")
	ErrConflict     = errors.New("history content conflict")
	ErrNotFound     = errors.New("history run not found")
	ErrPageTooLarge = errors.New("history page exceeds protocol size; request a smaller limit")
)

type StorageState string

const (
	Active      StorageState = "active"
	Complete    StorageState = "complete"
	Interrupted StorageState = "interrupted"
)

// SafeJSON is the only projection form accepted by this store. The caller must
// sanitize typed payloads before construction; known values are checked again
// here so they cannot reach SQLite through a mistaken call site.
type SafeJSON struct {
	data   []byte
	digest string
}

func ParseSafeJSON(data []byte, forbidden []string) (SafeJSON, error) {
	if len(data) == 0 || len(data) > maxProjectionBytes || !json.Valid(data) {
		return SafeJSON{}, errors.New("safe history projection is invalid or too large")
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	keys.UseNumber()
	if err := rejectDuplicateJSONKeys(keys); err != nil {
		return SafeJSON{}, fmt.Errorf("invalid safe history projection: %w", err)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return SafeJSON{}, fmt.Errorf("decode safe history projection: %w", err)
	}
	for _, value := range forbidden {
		if value != "" && containsString(decoded, value) {
			return SafeJSON{}, errors.New("safe history projection contains a configured private value")
		}
	}
	canonical, err := json.Marshal(decoded)
	if err != nil || len(canonical) > maxProjectionBytes {
		return SafeJSON{}, errors.New("safe history projection cannot be encoded within limit")
	}
	sum := sha256.Sum256(canonical)
	return SafeJSON{data: canonical, digest: hex.EncodeToString(sum[:])}, nil
}

func rejectDuplicateJSONKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key := keyToken.(string)
			if seen[key] {
				return errors.New("duplicate JSON object key")
			}
			seen[key] = true
			if err := rejectDuplicateJSONKeys(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := rejectDuplicateJSONKeys(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func containsString(value any, forbidden string) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, forbidden)
	case []any:
		for _, item := range v {
			if containsString(item, forbidden) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			if strings.Contains(key, forbidden) || containsString(item, forbidden) {
				return true
			}
		}
	}
	return false
}

func (p SafeJSON) Bytes() []byte { return append([]byte(nil), p.data...) }
func (p SafeJSON) valid() bool   { return len(p.data) != 0 && p.digest != "" }

func storedProjection(data []byte, digest string) (SafeJSON, error) {
	if len(data) == 0 || len(data) > maxProjectionBytes || !json.Valid(data) {
		return SafeJSON{}, fmt.Errorf("%w: invalid stored history projection", ErrUnavailable)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return SafeJSON{}, fmt.Errorf("%w: stored history projection digest mismatch", ErrUnavailable)
	}
	return SafeJSON{data: data, digest: digest}, nil
}

type LaunchRecord struct {
	RunID, ProjectID, WorkspaceID, DocumentID, GraphID, GraphHash string
	StartedAt                                                     time.Time
	Graph, Launch, Completeness                                   SafeJSON
}

type Run struct {
	RunID, ProjectID, WorkspaceID, DocumentID, GraphID, GraphHash string
	StartedAt                                                     time.Time
	EndedAt                                                       *time.Time
	StorageState                                                  StorageState
	RuntimeState                                                  string
	Pinned                                                        bool
	Revision                                                      uint64
	Graph, Launch, Completeness                                   SafeJSON
	Result                                                        *SafeJSON
}

type Event struct {
	Sequence   uint64
	Projection SafeJSON
}

type EventOmission struct {
	Sequence uint64
	Reason   string
}

type OmissionRange struct {
	From, To uint64
	Reason   string
}

type EventPage struct {
	Events       []Event
	Omissions    []OmissionRange
	Watermark    uint64
	NextSequence uint64
	Finished     bool
}

func encodedEventBytes(sequence uint64, projection SafeJSON) (int, error) {
	encoded, err := json.Marshal(struct {
		Sequence   uint64          `json:"sequence"`
		Projection json.RawMessage `json:"projection"`
		Digest     string          `json:"digest"`
	}{sequence, projection.data, projection.digest})
	return len(encoded), err
}

func encodedOmissionBytes(omission OmissionRange) (int, error) {
	encoded, err := json.Marshal(omission)
	return len(encoded), err
}

type Finalization struct {
	RunID                string
	EndedAt              time.Time
	RuntimeState         string
	Watermark            uint64
	Result, Completeness SafeJSON
	Events               []Event
	Omissions            []EventOmission
	Nodes                []NodeOutcome
	Attempts             []AttemptOutcome
}

type NodeOutcome struct {
	NodeRunID, NodeID, Status string
	Projection                SafeJSON
}

type AttemptOutcome struct {
	AttemptID, NodeRunID, Status string
	Projection                   SafeJSON
}

type Store struct {
	db  *sql.DB
	dir string
}

// Open expects the caller's already pinned private state directory. The
// separate registry lifetime lock remains the desktop service's authority.
func Open(ctx context.Context, dir string) (*Store, error) {
	if ctx == nil || !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%w: history directory must be absolute", ErrUnavailable)
	}
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("%w: native Windows history path protection is unverified", ErrUnavailable)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: history directory is not private", ErrUnavailable)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := checkStoreFile(filepath.Join(dir, "history.db") + suffix); err != nil {
			return nil, err
		}
	}
	path := filepath.Join(dir, "history.db")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_ = file.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("%w: create history database: %v", ErrUnavailable, err)
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("%w: open SQLite: %v", ErrUnavailable, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, dir: dir}
	if err := store.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := checkStoreFile(path + suffix); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := store.sweepEvidence(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func checkStoreFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%w: unsafe history database path", ErrUnavailable)
	}
	return nil
}

func (s *Store) initialize(ctx context.Context) error {
	// A newer database is read-only to this binary: journal_mode=WAL itself
	// mutates the database, so check the version before any write PRAGMA.
	var existingVersion int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&existingVersion); err != nil {
		return fmt.Errorf("%w: read schema version: %v", ErrUnavailable, err)
	}
	if existingVersion > schemaVersion {
		return fmt.Errorf("%w: found %d, supported %d", ErrNewerSchema, existingVersion, schemaVersion)
	}
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=2000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("%w: configure SQLite: %v", ErrUnavailable, err)
		}
	}
	var integrity string
	if err := s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		return fmt.Errorf("%w: SQLite integrity check failed", ErrUnavailable)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin migration: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("%w: read schema version: %v", ErrUnavailable, err)
	}
	if version > schemaVersion {
		return fmt.Errorf("%w: found %d, supported %d", ErrNewerSchema, version, schemaVersion)
	}
	if version == 0 {
		for _, statement := range migrationV1 {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("%w: migrate schema: %v", ErrUnavailable, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version=1"); err != nil {
			return fmt.Errorf("%w: set schema version: %v", ErrUnavailable, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit migration: %v", ErrUnavailable, err)
	}
	return s.interruptActive(ctx)
}

func (s *Store) interruptActive(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin interruption recovery: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE runs SET storage_state='interrupted', revision=revision+1 WHERE storage_state='active'")
	if err != nil {
		return fmt.Errorf("%w: recover active markers: %v", ErrUnavailable, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE store_meta SET list_generation=list_generation+1 WHERE id=1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Launch(ctx context.Context, record LaunchRecord) error {
	if !validHistoryID(record.RunID) || !validHistoryID(record.ProjectID) || !validHistoryID(record.WorkspaceID) || !validHistoryID(record.DocumentID) || !validHistoryID(record.GraphID) || !validHistoryID(record.GraphHash) || record.StartedAt.IsZero() || !record.Graph.valid() || !record.Launch.valid() || !record.Completeness.valid() {
		return errors.New("invalid history launch marker")
	}
	if !runProjectionFits(record.Graph, record.Launch, SafeJSON{}, record.Completeness,
		len(record.RunID)+len(record.ProjectID)+len(record.WorkspaceID)+len(record.DocumentID)+len(record.GraphID)+len(record.GraphHash)) {
		return ErrPageTooLarge
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin launch marker: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO runs(run_id,project_id,workspace_id,document_id,graph_id,graph_hash,started_at,storage_state,revision,pinned,pending_approval,safe_graph_json,graph_digest,safe_launch_json,launch_digest,completeness_json,completeness_digest)
	VALUES(?,?,?,?,?,?,?,'active',0,0,0,?,?,?,?,?,?)`, record.RunID, record.ProjectID, record.WorkspaceID, record.DocumentID, record.GraphID, record.GraphHash, formatHistoryTime(record.StartedAt), record.Graph.data, record.Graph.digest, record.Launch.data, record.Launch.digest, record.Completeness.data, record.Completeness.digest)
	if err != nil {
		return fmt.Errorf("%w: insert launch marker: %v", ErrUnavailable, err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE store_meta SET list_generation=list_generation+1 WHERE id=1"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit launch marker: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *Store) Finalize(ctx context.Context, final Finalization) error {
	if !validHistoryID(final.RunID) || final.EndedAt.IsZero() || final.RuntimeState == "" || len(final.RuntimeState) > maxHistoryStatusBytes || final.Watermark == 0 || final.Watermark > uint64(math.MaxInt64) || !final.Result.valid() || !final.Completeness.valid() {
		return errors.New("invalid history finalization")
	}
	seen := make(map[uint64]bool, len(final.Events))
	for _, event := range final.Events {
		if event.Sequence == 0 || !event.Projection.valid() || seen[event.Sequence] {
			return errors.New("invalid or repeated final event")
		}
		if len(event.Projection.data) > maxIndexedProjectionBytes {
			return ErrPageTooLarge
		}
		seen[event.Sequence] = true
	}
	for _, omission := range final.Omissions {
		if omission.Sequence == 0 || !validEventOmissionReason(omission.Reason) || seen[omission.Sequence] {
			return errors.New("invalid or repeated final event omission")
		}
		seen[omission.Sequence] = true
	}
	if uint64(len(seen)) != final.Watermark {
		return errors.New("final trace does not cover source watermark")
	}
	for sequence := range seen {
		if sequence > final.Watermark {
			return errors.New("final trace sequence exceeds source watermark")
		}
	}
	nodeIDs := make(map[string]bool, len(final.Nodes))
	for _, node := range final.Nodes {
		if !validHistoryID(node.NodeRunID) || !validHistoryID(node.NodeID) || node.Status == "" || len(node.Status) > maxHistoryStatusBytes || !node.Projection.valid() || nodeIDs[node.NodeRunID] {
			return errors.New("invalid or repeated final node outcome")
		}
		if len(node.Projection.data) > maxIndexedProjectionBytes {
			return ErrPageTooLarge
		}
		nodeIDs[node.NodeRunID] = true
	}
	attemptNodes := make(map[string]string, len(final.Attempts))
	for _, attempt := range final.Attempts {
		_, repeated := attemptNodes[attempt.AttemptID]
		if !validHistoryID(attempt.AttemptID) || !validHistoryID(attempt.NodeRunID) || attempt.Status == "" || len(attempt.Status) > maxHistoryStatusBytes || !attempt.Projection.valid() || repeated || !nodeIDs[attempt.NodeRunID] {
			return errors.New("invalid or repeated final attempt outcome")
		}
		if len(attempt.Projection.data) > maxIndexedProjectionBytes {
			return ErrPageTooLarge
		}
		attemptNodes[attempt.AttemptID] = attempt.NodeRunID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin finalization: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	var state StorageState
	var ended, runtimeState, resultDigest, completenessDigest sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT storage_state,ended_at,runtime_state,result_digest,completeness_digest FROM runs WHERE run_id=?", final.RunID).Scan(&state, &ended, &runtimeState, &resultDigest, &completenessDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("%w: read finalization target: %v", ErrUnavailable, err)
	}
	endedText := formatHistoryTime(final.EndedAt)
	if state == Complete {
		if !ended.Valid || ended.String != endedText || !runtimeState.Valid || runtimeState.String != final.RuntimeState || !resultDigest.Valid || resultDigest.String != final.Result.digest || !completenessDigest.Valid || completenessDigest.String != final.Completeness.digest {
			return ErrConflict
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM events WHERE run_id=?", final.RunID).Scan(&count); err != nil {
			return err
		}
		if count != len(final.Events) {
			return ErrConflict
		}
		for _, event := range final.Events {
			var digest string
			if err := tx.QueryRowContext(ctx, "SELECT digest FROM events WHERE run_id=? AND sequence=?", final.RunID, event.Sequence).Scan(&digest); err != nil || digest != event.Projection.digest {
				return ErrConflict
			}
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM event_omissions WHERE run_id=?", final.RunID).Scan(&count); err != nil {
			return err
		}
		if count != len(final.Omissions) {
			return ErrConflict
		}
		for _, omission := range final.Omissions {
			var reason string
			if err := tx.QueryRowContext(ctx, "SELECT reason FROM event_omissions WHERE run_id=? AND sequence=?", final.RunID, omission.Sequence).Scan(&reason); err != nil || reason != omission.Reason {
				return ErrConflict
			}
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM node_outcomes WHERE run_id=?", final.RunID).Scan(&count); err != nil {
			return err
		}
		if count != len(final.Nodes) {
			return ErrConflict
		}
		for _, node := range final.Nodes {
			var nodeID, status, digest string
			if err := tx.QueryRowContext(ctx, "SELECT node_id,status,digest FROM node_outcomes WHERE run_id=? AND node_run_id=?", final.RunID, node.NodeRunID).Scan(&nodeID, &status, &digest); err != nil || nodeID != node.NodeID || status != node.Status || digest != node.Projection.digest {
				return ErrConflict
			}
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM attempt_outcomes WHERE run_id=?", final.RunID).Scan(&count); err != nil {
			return err
		}
		if count != len(final.Attempts) {
			return ErrConflict
		}
		for _, attempt := range final.Attempts {
			var nodeRunID, status, digest string
			if err := tx.QueryRowContext(ctx, "SELECT node_run_id,status,digest FROM attempt_outcomes WHERE run_id=? AND attempt_id=?", final.RunID, attempt.AttemptID).Scan(&nodeRunID, &status, &digest); err != nil || nodeRunID != attempt.NodeRunID || status != attempt.Status || digest != attempt.Projection.digest {
				return ErrConflict
			}
		}
		return tx.Commit()
	}
	if state != Active {
		return ErrConflict
	}
	var graphData, launchData []byte
	var graphDigest, launchDigest string
	if err := tx.QueryRowContext(ctx, "SELECT safe_graph_json,graph_digest,safe_launch_json,launch_digest FROM runs WHERE run_id=?", final.RunID).Scan(&graphData, &graphDigest, &launchData, &launchDigest); err != nil {
		return fmt.Errorf("%w: read final snapshot budget: %v", ErrUnavailable, err)
	}
	graphProjection, err := storedProjection(graphData, graphDigest)
	if err != nil {
		return err
	}
	launchProjection, err := storedProjection(launchData, launchDigest)
	if err != nil {
		return err
	}
	if !runProjectionFits(graphProjection, launchProjection, final.Result, final.Completeness, len(final.RunID)+len(final.RuntimeState)+2048) {
		return ErrPageTooLarge
	}
	references, err := tx.QueryContext(ctx, "SELECT attempt_id,node_run_id FROM managed_evidence WHERE run_id=?", final.RunID)
	if err != nil {
		return fmt.Errorf("%w: read final evidence references: %v", ErrUnavailable, err)
	}
	for references.Next() {
		var attemptID string
		var nodeID sql.NullString
		if err := references.Scan(&attemptID, &nodeID); err != nil {
			references.Close()
			return fmt.Errorf("%w: scan final evidence reference: %v", ErrUnavailable, err)
		}
		boundNode, exists := attemptNodes[attemptID]
		if !exists || (nodeID.Valid && nodeID.String != boundNode) {
			references.Close()
			return ErrConflict
		}
	}
	if err := references.Err(); err != nil {
		references.Close()
		return fmt.Errorf("%w: read final evidence references: %v", ErrUnavailable, err)
	}
	if err := references.Close(); err != nil {
		return fmt.Errorf("%w: close final evidence references: %v", ErrUnavailable, err)
	}
	for _, event := range final.Events {
		if _, err := tx.ExecContext(ctx, "INSERT INTO events(run_id,sequence,event_json,digest) VALUES(?,?,?,?)", final.RunID, event.Sequence, event.Projection.data, event.Projection.digest); err != nil {
			return fmt.Errorf("%w: insert final event: %v", ErrUnavailable, err)
		}
	}
	for _, omission := range final.Omissions {
		if _, err := tx.ExecContext(ctx, "INSERT INTO event_omissions(run_id,sequence,reason) VALUES(?,?,?)", final.RunID, omission.Sequence, omission.Reason); err != nil {
			return fmt.Errorf("%w: insert final event omission: %v", ErrUnavailable, err)
		}
	}
	for _, node := range final.Nodes {
		if _, err := tx.ExecContext(ctx, "INSERT INTO node_outcomes(run_id,node_run_id,node_id,status,safe_json,digest) VALUES(?,?,?,?,?,?)", final.RunID, node.NodeRunID, node.NodeID, node.Status, node.Projection.data, node.Projection.digest); err != nil {
			return fmt.Errorf("%w: insert node outcome: %v", ErrUnavailable, err)
		}
	}
	for _, attempt := range final.Attempts {
		if _, err := tx.ExecContext(ctx, "INSERT INTO attempt_outcomes(run_id,attempt_id,node_run_id,status,safe_json,digest) VALUES(?,?,?,?,?,?)", final.RunID, attempt.AttemptID, attempt.NodeRunID, attempt.Status, attempt.Projection.data, attempt.Projection.digest); err != nil {
			return fmt.Errorf("%w: insert attempt outcome: %v", ErrUnavailable, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET storage_state='complete',ended_at=?,runtime_state=?,safe_result_json=?,result_digest=?,completeness_json=?,completeness_digest=?,revision=revision+1 WHERE run_id=?`, endedText, final.RuntimeState, final.Result.data, final.Result.digest, final.Completeness.data, final.Completeness.digest, final.RunID); err != nil {
		return fmt.Errorf("%w: update final run: %v", ErrUnavailable, err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE store_meta SET list_generation=list_generation+1 WHERE id=1"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit finalization: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, runID string) (Run, error) {
	var run Run
	var started, ended string
	var endedNull sql.NullString
	var runtimeState sql.NullString
	var pinned int
	var revision int64
	var graph, launch, complete, result []byte
	var graphDigest, launchDigest, completeDigest string
	var resultDigest sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT run_id,project_id,workspace_id,document_id,graph_id,graph_hash,started_at,ended_at,storage_state,runtime_state,pinned,revision,safe_graph_json,graph_digest,safe_launch_json,launch_digest,completeness_json,completeness_digest,safe_result_json,result_digest FROM runs WHERE run_id=?`, runID).Scan(&run.RunID, &run.ProjectID, &run.WorkspaceID, &run.DocumentID, &run.GraphID, &run.GraphHash, &started, &endedNull, &run.StorageState, &runtimeState, &pinned, &revision, &graph, &graphDigest, &launch, &launchDigest, &complete, &completeDigest, &result, &resultDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("%w: read run: %v", ErrUnavailable, err)
	}
	run.StartedAt, err = time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return Run{}, fmt.Errorf("%w: invalid start time", ErrUnavailable)
	}
	if endedNull.Valid {
		ended = endedNull.String
		parsed, e := time.Parse(time.RFC3339Nano, ended)
		if e != nil {
			return Run{}, fmt.Errorf("%w: invalid end time", ErrUnavailable)
		}
		run.EndedAt = &parsed
	}
	if runtimeState.Valid {
		run.RuntimeState = runtimeState.String
	}
	if run.StorageState == Complete && (!endedNull.Valid || !runtimeState.Valid || runtimeState.String == "" || !resultDigest.Valid || len(result) == 0) {
		return Run{}, fmt.Errorf("%w: incomplete terminal run record", ErrUnavailable)
	}
	run.Pinned = pinned != 0
	run.Revision = uint64(revision)
	if run.Graph, err = storedProjection(graph, graphDigest); err != nil {
		return Run{}, err
	}
	if run.Launch, err = storedProjection(launch, launchDigest); err != nil {
		return Run{}, err
	}
	if run.Completeness, err = storedProjection(complete, completeDigest); err != nil {
		return Run{}, err
	}
	if resultDigest.Valid {
		value, err := storedProjection(result, resultDigest.String)
		if err != nil {
			return Run{}, err
		}
		run.Result = &value
	}
	if !validHistoryID(run.RunID) || !validHistoryID(run.ProjectID) || !validHistoryID(run.WorkspaceID) || !validHistoryID(run.DocumentID) || !validHistoryID(run.GraphID) || !validHistoryID(run.GraphHash) || len(run.RuntimeState) > maxHistoryStatusBytes {
		return Run{}, ErrPageTooLarge
	}
	resultProjection := SafeJSON{}
	if run.Result != nil {
		resultProjection = *run.Result
	}
	if !runProjectionFits(run.Graph, run.Launch, resultProjection, run.Completeness,
		len(run.RunID)+len(run.ProjectID)+len(run.WorkspaceID)+len(run.DocumentID)+len(run.GraphID)+len(run.GraphHash)+len(run.RuntimeState)) {
		return Run{}, ErrPageTooLarge
	}
	return run, nil
}

func (s *Store) Events(ctx context.Context, runID string, after uint64, limit int) (EventPage, error) {
	if !validHistoryID(runID) || limit < 1 || limit > 256 {
		return EventPage{}, errors.New("event page limit must be 1–256")
	}
	if after > math.MaxInt64 {
		return EventPage{}, ErrStaleCursor
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return EventPage{}, fmt.Errorf("%w: begin event page: %v", ErrUnavailable, err)
	}
	defer tx.Rollback()
	page, err := s.eventsWithin(ctx, tx, runID, after, limit)
	if err != nil {
		return EventPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return EventPage{}, fmt.Errorf("%w: finish event page: %v", ErrUnavailable, err)
	}
	return page, nil
}

func (s *Store) eventsWithin(ctx context.Context, tx *sql.Tx, runID string, after uint64, limit int) (EventPage, error) {
	var state StorageState
	var ended, runtimeState sql.NullString
	var hasResult, hasResultDigest int
	if err := tx.QueryRowContext(ctx, "SELECT storage_state,ended_at,runtime_state,safe_result_json IS NOT NULL,result_digest IS NOT NULL FROM runs WHERE run_id=?", runID).Scan(&state, &ended, &runtimeState, &hasResult, &hasResultDigest); errors.Is(err, sql.ErrNoRows) {
		return EventPage{}, ErrNotFound
	} else if err != nil {
		return EventPage{}, fmt.Errorf("%w: read event page run: %v", ErrUnavailable, err)
	}
	if state == Complete && (!ended.Valid || !runtimeState.Valid || runtimeState.String == "" || hasResult == 0 || hasResultDigest == 0) {
		return EventPage{}, fmt.Errorf("%w: incomplete terminal run record", ErrUnavailable)
	}
	page := EventPage{NextSequence: after, Finished: state == Complete}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM (
	SELECT sequence FROM events WHERE run_id=? UNION ALL SELECT sequence FROM event_omissions WHERE run_id=?)`, runID, runID).Scan(&page.Watermark); err != nil {
		return EventPage{}, fmt.Errorf("%w: read event watermark: %v", ErrUnavailable, err)
	}
	if after > page.Watermark {
		return EventPage{}, ErrStaleCursor
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,event_json,digest,NULL AS reason FROM events WHERE run_id=? AND sequence>?
	UNION ALL SELECT sequence,NULL,NULL,reason FROM event_omissions WHERE run_id=? AND sequence>?
	ORDER BY sequence LIMIT ?`, runID, after, runID, after, limit)
	if err != nil {
		return EventPage{}, fmt.Errorf("%w: read events: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	// Reserve room for the surrounding frame, cursors and future completeness
	// fields. Each item is measured in its JSON wire shape before append.
	pageBytes := 1024
	for rows.Next() {
		var seq uint64
		var data []byte
		var digest, reason sql.NullString
		if err := rows.Scan(&seq, &data, &digest, &reason); err != nil {
			return EventPage{}, err
		}
		if reason.Valid {
			if !validEventOmissionReason(reason.String) {
				return EventPage{}, fmt.Errorf("%w: invalid stored event omission", ErrUnavailable)
			}
			itemBytes, err := encodedOmissionBytes(OmissionRange{From: seq, To: seq, Reason: reason.String})
			if err != nil || pageBytes+itemBytes+1 > maxEventPageBytes {
				return EventPage{}, ErrPageTooLarge
			}
			pageBytes += itemBytes + 1
			if len(page.Omissions) > 0 && page.Omissions[len(page.Omissions)-1].To+1 == seq && page.Omissions[len(page.Omissions)-1].Reason == reason.String {
				page.Omissions[len(page.Omissions)-1].To = seq
			} else {
				page.Omissions = append(page.Omissions, OmissionRange{From: seq, To: seq, Reason: reason.String})
			}
		} else {
			projection, err := storedProjection(data, digest.String)
			if err != nil {
				return EventPage{}, err
			}
			itemBytes, err := encodedEventBytes(seq, projection)
			if err != nil || pageBytes+itemBytes+1 > maxEventPageBytes {
				return EventPage{}, ErrPageTooLarge
			}
			pageBytes += itemBytes + 1
			page.Events = append(page.Events, Event{Sequence: seq, Projection: projection})
		}
		page.NextSequence = seq
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, err
	}
	if err := rows.Close(); err != nil {
		return EventPage{}, fmt.Errorf("%w: close event page rows: %v", ErrUnavailable, err)
	}
	return page, nil
}
