package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolFingerprintChangesWithContentAndIdentity(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tool")
	if err := os.WriteFile(path, []byte("first"), 0700); err != nil {
		t.Fatal(err)
	}
	first, err := fingerprintTool(path)
	if err != nil || first == "" {
		t.Fatalf("first fingerprint: %q, %v", first, err)
	}
	if err := os.WriteFile(path, []byte("other"), 0700); err != nil {
		t.Fatal(err)
	}
	changed, err := fingerprintTool(path)
	if err != nil || changed == first {
		t.Fatalf("same-inode content change: %q, %v", changed, err)
	}
	replacement := filepath.Join(filepath.Dir(path), "replacement")
	if err := os.WriteFile(replacement, []byte("other"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	replaced, err := fingerprintTool(path)
	if err != nil || replaced == changed {
		t.Fatalf("same-byte replacement: %q, %v", replaced, err)
	}
}

func TestToolFingerprintRejectsChangedPathAndOversize(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tool")
	if err := os.WriteFile(path, []byte("small"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprintTool(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxToolFingerprintBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprintTool(path); err == nil {
		t.Fatal("oversized executable accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprintTool(path); err == nil {
		t.Fatal("missing executable accepted")
	}
}
