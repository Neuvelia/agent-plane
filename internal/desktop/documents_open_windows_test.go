//go:build windows

package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDocumentOpenRejectsReparseAndDirectory(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte("schema_version: 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openRegularDocument(root, "graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := openRegularDocument(root, "."); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("directory accepted: %v", err)
	}
	if err := os.Symlink("graph.yaml", filepath.Join(dir, "linked.yaml")); err == nil {
		if file, err := openRegularDocument(root, "linked.yaml"); file != nil || !errors.Is(err, ErrInvalidDocumentPath) {
			t.Fatalf("reparse point accepted: %v, %v", file, err)
		}
	} else if os.Getenv("AGENTPLANE_REQUIRE_REPARSE_TEST") == "1" {
		t.Fatalf("required file reparse fixture could not be created: %v", err)
	} else {
		t.Logf("file reparse assertion unavailable on this host: %v", err)
	}
}
