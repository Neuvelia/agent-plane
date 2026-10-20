package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixedProjectRoot ResolvedProject

func (r fixedProjectRoot) ResolveProject(id string) (ResolvedProject, error) {
	if id != "project-1" {
		return ResolvedProject{}, errors.New("unknown project")
	}
	return ResolvedProject(r), nil
}

const validGraphText = `# user comment
schema_version: 0
id: local
inputs: {}
nodes: {}
edges: []
outputs: {}
`

func documentFixture(t *testing.T) (*DocumentStore, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agents", "graphs"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".agents", "graphs", "local.yaml")
	if err := os.WriteFile(path, []byte(validGraphText), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := RegisterProjectRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	return NewDocumentStore(fixedProjectRoot(project), stateDir), path
}

func TestDocumentOpenUpdateAndInvalidDraft(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.DocumentID == "" || doc.Revision != 1 || doc.Text != validGraphText || doc.DiskDigest == "" || doc.Validity != DocumentValid || doc.SemanticVersion == "" {
		t.Fatalf("opened document = %#v", doc)
	}
	reopened, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil || reopened.DocumentID != doc.DocumentID {
		t.Fatalf("reopened = %#v, %v", reopened, err)
	}
	invalid, err := store.Update(doc.DocumentID, 1, "not a graph: [")
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Revision != 2 || invalid.Validity != DocumentInvalid || invalid.SemanticVersion != "" || invalid.Text != "not a graph: [" {
		t.Fatalf("invalid draft = %#v", invalid)
	}
	if len(invalid.Diagnostics) != 1 || invalid.Diagnostics[0].Code != "E_GRAPH_PARSE" || invalid.Diagnostics[0].Message == "" {
		t.Fatalf("missing parse diagnostic: %#v", invalid.Diagnostics)
	}
	if _, err := store.Update(doc.DocumentID, 1, validGraphText); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	reopened, err = store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil || reopened.Revision != 2 || reopened.Text != invalid.Text {
		t.Fatalf("draft was lost: %#v, %v", reopened, err)
	}
	valid, err := store.Update(doc.DocumentID, 2, strings.Replace(validGraphText, "id: local", "id: local-2", 1))
	if err != nil || valid.Validity != DocumentValid || valid.Revision != 3 || valid.SemanticVersion == doc.SemanticVersion {
		t.Fatalf("valid revision = %#v, %v", valid, err)
	}
}

func TestDocumentRejectsEscapesAndSymlinks(t *testing.T) {
	store, path := documentFixture(t)
	for _, relative := range []string{"", "../outside.yaml", "/tmp/outside.yaml", ".agents/../local.yaml", `.agents\graphs\local.yaml`} {
		if _, err := store.Open("project-1", relative); !errors.Is(err, ErrInvalidDocumentPath) {
			t.Errorf("path %q: error = %v", relative, err)
		}
	}
	link := filepath.Join(filepath.Dir(path), "linked.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open("project-1", ".agents/graphs/linked.yaml"); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("symlink target error = %v", err)
	}
	parent := filepath.Join(filepath.Dir(filepath.Dir(path)), "shortcut")
	if err := os.Symlink(filepath.Dir(path), parent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open("project-1", ".agents/shortcut/local.yaml"); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("symlink parent error = %v", err)
	}
}

func TestDocumentRejectsReplacedProjectRoot(t *testing.T) {
	store, path := documentFixture(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	moved := root + "-moved"
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, ".agents", "graphs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, ".agents", "graphs", "local.yaml"), []byte(validGraphText), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open("project-1", ".agents/graphs/local.yaml"); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("replaced root error = %v", err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outside, root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open("project-1", ".agents/graphs/local.yaml"); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("different real root error = %v", err)
	}
}

func TestDocumentUpdateBounds(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(doc.DocumentID, 1, strings.Repeat("x", MaxDocumentBytes+1)); !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("oversized update error = %v", err)
	}
	if _, err := store.Update(doc.DocumentID, 1, string([]byte{0xff})); !errors.Is(err, ErrDocumentInvalidEncoding) {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
}

func TestDocumentDiagnosticsAreDetached(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	badEdges := strings.Replace(validGraphText, "edges: []", "edges:\n  - from: missing.outputs.value\n    to: other.inputs.value", 1)
	updated, err := store.Update(doc.DocumentID, 1, badEdges)
	if err != nil {
		t.Fatal(err)
	}
	var pointerFound bool
	for i := range updated.Diagnostics {
		if updated.Diagnostics[i].Location.EdgeIndex != nil {
			pointerFound = true
			*updated.Diagnostics[i].Location.EdgeIndex = 999
		}
	}
	if !pointerFound {
		t.Fatalf("fixture lacks edge index diagnostics: %#v", updated.Diagnostics)
	}
	reopened, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range reopened.Diagnostics {
		if diagnostic.Location.EdgeIndex != nil && *diagnostic.Location.EdgeIndex == 999 {
			t.Fatalf("caller mutated stored diagnostic: %#v", reopened.Diagnostics)
		}
	}
}

func TestDocumentLayoutMetadataDoesNotChangeSemanticVersion(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	withLayout := strings.Replace(validGraphText, "inputs: {}", "metadata:\n  layout:\n    version: 1\n    x: 12\ninputs: {}", 1)
	updated, err := store.Update(doc.DocumentID, doc.Revision, withLayout)
	if err != nil || updated.Validity != DocumentValid || updated.SemanticVersion != doc.SemanticVersion {
		t.Fatalf("layout-only revision changed semantic identity: %#v, %v", updated, err)
	}
}
