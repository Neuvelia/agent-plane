package desktop

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"agentplane/internal/graph"
)

const MaxDocumentBytes = 4 << 20
const MaxRelativePathBytes = 4096

var ErrInvalidDocumentPath = errors.New("invalid document path")
var ErrDocumentConflict = errors.New("document revision conflict")
var ErrDocumentTooLarge = errors.New("document exceeds 4 MiB")
var ErrDocumentNotFound = errors.New("document not found")
var ErrDocumentInvalidEncoding = errors.New("document is not UTF-8")
var ErrDocumentInvalid = errors.New("graph document is invalid")
var ErrDocumentUnsaved = errors.New("graph document has unsaved changes")

type DocumentValidity string

const (
	DocumentValid   DocumentValidity = "valid"
	DocumentInvalid DocumentValidity = "invalid"
)

type Document struct {
	DocumentID      string
	ProjectID       string
	RelativePath    string
	Text            string
	Revision        uint64
	DiskDigest      string
	Validity        DocumentValidity
	SemanticVersion string
	Diagnostics     []graph.Diagnostic
	path            string
	rootIdentity    ProjectIdentity
	fileIdentity    ProjectIdentity
}

type ProjectIdentity string

type ResolvedProject struct {
	CanonicalRoot string
	Identity      ProjectIdentity
}

// ProjectResolver supplies only previously registered project roots. The
// document store does not accept absolute paths from protocol callers.
type ProjectResolver interface {
	ResolveProject(projectID string) (ResolvedProject, error)
}

type DocumentStore struct {
	mu                   sync.Mutex
	resolver             ProjectResolver
	stateDir             string
	byID                 map[string]*Document
	byPath               map[string]string
	beforeOpen           func() error                                      // deterministic race seam for package tests
	beforeExchange       func() error                                      // deterministic race seam for package tests
	beforeRestore        func() error                                      // deterministic restore race seam for package tests
	afterExchange        func() error                                      // deterministic crash seam for package tests
	beforeRecoveryCommit func() error                                      // deterministic recovery race seam for package tests
	candidateWriter      func(*os.Root, string, []byte, os.FileMode) error // write failure seam for package tests
}

// RegisterProjectRoot captures the directory identity associated with a native
// folder selection. Task 4 persists this identity with the project registry.
func RegisterProjectRoot(selectedPath string) (ResolvedProject, error) {
	canonical, err := filepath.EvalSymlinks(selectedPath)
	if err != nil || !filepath.IsAbs(canonical) {
		return ResolvedProject{}, ErrInvalidDocumentPath
	}
	canonical = filepath.Clean(canonical)
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return ResolvedProject{}, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return ResolvedProject{}, err
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil || !info.IsDir() {
		return ResolvedProject{}, ErrInvalidDocumentPath
	}
	identity, err := identityOfFile(dir)
	if err != nil {
		return ResolvedProject{}, err
	}
	project := ResolvedProject{CanonicalRoot: canonical, Identity: identity}
	if err := verifyRegisteredRoot(root, project); err != nil {
		return ResolvedProject{}, err
	}
	return project, nil
}

