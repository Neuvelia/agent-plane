package history

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func safeJSON(t *testing.T, source string) SafeJSON {
	t.Helper()
	value, err := ParseSafeJSON([]byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestListOrdersAndFiltersAcrossFullSecondBoundary(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	second := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		id string
		at time.Time
	}{
		{"whole", second},
		{"fraction", second.Add(100 * time.Millisecond)},
		{"next", second.Add(time.Second)},
	} {
		if err := store.Launch(ctx, LaunchRecord{RunID: item.id, ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: item.at, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	query := ListQuery{Limit: 1}
	for _, want := range []string{"next", "fraction", "whole"} {
		page, err := store.List(ctx, query)
		if err != nil || len(page.Runs) != 1 || page.Runs[0].RunID != want {
			t.Fatalf("history page for %s = %+v, %v", want, page, err)
		}
		query.Cursor = page.NextCursor
	}
	from := second.Add(50 * time.Millisecond)
	filtered, err := store.List(ctx, ListQuery{StartedFrom: &from, Limit: 3})
	if err != nil || len(filtered.Runs) != 2 || filtered.Runs[0].RunID != "next" || filtered.Runs[1].RunID != "fraction" {
		t.Fatalf("history since 50ms = %+v, %v", filtered, err)
	}
	before := from
	filtered, err = store.List(ctx, ListQuery{StartedBefore: &before, Limit: 3})
	if err != nil || len(filtered.Runs) != 1 || filtered.Runs[0].RunID != "whole" {
		t.Fatalf("history before 50ms = %+v, %v", filtered, err)
	}
}

func TestGetRejectsChangedSafeProjection(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: time.Now(), Graph: safeJSON(t, `{"id":"original"}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE runs SET safe_graph_json=? WHERE run_id='run'`, `{"id":"modified"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "run"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("changed projection accepted: %v", err)
	}
}

func TestEventsRejectChangedAndMalformedSafeProjections(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "hash", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(ctx, Finalization{RunID: "run", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{"type":"original"}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE events SET event_json=? WHERE run_id='run' AND sequence=1`, `{"type":"modified"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(ctx, "run", 0, 10); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("changed event accepted: %v", err)
	}
	invalid := []byte(`{"type":`)
	sum := sha256.Sum256(invalid)
	if _, err := store.db.ExecContext(ctx, `UPDATE events SET event_json=?,digest=? WHERE run_id='run' AND sequence=1`, invalid, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(ctx, "run", 0, 10); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("malformed event with matching digest accepted: %v", err)
	}
}

func TestSafeJSONCanonicalizesObjectKeys(t *testing.T) {
	first := safeJSON(t, `{"outer":{"b":2,"a":1},"z":0}`)
	second := safeJSON(t, `{"z":0,"outer":{"a":1,"b":2}}`)
	if first.digest != second.digest || string(first.Bytes()) != string(second.Bytes()) {
		t.Fatalf("equivalent projections differ: %q %q", first.Bytes(), second.Bytes())
	}
}

func TestSafeJSONRejectsDuplicateKeysBeforeSecretCheck(t *testing.T) {
	_, err := ParseSafeJSON([]byte(`{"token":"known-secret","token":"safe"}`), []string{"known-secret"})
	if err == nil {
		t.Fatal("ambiguous duplicate keys hid a known secret")
	}
}

func TestSafeJSONPreservesLargeNumberLexemes(t *testing.T) {
	value, err := ParseSafeJSON([]byte(`{"number":1e1000}`), nil)
	if err != nil || string(value.Bytes()) != `{"number":1e1000}` {
		t.Fatalf("large finite JSON number = %q, %v", value.Bytes(), err)
	}
}

func TestEventPageCompletenessIsTraceSpecific(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "partial-evidence", started)
	if err := store.Finalize(ctx, Finalization{RunID: "partial-evidence", EndedAt: started.Add(time.Second), RuntimeState: "succeeded", Watermark: 1,
		Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `[1,true,true,true,true,true,false]`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}); err != nil {
		t.Fatal(err)
	}
	page, err := store.Events(ctx, "partial-evidence", 0, 1)
	if err != nil || !page.Complete || len(page.Omissions) != 0 || page.CompletenessDigest == "" {
		t.Fatalf("partial evidence hid complete trace: %+v %v", page, err)
	}
}

func TestLaunchMarkerBecomesInterruptedWithoutInventedEvents(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	marker := LaunchRecord{
		RunID: "run-1", ProjectID: "project-1", WorkspaceID: "workspace-1",
		DocumentID: "document-1", GraphID: "graph-1", GraphHash: "sha256:abc",
		StartedAt: started, Graph: safeJSON(t, `{"id":"graph-1"}`),
		Launch: safeJSON(t, `{"input":"safe"}`), Completeness: safeJSON(t, `{"complete":false}`),
	}
	if err := store.Launch(ctx, marker); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.Get(ctx, marker.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.StorageState != Interrupted || run.RuntimeState != "" || run.ProjectID != marker.ProjectID {
		t.Fatalf("reopened marker = %+v", run)
	}
	page, err := store.Events(ctx, marker.RunID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 0 || page.Finished || page.Watermark != 0 {
		t.Fatalf("interrupted marker acquired synthetic events: %+v", page)
	}
}

func TestFinalizationIsIdempotentAndRejectsConflictingEventAtomically(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if err := store.Launch(ctx, LaunchRecord{RunID: "run-1", ProjectID: "p", WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "sha256:abc", StartedAt: started, Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{"complete":false}`)}); err != nil {
		t.Fatal(err)
	}
	final := Finalization{RunID: "run-1", EndedAt: started.Add(time.Minute), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{"status":"succeeded"}`), Completeness: safeJSON(t, `{"complete":true}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{"sequence":1,"type":"run_started"}`)}}}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(ctx, final); err != nil {
		t.Fatalf("identical ingestion: %v", err)
	}
	run, err := store.Get(ctx, "run-1")
	if err != nil || run.StorageState != Complete || run.RuntimeState != "succeeded" {
		t.Fatalf("final run=%+v, %v", run, err)
	}
	conflict := final
	conflict.Watermark = 2
	conflict.Events = []Event{{Sequence: 2, Projection: safeJSON(t, `{"sequence":2,"type":"node_started"}`)}, {Sequence: 1, Projection: safeJSON(t, `{"sequence":1,"type":"run_finished"}`)}}
	if err := store.Finalize(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting event error=%v", err)
	}
	page, err := store.Events(ctx, "run-1", 0, 10)
	if err != nil || len(page.Events) != 1 || page.Events[0].Sequence != 1 || page.Watermark != 1 {
		t.Fatalf("conflicting ingestion changed trace: %+v, %v", page, err)
	}
}

func TestListFiltersAndInvalidatesCursorAfterStatusMutation(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		id, project string
		minute      int
	}{{"run-a", "p", 0}, {"run-b", "p", 1}, {"run-c", "p", 2}, {"run-other", "other", 3}} {
		if err := store.Launch(ctx, LaunchRecord{RunID: tc.id, ProjectID: tc.project, WorkspaceID: "w", DocumentID: "d", GraphID: "g", GraphHash: "sha256:abc", StartedAt: started.Add(time.Duration(tc.minute) * time.Minute), Graph: safeJSON(t, `{}`), Launch: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.List(ctx, ListQuery{ProjectID: "p", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Runs) != 2 || first.Runs[0].RunID != "run-c" || first.Runs[1].RunID != "run-b" || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	second, err := store.List(ctx, ListQuery{ProjectID: "p", Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Runs) != 1 || second.Runs[0].RunID != "run-a" {
		t.Fatalf("second page=%+v, %v", second, err)
	}
	if _, err := store.List(ctx, ListQuery{ProjectID: "other", Limit: 2, Cursor: first.NextCursor}); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("cursor reused with different filter: %v", err)
	}
	if err := store.Finalize(ctx, Finalization{RunID: "run-a", EndedAt: started.Add(2 * time.Minute), RuntimeState: "succeeded", Watermark: 1, Result: safeJSON(t, `{}`), Completeness: safeJSON(t, `{}`), Events: []Event{{Sequence: 1, Projection: safeJSON(t, `{}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, ListQuery{ProjectID: "p", Limit: 2, Cursor: first.NextCursor}); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("status mutation did not invalidate cursor: %v", err)
	}
}

func TestNewerSchemaIsRejectedWithoutChangingVersion(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, "PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, "PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(ctx, dir); reopened != nil || !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("newer schema open=%v, %v", reopened, err)
	}
	raw, err = sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var version int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatalf("schema version after rejected open=%d, %v", version, err)
	}
	var mode string
	if err := raw.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "delete" {
		t.Fatalf("rejected newer schema changed journal mode to %q: %v", mode, err)
	}
}

func TestFailedMigrationLeavesOriginalSchemaAndVersion(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, "CREATE TABLE runs (sentinel TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(ctx, dir); reopened != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("colliding migration open=%v, %v", reopened, err)
	}
	raw, err = sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var version int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 0 {
		t.Fatalf("failed migration changed version=%d, %v", version, err)
	}
	var definition string
	if err := raw.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='runs'").Scan(&definition); err != nil || definition != "CREATE TABLE runs (sentinel TEXT NOT NULL)" {
		t.Fatalf("original table changed: %q, %v", definition, err)
	}
}

func TestOpenRejectsBroadPermissionsAndStaticSymlinkSubstitution(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(ctx, dir); store != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("broad directory accepted: %v, %v", store, err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "history.db")); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
	if store, err := Open(ctx, dir); store != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("symlink database accepted: %v, %v", store, err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "unchanged" {
		t.Fatalf("outside target changed: %q, %v", got, err)
	}
}
