package history

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openV2EvidenceFixture(t *testing.T) string {
	t.Helper()
	dir := openV1Fixture(t)
	db, err := sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range migrationV2 {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEvidenceDescriptorV2MigrationPreservesLegacyRowsAndRollsBack(t *testing.T) {
	ctx := context.Background()
	dir := openV2EvidenceFixture(t)
	db, err := sql.Open("sqlite", filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("legacy bytes")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	empty := sha256.Sum256([]byte(`{}`))
	emptyDigest := hex.EncodeToString(empty[:])
	started := formatHistoryTime(time.Now().UTC())
	if _, err = db.ExecContext(ctx, `INSERT INTO runs(run_id,project_id,workspace_id,document_id,graph_id,graph_hash,started_at,storage_state,pinned,pending_approval,revision,safe_graph_json,graph_digest,safe_launch_json,launch_digest,completeness_json,completeness_digest)
 VALUES('run','p','w','d','g','hash',?,'active',0,0,0,'{}',?,'{}',?,'{}',?)`, started, emptyDigest, emptyDigest, emptyDigest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "evidence"), 0700); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("a", 32)
	if err := os.WriteFile(filepath.Join(dir, "evidence", name), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO managed_evidence(evidence_id,run_id,attempt_id,node_run_id,kind,availability,created_at,relative_path,byte_length,digest,redacted,source_path,capture_group)
 VALUES('legacy','run','attempt',NULL,'preimage','available',?,?,?,?,0,'src/old.txt','old-group')`, started, "evidence/"+name, len(body), digest); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.EvidenceDescriptors(ctx, "run", "", "", 10)
	if err != nil || len(page.Descriptors) != 1 || page.Descriptors[0].AttemptID != "attempt" || page.Descriptors[0].NodeRunID != "" || page.Descriptors[0].Digest != digest || page.Descriptors[0].SourcePath != "src/old.txt" {
		t.Fatalf("legacy descriptor=%+v err=%v", page, err)
	}
	chunk, err := store.Evidence(ctx, "legacy", 0, 100)
	if err != nil || string(chunk.Data) != string(body) {
		t.Fatalf("legacy bytes=%q err=%v", chunk.Data, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	broken := openV2EvidenceFixture(t)
	raw, err := sql.Open("sqlite", filepath.Join(broken, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.ExecContext(ctx, "CREATE TABLE managed_evidence_v3 (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(ctx, broken); reopened != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("failed v3 migration accepted: store=%v err=%v", reopened, err)
	}
	raw, err = sql.Open("sqlite", filepath.Join(broken, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var version int
	if err = raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatalf("failed v3 migration changed version=%d err=%v", version, err)
	}
}

func openV1Fixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range migrationV1 {
		if _, err = db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("PRAGMA user_version=1"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEvidenceDescriptorV1MigrationAndRollback(t *testing.T) {
	ctx := context.Background()
	dir := openV1Fixture(t)
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err = store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatalf("migrated version=%d err=%v", version, err)
	}
	launchEvidenceRun(t, store, "run", time.Now().UTC())
	if err = store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "old", RunID: "run", AttemptID: "a", Kind: "diff", CreatedAt: time.Now().UTC(), OmissionReason: "diff_unavailable"}); err != nil {
		t.Fatal(err)
	}
	page, err := store.EvidenceDescriptors(ctx, "run", "", "", 1)
	if err != nil || len(page.Descriptors) != 1 || page.Descriptors[0].Redacted {
		t.Fatalf("v1 migration page=%+v err=%v", page, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	broken := openV1Fixture(t)
	raw, err := sql.Open("sqlite", filepath.Join(broken, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.ExecContext(ctx, "CREATE INDEX managed_evidence_attempt_page ON runs(run_id)"); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(ctx, broken); reopened != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("colliding v2 migration open=%v err=%v", reopened, err)
	}
	raw, err = sql.Open("sqlite", filepath.Join(broken, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err = raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed migration changed version=%d err=%v", version, err)
	}
	rows, err := raw.QueryContext(ctx, "PRAGMA table_info(managed_evidence)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "redacted" {
			t.Fatal("failed migration added redacted column")
		}
	}
}

func TestNodeScopedEvidenceIdentityAndPaging(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Now().UTC()
	launchEvidenceRun(t, store, "run", started)
	content, err := ParseSafeEvidence([]byte("node content"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []EvidenceCapture{
		{EvidenceID: "node-evidence", RunID: "run", NodeRunID: "node-one", Kind: "postimage", CreatedAt: started, Content: &content},
		{EvidenceID: "loop-evidence", RunID: "run", AttemptID: "attempt-one", NodeRunID: "node-two", Kind: "diff", CreatedAt: started.Add(time.Second), OmissionReason: "diff_unavailable"},
	} {
		if err := store.CaptureEvidence(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.EvidenceDescriptors(ctx, "run", "", "", 10)
	if err != nil || len(page.Descriptors) != 2 || page.Descriptors[0].AttemptID != "" || page.Descriptors[0].NodeRunID != "node-one" {
		t.Fatalf("mixed evidence=%+v err=%v", page, err)
	}
	filtered, err := store.EvidenceDescriptors(ctx, "run", "attempt-one", "", 10)
	if err != nil || len(filtered.Descriptors) != 1 || filtered.Descriptors[0].EvidenceID != "loop-evidence" {
		t.Fatalf("attempt filter=%+v err=%v", filtered, err)
	}
	for _, invalid := range []EvidenceCapture{
		{EvidenceID: "no-owner", RunID: "run", Kind: "diff", CreatedAt: started, OmissionReason: "diff_unavailable"},
		{EvidenceID: "bad-node", RunID: "run", NodeRunID: strings.Repeat("x", 257), Kind: "diff", CreatedAt: started, OmissionReason: "diff_unavailable"},
	} {
		if err := store.CaptureEvidence(ctx, invalid); err == nil {
			t.Fatalf("invalid evidence identity accepted: %+v", invalid)
		}
	}
}

func TestEvidenceDescriptorsPageRedactionAndStaleCursor(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	launchEvidenceRun(t, store, "run", started)
	content, err := ParseSafeEvidence([]byte("sanitized"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []EvidenceCapture{
		{EvidenceID: "e1", RunID: "run", AttemptID: "a1", NodeRunID: "n1", Kind: "preimage", SourcePath: "src/work.txt", CaptureGroup: "group-1", CreatedAt: started, Content: &content, Redacted: true},
		{EvidenceID: "e2", RunID: "run", AttemptID: "a2", NodeRunID: "n2", Kind: "diff", CreatedAt: started.Add(time.Second), OmissionReason: "preimage_missing"},
	} {
		if err = store.CaptureEvidence(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.EvidenceDescriptors(ctx, "run", "", "", 1)
	if err != nil || len(first.Descriptors) != 1 || first.Descriptors[0].EvidenceID != "e1" || !first.Descriptors[0].Redacted || first.Descriptors[0].SourcePath != "src/work.txt" || first.Descriptors[0].CaptureGroup != "group-1" || first.Complete || first.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := store.EvidenceDescriptors(ctx, "run", "", first.NextCursor, 1)
	if err != nil || len(second.Descriptors) != 1 || second.Descriptors[0].EvidenceID != "e2" || second.NextCursor != "" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	filtered, err := store.EvidenceDescriptors(ctx, "run", "a1", "", 2)
	if err != nil || len(filtered.Descriptors) != 1 || filtered.Descriptors[0].AttemptID != "a1" || filtered.Complete {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	if _, err := store.EvidenceDescriptors(ctx, "other", "", first.NextCursor, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other run err=%v", err)
	}
	if err = store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e3", RunID: "run", AttemptID: "a1", Kind: "diff", CreatedAt: started.Add(2 * time.Second), OmissionReason: "capture_failed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EvidenceDescriptors(ctx, "run", "", first.NextCursor, 1); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("cursor survived capture: %v", err)
	}
}

func TestEvidenceDescriptorListingRejectsChangedFile(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	launchEvidenceRun(t, store, "run", time.Now().UTC())
	content, err := ParseSafeEvidence([]byte("original"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CaptureEvidence(ctx, EvidenceCapture{EvidenceID: "e", RunID: "run", AttemptID: "a", Kind: "diff", CreatedAt: time.Now().UTC(), Content: &content}); err != nil {
		t.Fatal(err)
	}
	var path string
	if err = store.db.QueryRowContext(ctx, "SELECT relative_path FROM managed_evidence WHERE evidence_id='e'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store.dir, path), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.EvidenceDescriptors(ctx, "run", "", "", 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("changed file descriptor returned: %v", err)
	}
}

func TestEvidenceDescriptorsEmptyActiveAndInterruptedAreIncomplete(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	launchEvidenceRun(t, store, "run", time.Now().UTC())
	active, err := store.EvidenceDescriptors(ctx, "run", "", "", 10)
	if err != nil || active.Complete || len(active.Descriptors) != 0 {
		t.Fatalf("active empty evidence=%+v err=%v", active, err)
	}
	dir := store.dir
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	interrupted, err := reopened.EvidenceDescriptors(ctx, "run", "", "", 10)
	if err != nil || interrupted.Complete || len(interrupted.Descriptors) != 0 {
		t.Fatalf("interrupted empty evidence=%+v err=%v", interrupted, err)
	}
}
