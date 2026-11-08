package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentCreateIsExclusiveAndOpensValidGraph(t *testing.T) {
	store, existing := documentFixture(t)
	doc, err := store.Create("project-1", ".agents/graphs/new.yaml", "new-graph")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Revision != 1 || doc.Validity != DocumentValid || doc.SemanticVersion == "" || !strings.Contains(doc.Text, "id: new-graph") {
		t.Fatalf("created document = %#v", doc)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(existing), "new.yaml"))
	if err != nil || string(data) != doc.Text {
		t.Fatalf("saved bytes = %q, %v", data, err)
	}
	if _, err := store.Create("project-1", ".agents/graphs/new.yaml", "other"); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("duplicate create = %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(filepath.Dir(existing), "new.yaml"))
	if string(data) != doc.Text {
		t.Fatal("create overwrote existing graph")
	}
}

func TestDocumentCreateRejectsUnsafePathAndGraphID(t *testing.T) {
	store, existing := documentFixture(t)
	for _, relative := range []string{"../escape.yaml", ".agents/graphs/missing/new.yaml", ".agents/graphs/local.yaml"} {
		_, err := store.Create("project-1", relative, "valid")
		if err == nil {
			t.Fatalf("create %q succeeded", relative)
		}
	}
	if _, err := store.Create("project-1", ".agents/graphs/bad.yaml", "Invalid.ID"); err == nil {
		t.Fatal("invalid graph ID accepted")
	}
	if _, err := store.Create("project-1", ".agents/graphs/huge.yaml", "a"+strings.Repeat("b", MaxDocumentBytes)); !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("oversized graph ID = %v", err)
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != validGraphText {
		t.Fatalf("existing graph changed: %q, %v", data, err)
	}
}

func TestDocumentReloadRequiresDiscardAndAcceptsAtomicReplacement(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.Update(doc.DocumentID, doc.Revision, "invalid: [")
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(filename), "replacement.yaml")
	replaced := strings.Replace(validGraphText, "id: local", "id: external", 1)
	if err := os.WriteFile(replacement, []byte(replaced), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filename); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reload(doc.DocumentID, draft.Revision, false); !errors.Is(err, ErrDocumentUnsaved) {
		t.Fatalf("dirty reload = %v", err)
	}
	if _, err := store.Reload(doc.DocumentID, doc.Revision, true); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("stale reload = %v", err)
	}
	reloaded, err := store.Reload(doc.DocumentID, draft.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Text != replaced || reloaded.Revision != draft.Revision+1 || reloaded.DiskDigest != digestBytes([]byte(replaced)) || reloaded.Validity != DocumentValid {
		t.Fatalf("reloaded = %#v", reloaded)
	}
}

func TestDocumentReloadRejectsSymlinkReplacementWithoutLosingDraft(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.Update(doc.DocumentID, 1, "my draft")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filename); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reload(doc.DocumentID, draft.Revision, true); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("symlink reload = %v", err)
	}
	retained, err := store.Get(doc.DocumentID, draft.Revision)
	if err != nil || retained.Text != draft.Text {
		t.Fatalf("lost draft: %#v, %v", retained, err)
	}
}

func TestDocumentCreatePublishedFileSurvivesInterruptedResponse(t *testing.T) {
	store, existing := documentFixture(t)
	store.afterCreatePublish = func() error { return ErrDocumentSaveInDoubt }
	if _, err := store.Create("project-1", ".agents/graphs/interrupted.yaml", "interrupted"); !errors.Is(err, ErrDocumentSaveInDoubt) {
		t.Fatalf("interrupted create = %v", err)
	}
	store = NewDocumentStore(store.resolver, store.stateDir)
	doc, err := store.Open("project-1", ".agents/graphs/interrupted.yaml")
	if err != nil || doc.Validity != DocumentValid || !strings.Contains(doc.Text, "id: interrupted") {
		t.Fatalf("recovered graph = %#v, %v", doc, err)
	}
	if _, err := os.ReadFile(filepath.Join(filepath.Dir(existing), "interrupted.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentCreateRejectsSymlinkParent(t *testing.T) {
	store, existing := documentFixture(t)
	link := filepath.Join(filepath.Dir(filepath.Dir(existing)), "shortcut")
	if err := os.Symlink(filepath.Dir(existing), link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("project-1", ".agents/shortcut/new.yaml", "new"); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("symlink parent = %v", err)
	}
}
