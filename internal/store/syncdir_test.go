package store

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSyncDirSyncsARealDirectoryAndRejectsAMissingOneOnlyWhereItOpensIt(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("syncing an existing directory: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "absent")
	err := syncDir(missing)
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("directory sync is a documented no-op on Windows: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("a directory that cannot be opened must be reported")
	}
}