func openRegisteredRoot(project ResolvedProject) (*os.Root, error) {
	if project.Identity == "" || !filepath.IsAbs(project.CanonicalRoot) {
		return nil, ErrInvalidDocumentPath
	}
	root, err := os.OpenRoot(project.CanonicalRoot)
	if err != nil {
		return nil, fmt.Errorf("open project root: %w", err)
	}
	if err := verifyRegisteredRoot(root, project); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func verifyRegisteredRoot(root *os.Root, project ResolvedProject) error {
	dir, err := root.Open(".")
	if err != nil {
		return ErrInvalidDocumentPath
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil || !info.IsDir() {
		return ErrInvalidDocumentPath
	}
	identity, err := identityOfFile(dir)
	if err != nil || identity != project.Identity {
		return ErrInvalidDocumentPath
	}
	current, err := os.Lstat(project.CanonicalRoot)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, current) {
		return ErrInvalidDocumentPath
	}
	return nil
}

func NewDocumentStore(resolver ProjectResolver, stateDir string) *DocumentStore {
	return &DocumentStore{resolver: resolver, stateDir: stateDir, byID: make(map[string]*Document), byPath: make(map[string]string)}
}

func (s *DocumentStore) Open(projectID, relativePath string) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := projectID + "\x00" + relativePath
	if id, ok := s.byPath[key]; ok {
		return cloneDocument(s.byID[id]), nil
	}
	project, err := s.resolver.ResolveProject(projectID)
	if err != nil {
		return Document{}, fmt.Errorf("resolve project: %w", err)
	}
	rootHandle, err := openRegisteredRoot(project)
	if err != nil {
		return Document{}, err
	}
	defer rootHandle.Close()
	filename, err := resolveDocumentPath(project.CanonicalRoot, relativePath)
	if err != nil {
		return Document{}, err
	}
	if s.beforeOpen != nil {
		if err := s.beforeOpen(); err != nil {
			return Document{}, err
		}
	}
	file, err := openRegularDocument(rootHandle, relativePath)
	if err != nil {
		return Document{}, fmt.Errorf("open graph document: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	if err != nil {
		return Document{}, fmt.Errorf("read graph document: %w", err)
	}
	if len(data) > MaxDocumentBytes {
		return Document{}, ErrDocumentTooLarge
	}
	if !utf8.Valid(data) {
		return Document{}, ErrDocumentInvalidEncoding
	}
	if err := verifyOpenPath(file, rootHandle, project, relativePath, filename); err != nil {
		return Document{}, err
	}
	fileIdentity, err := identityOfFile(file)
	if err != nil {
		return Document{}, err
	}
	id, err := randomDocumentID()
	if err != nil {
		return Document{}, fmt.Errorf("create document identity: %w", err)
	}
	doc := &Document{DocumentID: id, ProjectID: projectID, RelativePath: relativePath,
		Text: string(data), Revision: 1, DiskDigest: digestBytes(data), path: filename,
		rootIdentity: project.Identity, fileIdentity: fileIdentity}
	validateDocument(doc)
	s.byID[id] = doc
	s.byPath[key] = id
	return cloneDocument(doc), nil
}

func (s *DocumentStore) Update(documentID string, expectedRevision uint64, text string) (Document, error) {
	if len(text) > MaxDocumentBytes || !utf8.ValidString(text) {
		if len(text) > MaxDocumentBytes {
			return Document{}, ErrDocumentTooLarge
		}
		return Document{}, ErrDocumentInvalidEncoding
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.byID[documentID]
	if !ok {
		return Document{}, ErrDocumentNotFound
	}
	if expectedRevision != doc.Revision {
		return Document{}, ErrDocumentConflict
	}
	if doc.Revision == ^uint64(0) {
		return Document{}, ErrDocumentConflict
	}
	doc.Text = text
	doc.Revision++
	validateDocument(doc)
	return cloneDocument(doc), nil
}

func resolveDocumentPath(root, relative string) (string, error) {
	if !validRelativeDocumentPath(relative) {
		return "", ErrInvalidDocumentPath
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(canonical) || filepath.Clean(canonical) != filepath.Clean(root) {
		return "", fmt.Errorf("project root unavailable: %w", ErrInvalidDocumentPath)
	}
	canonical = filepath.Clean(canonical)
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project root is not a directory: %w", ErrInvalidDocumentPath)
	}
	current := canonical
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("document path unavailable or symbolic link: %w", ErrInvalidDocumentPath)
		}
		if i < len(parts)-1 && !info.IsDir() || i == len(parts)-1 && !info.Mode().IsRegular() {
			return "", fmt.Errorf("document path is not a regular file: %w", ErrInvalidDocumentPath)
		}
	}
	return current, nil
}

func validRelativeDocumentPath(relative string) bool {
	if len(relative) == 0 || len(relative) > MaxRelativePathBytes || !utf8.ValidString(relative) ||
		strings.ContainsAny(relative, "\\\x00\n\r") || strings.Contains(relative, ":") ||
		path.IsAbs(relative) || path.Clean(relative) != relative || relative == "." {
		return false
	}
	for _, part := range strings.Split(relative, "/") {
		if part == "." || part == ".." || part == "" {
			return false
		}
	}
	return true
}

func verifyOpenPath(file *os.File, rootHandle *os.Root, project ResolvedProject, relative, filename string) error {
	if err := verifyRegisteredRoot(rootHandle, project); err != nil {
		return err
	}
	resolved, err := resolveDocumentPath(project.CanonicalRoot, relative)
	if err != nil || resolved != filename {
		return ErrInvalidDocumentPath
	}
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	current, err := rootHandle.Lstat(filepath.FromSlash(relative))
	if err != nil || !os.SameFile(opened, current) {
		return ErrDocumentConflict
	}
	return nil
}

func validateDocument(doc *Document) {
	doc.SemanticVersion = ""
	doc.Diagnostics = nil
	definition, err := graph.Parse([]byte(doc.Text))
	if err != nil {
		doc.Validity = DocumentInvalid
		doc.Diagnostics = []graph.Diagnostic{{Severity: graph.SeverityError,
			Code: "E_GRAPH_PARSE", Message: "Graph YAML cannot be parsed",
			Suggestion: "Correct YAML syntax and the schema_version field"}}
		return
	}
	result := graph.Validate(definition)
	doc.Diagnostics = result.Diagnostics
	if result.HasErrors() {
		doc.Validity = DocumentInvalid
		return
	}
	version, err := graph.SemanticVersion(definition)
	if err != nil {
		doc.Validity = DocumentInvalid
		doc.Diagnostics = []graph.Diagnostic{{Severity: graph.SeverityError,
			Code: "E_GRAPH_VERSION", Message: "Graph semantic version cannot be computed"}}
		return
	}
	doc.Validity = DocumentValid
	doc.SemanticVersion = version
}

func cloneDocument(doc *Document) Document {
	clone := *doc
	clone.Diagnostics = append([]graph.Diagnostic(nil), doc.Diagnostics...)
	for i := range clone.Diagnostics {
		original := doc.Diagnostics[i].Location
		if original.EdgeIndex != nil {
			value := *original.EdgeIndex
			clone.Diagnostics[i].Location.EdgeIndex = &value
		}
		if original.GraphInputBindingIndex != nil {
			value := *original.GraphInputBindingIndex
			clone.Diagnostics[i].Location.GraphInputBindingIndex = &value
		}
	}
	return clone
}

func digestBytes(data []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}

func randomDocumentID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
