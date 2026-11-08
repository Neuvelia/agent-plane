//go:build windows

package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDocumentCreatePublishesCompleteFileWithoutClobber(t *testing.T) {
	store, existing := documentFixture(t)
	doc, err := store.Create("project-1", ".agents/graphs/new.yaml", "new-graph")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(existing), "new.yaml"))
	if err != nil || string(data) != doc.Text || doc.Validity != DocumentValid {
		t.Fatalf("published = %q, %#v, %v", data, doc, err)
	}
	if _, err := store.Create("project-1", ".agents/graphs/new.yaml", "different"); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("collision = %v", err)
	}
	data, err = os.ReadFile(filepath.Join(filepath.Dir(existing), "new.yaml"))
	if err != nil || string(data) != doc.Text {
		t.Fatalf("collision overwrote target: %q, %v", data, err)
	}
}
