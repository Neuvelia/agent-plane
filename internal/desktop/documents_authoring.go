package desktop

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var ErrDocumentCreateUnavailable = errors.New("safe graph creation unavailable")
var ErrInvalidGraphID = errors.New("invalid graph ID")

// Create publishes complete graph bytes with an exclusive same-directory link.
// A crash before publication leaves only an unreferenced temporary file; a
// crash after publication leaves a valid graph that Open can recover normally.
func (s *DocumentStore) Create(projectID, relativePath, graphID string) (Document, error) {
	if !documentCreateAvailable {
		return Document{}, ErrDocumentCreateUnavailable
	}
	if !validRelativeDocumentPath(relativePath) {
		return Document{}, ErrInvalidDocumentPath
	}
	if !validGraphIdentifier(graphID) {
		return Document{}, ErrInvalidGraphID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := projectID + "\x00" + relativePath
	if _, exists := s.byPath[key]; exists {
		return Document{}, ErrDocumentConflict
	}
	project, err := s.resolver.ResolveProject(projectID)
	if err != nil {
		return Document{}, fmt.Errorf("resolve project: %w", err)
	}
	root, err := openRegisteredRoot(project)
	if err != nil {
		return Document{}, err
	}
	defer root.Close()
	if err := verifyCreateParentPath(root, relativePath); err != nil {
		return Document{}, err
	}
	parent, err := root.OpenRoot(filepath.FromSlash(path.Dir(relativePath)))
	if err != nil {
		return Document{}, ErrInvalidDocumentPath
	}
	defer parent.Close()
	if err := verifyDocumentParent(root, parent, relativePath); err != nil {
		return Document{}, err
	}
	name := path.Base(relativePath)
	if _, err := parent.Lstat(name); err == nil {
		return Document{}, ErrDocumentConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return Document{}, ErrInvalidDocumentPath
	}
	id, err := randomDocumentID()
	if err != nil {
		return Document{}, err
	}
	data := []byte(fmt.Sprintf("schema_version: 0\nid: %s\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n", graphID))
	if len(data) > MaxDocumentBytes {
		return Document{}, ErrDocumentTooLarge
	}
	temp := ".agentplane-create-" + id + ".tmp"
	if err := writeCandidate(parent, temp, data, 0600); err != nil {
		return Document{}, err
	}
	defer parent.Remove(temp)
	if err := syncDirectory(parent); err != nil {
		return Document{}, err
	}
	candidate, err := openRegularDocument(parent, temp)
	if err != nil {
		return Document{}, err
	}
	defer candidate.Close()
	candidateIdentity, err := identityOfFile(candidate)
	if err != nil {
		return Document{}, err
	}
	candidateBytes, err := readBoundedDocument(candidate)
	if err != nil || string(candidateBytes) != string(data) {
		return Document{}, ErrDocumentConflict
	}
	if err := verifyRegisteredRoot(root, project); err != nil {
		return Document{}, err
	}
	if err := verifyDocumentParent(root, parent, relativePath); err != nil {
		return Document{}, err
	}
	if err := linkDocumentExclusive(parent, candidate, temp, name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Document{}, ErrDocumentConflict
		}
		return Document{}, err
	}
	if err := syncDirectory(parent); err != nil {
		return Document{}, ErrDocumentSaveInDoubt
	}
	if s.afterCreatePublish != nil {
		if err := s.afterCreatePublish(); err != nil {
			return Document{}, err
		}
	}
	file, err := openRegularDocument(root, relativePath)
	if err != nil {
		return Document{}, err
	}
	defer file.Close()
	filename := filepath.Join(project.CanonicalRoot, filepath.FromSlash(relativePath))
	if err := verifyOpenPath(file, root, project, relativePath, filename); err != nil {
		return Document{}, err
	}
	read, err := readBoundedDocument(file)
	if err != nil || string(read) != string(data) {
		return Document{}, ErrDocumentConflict
	}
	identity, err := identityOfFile(file)
	if err != nil {
		return Document{}, err
	}
	if identity != candidateIdentity {
		return Document{}, ErrDocumentConflict
	}
	doc := &Document{DocumentID: id, ProjectID: projectID, RelativePath: relativePath,
		Text: string(data), Revision: 1, DiskDigest: digestBytes(data), path: filename,
		rootIdentity: project.Identity, fileIdentity: identity}
	validateDocument(doc)
	s.byID[id], s.byPath[key] = doc, id
	return cloneDocument(doc), nil
}

// Reload is the only operation that rebases a buffer onto external file bytes.
// A changed regular-file identity is accepted only at the same pinned path.
func (s *DocumentStore) Reload(documentID string, expectedRevision uint64, discardDraft bool) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.byID[documentID]
	if !ok {
		return Document{}, ErrDocumentNotFound
	}
	if expectedRevision != doc.Revision || doc.Revision == ^uint64(0) {
		return Document{}, ErrDocumentConflict
	}
	if !discardDraft && digestBytes([]byte(doc.Text)) != doc.DiskDigest {
		return Document{}, ErrDocumentUnsaved
	}
	project, err := s.resolver.ResolveProject(doc.ProjectID)
	if err != nil {
		return Document{}, err
	}
	if project.Identity != doc.rootIdentity {
		return Document{}, ErrDocumentConflict
	}
	root, err := openRegisteredRoot(project)
	if err != nil {
		return Document{}, err
	}
	defer root.Close()
	filename, err := resolveDocumentPath(project.CanonicalRoot, doc.RelativePath)
	if err != nil {
		return Document{}, err
	}
	if filename != doc.path {
		return Document{}, ErrDocumentConflict
	}
	file, err := openRegularDocument(root, doc.RelativePath)
	if err != nil {
		return Document{}, err
	}
	defer file.Close()
	data, err := readBoundedDocument(file)
	if err != nil {
		return Document{}, err
	}
	if !utf8.Valid(data) {
		return Document{}, ErrDocumentInvalidEncoding
	}
	if err := verifyOpenPath(file, root, project, doc.RelativePath, filename); err != nil {
		return Document{}, err
	}
	identity, err := identityOfFile(file)
	if err != nil {
		return Document{}, err
	}
	doc.Text, doc.DiskDigest, doc.fileIdentity = string(data), digestBytes(data), identity
	doc.Revision++
	validateDocument(doc)
	return cloneDocument(doc), nil
}

func verifyCreateParentPath(root *os.Root, relative string) error {
	parts := strings.Split(path.Dir(relative), "/")
	current := ""
	for _, part := range parts {
		if part == "." {
			continue
		}
		current = path.Join(current, part)
		info, err := root.Lstat(filepath.FromSlash(current))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidDocumentPath
		}
	}
	return nil
}

func validGraphIdentifier(id string) bool {
	if id == "" || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for _, ch := range id[1:] {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}
