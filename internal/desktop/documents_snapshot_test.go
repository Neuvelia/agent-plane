package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentGetKeepsDraftAndChecksRevision(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, "not a graph: [")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(doc.DocumentID, updated.Revision)
	if err != nil || got.Text != updated.Text || got.Validity != DocumentInvalid || len(got.Diagnostics) != 1 {
		t.Fatalf("draft = %#v, %v", got, err)
	}
	got.Diagnostics[0].Code = "altered"
	again, err := store.Get(doc.DocumentID, updated.Revision)
	if err != nil || again.Diagnostics[0].Code != "E_GRAPH_PARSE" {
		t.Fatalf("mutable draft = %#v, %v", again, err)
	}
	if _, err := store.Get(doc.DocumentID, doc.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("old revision error = %v", err)
	}
	if _, err := store.Get("missing", 1); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("unknown document error = %v", err)
	}
}

func TestDocumentRevisionCannotWrap(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	store.byID[doc.DocumentID].Revision = ^uint64(0)
	if _, err := store.Update(doc.DocumentID, ^uint64(0), validGraphText); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("revision overflow error = %v", err)
	}
}

func TestSavedSnapshotRequiresExactSavedRevisionAndFileIdentity(t *testing.T) {
	store, path := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.SavedSnapshot(doc.DocumentID, doc.Revision)
	if err != nil || snapshot.Text != validGraphText || snapshot.DiskDigest != doc.DiskDigest ||
		snapshot.FileIdentity == "" || snapshot.RootIdentity == "" || snapshot.DocumentID != doc.DocumentID {
		t.Fatalf("saved snapshot = %#v, %v", snapshot, err)
	}
	if _, err := store.SavedSnapshot(doc.DocumentID, doc.Revision+1); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("future revision error = %v", err)
	}
	updatedText := strings.Replace(validGraphText, "id: local", "id: local-2", 1)
	updated, err := store.Update(doc.DocumentID, doc.Revision, updatedText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavedSnapshot(doc.DocumentID, updated.Revision); !errors.Is(err, ErrDocumentUnsaved) {
		t.Fatalf("unsaved valid draft error = %v", err)
	}
	saved, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.SavedSnapshot(saved.DocumentID, saved.Revision)
	if err != nil || snapshot.Text != updatedText || snapshot.DiskDigest != saved.DiskDigest || snapshot.FileIdentity == "" {
		t.Fatalf("snapshot after save = %#v, %v", snapshot, err)
	}
	if snapshot.FileIdentity == doc.fileIdentity {
		t.Fatal("file identity did not advance after exchange")
	}
	if disk, err := os.ReadFile(path); err != nil || string(disk) != snapshot.Text {
		t.Fatalf("disk mismatch = %q, %v", disk, err)
	}
}

func TestSavedSnapshotRejectsInvalidAndChangedDisk(t *testing.T) {
	store, path := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := store.Update(doc.DocumentID, doc.Revision, "not a graph: [")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavedSnapshot(invalid.DocumentID, invalid.Revision); !errors.Is(err, ErrDocumentInvalid) {
		t.Fatalf("invalid draft error = %v", err)
	}
	valid, err := store.Update(invalid.DocumentID, invalid.Revision, validGraphText)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(validGraphText, "id: local", "id: external", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavedSnapshot(valid.DocumentID, valid.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("external content error = %v", err)
	}
}

func TestSavedSnapshotRejectsSameByteReplacementAndMovedProject(t *testing.T) {
	store, path := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(validGraphText), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavedSnapshot(doc.DocumentID, doc.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("same-byte replacement error = %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(validGraphText), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavedSnapshot(doc.DocumentID, doc.Revision); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("replaced project root error = %v", err)
	}
}

func TestSavedSnapshotRejectsPendingIntent(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.stateDir, "save-pending.intent"), []byte("in doubt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavedSnapshot(doc.DocumentID, doc.Revision); !errors.Is(err, ErrDocumentSaveInDoubt) {
		t.Fatalf("pending save error = %v", err)
	}
}
