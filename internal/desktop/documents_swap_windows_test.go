//go:build windows

package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDocumentSwapRetainsBothVersions(t *testing.T) {
	const candidateName = ".agentplane-save-00000000-0000-4000-8000-000000000001.candidate"
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile(candidateName, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("graph.yaml", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := swapDocumentEntries(root, candidateName, "graph.yaml"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{candidateName: "old", "graph.yaml": "new"} {
		got, err := root.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestWindowsPartialReplacementRestoresNamedOriginal(t *testing.T) {
	const candidateName = ".agentplane-save-00000000-0000-4000-8000-000000000001.candidate"
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile(candidateName, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("graph.yaml", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := openRegularDocument(root, "graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	originalID, err := identityOfFile(original)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "graph.yaml"), filepath.Join(dir, candidateName+".displaced")); err != nil {
		t.Fatal(err)
	}
	intent := saveIntent{Candidate: candidateName, OriginalIdentity: originalID, ExpectedDigest: digestBytes([]byte("old"))}
	if err := reconcileWindowsSwap(root, "graph.yaml", intent); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{candidateName: "new", "graph.yaml": "old"} {
		got, err := root.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := root.Lstat(candidateName + ".displaced"); !os.IsNotExist(err) {
		t.Fatalf("backup remains: %v", err)
	}
}

func TestWindowsRecoveryRemovesBackupAfterCompletedExchange(t *testing.T) {
	const candidateName = ".agentplane-save-00000000-0000-4000-8000-000000000002.candidate"
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile(candidateName+".displaced", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, candidateName+".displaced"), filepath.Join(dir, candidateName)); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("graph.yaml", []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := openRegularDocument(root, candidateName)
	if err != nil {
		t.Fatal(err)
	}
	originalID, err := identityOfFile(original)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := openRegularDocument(root, "graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	candidateID, err := identityOfFile(candidate)
	candidate.Close()
	if err != nil {
		t.Fatal(err)
	}
	intent := saveIntent{Candidate: candidateName, OriginalIdentity: originalID, CandidateIdentity: candidateID,
		ExpectedDigest: digestBytes([]byte("old")), CandidateDigest: digestBytes([]byte("new"))}
	if err := reconcileWindowsSwap(root, "graph.yaml", intent); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Lstat(candidateName + ".displaced"); !os.IsNotExist(err) {
		t.Fatalf("redundant backup remains: %v", err)
	}
	if data, err := root.ReadFile("graph.yaml"); err != nil || string(data) != "new" {
		t.Fatalf("target = %q, %v", data, err)
	}
	if data, err := root.ReadFile(candidateName); err != nil || string(data) != "old" {
		t.Fatalf("displaced = %q, %v", data, err)
	}
}
