package history

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func failureModeLaunch(t *testing.T, store *Store, id string, graph SafeJSON) {
	t.Helper()
	if err := store.Launch(context.Background(), LaunchRecord{
		RunID: id, ProjectID: "project", WorkspaceID: "workspace", DocumentID: "document",
		GraphID: "graph", GraphHash: "sha256:graph", StartedAt: time.Now().UTC(),
		Graph: graph, Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `[1,true,true]`),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlySQLiteConnectionRejectsLaunchWithoutLosingPriorHistory(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	failureModeLaunch(t, store, "prior", safeJSON(t, `{}`))
	if _, err := store.db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	err := store.Launch(ctx, LaunchRecord{RunID: "blocked", ProjectID: "project", WorkspaceID: "workspace", DocumentID: "document", GraphID: "graph", GraphHash: "sha256:graph", StartedAt: time.Now().UTC(), Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `[1,true,true]`)})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("read-only launch error=%v", err)
	}
	if prior, err := store.Get(ctx, "prior"); err != nil || prior.RunID != "prior" {
		t.Fatalf("prior history after read-only failure=%+v, %v", prior, err)
	}
	if _, err := store.Get(ctx, "blocked"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed launch left a marker: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	failureModeLaunch(t, store, "recovered", safeJSON(t, `{}`))
}

func TestFullSQLiteDatabaseRejectsLaunchAtomically(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	failureModeLaunch(t, store, "prior", safeJSON(t, `{}`))
	var pageCount int
	if err := store.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		t.Fatal(err)
	}
	var limitedTo int
	if err := store.db.QueryRowContext(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", pageCount)).Scan(&limitedTo); err != nil || limitedTo != pageCount {
		t.Fatalf("page cap=%d, %v; want %d", limitedTo, err, pageCount)
	}
	largeGraph := safeJSON(t, `{"content":"`+strings.Repeat("x", 1<<20)+`"}`)
	err := store.Launch(ctx, LaunchRecord{RunID: "blocked", ProjectID: "project", WorkspaceID: "workspace", DocumentID: "document", GraphID: "graph", GraphHash: "sha256:graph", StartedAt: time.Now().UTC(), Graph: largeGraph, Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `[1,true,true]`)})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("full database launch error=%v", err)
	}
	if prior, err := store.Get(ctx, "prior"); err != nil || prior.RunID != "prior" {
		t.Fatalf("prior history after full database=%+v, %v", prior, err)
	}
	if _, err := store.Get(ctx, "blocked"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("full database left a marker: %v", err)
	}
}

func TestCorruptSQLiteDatabaseIsNotReinitializedOrOverwritten(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	failureModeLaunch(t, store, "prior", safeJSON(t, `{}`))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "history.db")
	original, err := os.ReadFile(path)
	if err != nil || len(original) < 100 {
		t.Fatalf("read original database: %d bytes, %v", len(original), err)
	}
	corrupt := bytes.Clone(original)
	copy(corrupt[:16], []byte("not a sqlite db!"))
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(ctx, dir); reopened != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("corrupt database open=%v, %v", reopened, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, corrupt) {
		t.Fatalf("rejected open modified corrupt database: %v", err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if prior, err := recovered.Get(ctx, "prior"); err != nil || prior.RunID != "prior" {
		t.Fatalf("prior history after repair=%+v, %v", prior, err)
	}
}

func TestFailedV1MigrationPreservesPriorRunForRetry(t *testing.T) {
	ctx := context.Background()
	dir := openV1Fixture(t)
	path := filepath.Join(dir, "history.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	graph := safeJSON(t, `{"id":"prior"}`)
	launch := safeJSON(t, `{}`)
	completeness := safeJSON(t, `[1,true,true]`)
	_, err = raw.ExecContext(ctx, `INSERT INTO runs(run_id,project_id,workspace_id,document_id,graph_id,graph_hash,started_at,storage_state,pinned,pending_approval,revision,safe_graph_json,graph_digest,safe_launch_json,launch_digest,completeness_json,completeness_digest)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "prior", "project", "workspace", "document", "graph", "sha256:graph", formatHistoryTime(time.Now().UTC()), "active", 0, 0, 1,
		graph.Bytes(), graph.digest, launch.Bytes(), launch.digest, completeness.Bytes(), completeness.digest)
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, "CREATE INDEX managed_evidence_attempt_page ON runs(run_id)"); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if migrated, err := Open(ctx, dir); migrated != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("colliding migration open=%v, %v", migrated, err)
	}
	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed migration version=%d, %v", version, err)
	}
	var count int
	if err := raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE run_id='prior' AND safe_graph_json=? AND graph_digest=?", graph.Bytes(), graph.digest).Scan(&count); err != nil || count != 1 {
		t.Fatalf("prior row after failed migration count=%d, %v", count, err)
	}
	if _, err := raw.ExecContext(ctx, "DROP INDEX managed_evidence_attempt_page"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if prior, err := migrated.Get(ctx, "prior"); err != nil || prior.RunID != "prior" || prior.StorageState != Interrupted {
		t.Fatalf("prior history after successful retry=%+v, %v", prior, err)
	}
}
