package desktop

import (
	"bytes"
	"os"
	"strings"
)

// SavedDocumentSnapshot is detached from the editable buffer and represents
// exact bytes still present at the pinned local file when it was checked.
// Start must verify these identities and bytes again before execution.
type SavedDocumentSnapshot struct {
	DocumentID      string
	ProjectID       string
	RelativePath    string
	Revision        uint64
	DiskDigest      string
	RootIdentity    ProjectIdentity
	FileIdentity    ProjectIdentity
	Text            string
	SemanticVersion string
}

// Get returns the current editable buffer, including invalid or unsaved drafts.
func (s *DocumentStore) Get(documentID string, expectedRevision uint64) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.byID[documentID]
	if !ok {
		return Document{}, ErrDocumentNotFound
	}
	if doc.Revision != expectedRevision {
		return Document{}, ErrDocumentConflict
	}
	return cloneDocument(doc), nil
}

// SavedSnapshot confirms an exact valid buffer is the currently saved file.
// It does not authorize Start by itself; disk and tool state can change later.
func (s *DocumentStore) SavedSnapshot(documentID string, expectedRevision uint64) (SavedDocumentSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.byID[documentID]
	if !ok {
		return SavedDocumentSnapshot{}, ErrDocumentNotFound
	}
	if doc.Revision != expectedRevision {
		return SavedDocumentSnapshot{}, ErrDocumentConflict
	}
	if doc.Validity != DocumentValid || doc.SemanticVersion == "" {
		return SavedDocumentSnapshot{}, ErrDocumentInvalid
	}
	if digestBytes([]byte(doc.Text)) != doc.DiskDigest {
		return SavedDocumentSnapshot{}, ErrDocumentUnsaved
	}
	if err := verifyStateDir(s.stateDir); err != nil {
		return SavedDocumentSnapshot{}, err
	}
	entries, err := os.ReadDir(s.stateDir)
	if err != nil {
		return SavedDocumentSnapshot{}, ErrDocumentSaveUnavailable
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "save-") && strings.HasSuffix(entry.Name(), ".intent") {
			return SavedDocumentSnapshot{}, ErrDocumentSaveInDoubt
		}
	}
	project, err := s.resolver.ResolveProject(doc.ProjectID)
	if err != nil {
		return SavedDocumentSnapshot{}, err
	}
	if project.Identity != doc.rootIdentity {
		return SavedDocumentSnapshot{}, ErrDocumentConflict
	}
	root, err := openRegisteredRoot(project)
	if err != nil {
		return SavedDocumentSnapshot{}, err
	}
	defer root.Close()
	filename, err := resolveDocumentPath(project.CanonicalRoot, doc.RelativePath)
	if err != nil {
		return SavedDocumentSnapshot{}, err
	}
	if filename != doc.path {
		return SavedDocumentSnapshot{}, ErrDocumentConflict
	}
	file, err := openRegularDocument(root, doc.RelativePath)
	if err != nil {
		return SavedDocumentSnapshot{}, err
	}
	defer file.Close()
	data, err := readBoundedDocument(file)
	if err != nil {
		return SavedDocumentSnapshot{}, err
	}
	if err := verifyOpenPath(file, root, project, doc.RelativePath, filename); err != nil {
		return SavedDocumentSnapshot{}, err
	}
	identity, err := identityOfFile(file)
	if err != nil {
		return SavedDocumentSnapshot{}, err
	}
	if identity != doc.fileIdentity || !bytes.Equal(data, []byte(doc.Text)) || digestBytes(data) != doc.DiskDigest {
		return SavedDocumentSnapshot{}, ErrDocumentConflict
	}
	return SavedDocumentSnapshot{DocumentID: doc.DocumentID, ProjectID: doc.ProjectID,
		RelativePath: doc.RelativePath, Revision: doc.Revision, DiskDigest: doc.DiskDigest,
		RootIdentity: project.Identity, FileIdentity: identity, Text: string(data),
		SemanticVersion: doc.SemanticVersion}, nil
}
