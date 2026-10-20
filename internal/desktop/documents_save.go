package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var ErrDocumentSaveUnavailable = errors.New("safe document save unavailable")
var ErrDocumentSaveInDoubt = errors.New("document save requires recovery")

type saveIntent struct {
	Version           int             `json:"version"`
	ProjectID         string          `json:"project_id"`
	RelativePath      string          `json:"relative_path"`
	RootIdentity      ProjectIdentity `json:"root_identity"`
	Candidate         string          `json:"candidate"`
	Draft             string          `json:"draft"`
	OriginalIdentity  ProjectIdentity `json:"original_identity"`
	CandidateIdentity ProjectIdentity `json:"candidate_identity"`
	ExpectedDigest    string          `json:"expected_digest"`
	CandidateDigest   string          `json:"candidate_digest"`
	journalPath       string
}

type SaveConflict struct {
	ProjectID    string
	RelativePath string
	DraftName    string
}

type RecoveryReport struct {
	Resolved  int
	Conflicts []SaveConflict
}

// Save preserves the document's exact text. It never silently overwrites a
// change made since Open. A durable intent protects the crash window around
// exchange; a concurrent edit may briefly observe the candidate at the target.
func (s *DocumentStore) Save(documentID string, expectedRevision uint64, expectedDiskDigest string, _ bool) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !documentSwapAvailable {
		return Document{}, ErrDocumentSaveUnavailable
	}
	doc, ok := s.byID[documentID]
	if !ok {
		return Document{}, ErrDocumentNotFound
	}
	if doc.Revision != expectedRevision || doc.DiskDigest != expectedDiskDigest {
		return Document{}, ErrDocumentConflict
	}
	if err := verifyStateDir(s.stateDir); err != nil {
		return Document{}, err
	}
	project, err := s.resolver.ResolveProject(doc.ProjectID)
	if err != nil {
		return Document{}, err
	}
	root, err := openRegisteredRoot(project)
	if err != nil {
		return Document{}, err
	}
	defer root.Close()
	if _, err := resolveDocumentPath(project.CanonicalRoot, doc.RelativePath); err != nil {
		return Document{}, err
	}
	parent, err := root.OpenRoot(filepath.FromSlash(path.Dir(doc.RelativePath)))
	if err != nil {
		return Document{}, err
	}
	defer parent.Close()
	if err := verifyDocumentParent(root, parent, doc.RelativePath); err != nil {
		return Document{}, err
	}
	targetName := path.Base(doc.RelativePath)
	original, err := openRegularDocument(parent, targetName)
	if err != nil {
		return Document{}, err
	}
	defer original.Close()
	originalIdentity, err := identityOfFile(original)
	if err != nil {
		return Document{}, err
	}
	if originalIdentity != doc.fileIdentity || project.Identity != doc.rootIdentity {
		return Document{}, ErrDocumentConflict
	}
	currentBytes, err := readBoundedDocument(original)
	if err != nil {
		return Document{}, err
	}
	if digestBytes(currentBytes) != expectedDiskDigest {
		return Document{}, ErrDocumentConflict
	}
	originalInfo, err := original.Stat()
	if err != nil {
		return Document{}, err
	}
	id, err := randomDocumentID()
	if err != nil {
		return Document{}, err
	}
	base := ".agentplane-save-" + id
	intent := saveIntent{Version: 1, ProjectID: doc.ProjectID, RelativePath: doc.RelativePath,
		RootIdentity: project.Identity, Candidate: base + ".candidate", Draft: base + ".draft",
		OriginalIdentity: originalIdentity, ExpectedDigest: expectedDiskDigest,
		CandidateDigest: digestBytes([]byte(doc.Text)), journalPath: filepath.Join(s.stateDir, "save-"+id+".intent")}
	writer := writeCandidate
	if s.candidateWriter != nil {
		writer = s.candidateWriter
	}
	if err := writer(parent, intent.Candidate, []byte(doc.Text), originalInfo.Mode().Perm()); err != nil {
		cleanupUnjournaledSave(parent, intent)
		return Document{}, err
	}
	if err := writer(parent, intent.Draft, []byte(doc.Text), 0600); err != nil {
		cleanupUnjournaledSave(parent, intent)
		return Document{}, err
	}
	candidate, err := openRegularDocument(parent, intent.Candidate)
	if err != nil {
		cleanupUnjournaledSave(parent, intent)
		return Document{}, err
	}
	defer candidate.Close()
	intent.CandidateIdentity, err = identityOfFile(candidate)
	if err != nil {
		cleanupUnjournaledSave(parent, intent)
		return Document{}, err
	}
	if err := syncDirectory(parent); err != nil {
		cleanupUnjournaledSave(parent, intent)
		return Document{}, err
	}
	if err := writeSaveIntent(s.stateDir, intent); err != nil {
		if _, statErr := os.Lstat(intent.journalPath); errors.Is(statErr, os.ErrNotExist) {
			cleanupUnjournaledSave(parent, intent)
		}
		return Document{}, err
	}
	// A failure after intent creation leaves a recoverable candidate and draft.
	if s.beforeExchange != nil {
		if err := s.beforeExchange(); err != nil {
			return Document{}, err
		}
	}
	if err := verifyRegisteredRoot(root, project); err != nil {
		return Document{}, err
	}
	if err := verifyDocumentParent(root, parent, doc.RelativePath); err != nil {
		return Document{}, err
	}
	if err := verifyCandidate(parent, intent); err != nil {
		return Document{}, err
	}
	if err := swapDocumentEntries(parent, intent.Candidate, targetName); err != nil {
		return Document{}, fmt.Errorf("exchange graph document: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return Document{}, ErrDocumentSaveInDoubt
	}
	if s.afterExchange != nil {
		if err := s.afterExchange(); err != nil {
			return Document{}, err
		}
	}
	resolved, err := inspectSwapped(parent, targetName, intent)
	if err != nil {
		if restoreErr := restoreOriginalIfDisplaced(parent, targetName, intent); restoreErr != nil {
			return Document{}, fmt.Errorf("inspect exchanged document: %w: %v", ErrDocumentSaveInDoubt, err)
		}
		return Document{}, ErrDocumentConflict
	}
	if !resolved {
		if s.beforeRestore != nil {
			if err := s.beforeRestore(); err != nil {
				return Document{}, err
			}
		}
		if err := restoreIfCandidate(parent, targetName, intent); err != nil {
			return Document{}, fmt.Errorf("save conflict recovery: %w: %v", ErrDocumentSaveInDoubt, err)
		}
		return Document{}, ErrDocumentConflict
	}
	if err := verifyRegisteredRoot(root, project); err != nil {
		return Document{}, ErrDocumentSaveInDoubt
	}
	if err := verifyDocumentParent(root, parent, doc.RelativePath); err != nil {
		return Document{}, ErrDocumentSaveInDoubt
	}
	if final, err := inspectSwapped(parent, targetName, intent); err != nil || !final {
		return Document{}, ErrDocumentSaveInDoubt
	}
	if err := removeSaveIntent(intent.journalPath); err != nil {
		return Document{}, ErrDocumentSaveInDoubt
	}
	_ = parent.Remove(intent.Candidate)
	_ = parent.Remove(intent.Draft)
	_ = syncDirectory(parent)
	doc.DiskDigest = intent.CandidateDigest
	doc.fileIdentity = intent.CandidateIdentity
	return cloneDocument(doc), nil
}

func verifyCandidate(parent *os.Root, intent saveIntent) error {
	candidate, err := openRegularDocument(parent, intent.Candidate)
	if err != nil {
		return ErrDocumentConflict
	}
	defer candidate.Close()
	identity, err := identityOfFile(candidate)
	if err != nil || identity != intent.CandidateIdentity {
		return ErrDocumentConflict
	}
	data, err := readBoundedDocument(candidate)
	if err != nil || digestBytes(data) != intent.CandidateDigest {
		return ErrDocumentConflict
	}
	return nil
}

func restoreOriginalIfDisplaced(parent *os.Root, targetName string, intent saveIntent) error {
	displaced, err := openRegularDocument(parent, intent.Candidate)
	if err != nil {
		return err
	}
	defer displaced.Close()
	identity, err := identityOfFile(displaced)
	if err != nil || identity != intent.OriginalIdentity {
		return ErrDocumentSaveInDoubt
	}
	if err := swapDocumentEntries(parent, targetName, intent.Candidate); err != nil {
		return err
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	return verifyRestoredOriginal(parent, targetName, intent)
}

func verifyRestoredOriginal(parent *os.Root, targetName string, intent saveIntent) error {
	original, err := openRegularDocument(parent, targetName)
	if err != nil {
		return err
	}
	defer original.Close()
	identity, err := identityOfFile(original)
	if err != nil || identity != intent.OriginalIdentity {
		return ErrDocumentSaveInDoubt
	}
	return nil
}

func cleanupUnjournaledSave(parent *os.Root, intent saveIntent) {
	_ = parent.Remove(intent.Candidate)
	_ = parent.Remove(intent.Draft)
	_ = syncDirectory(parent)
}

func verifyDocumentParent(root, parent *os.Root, relative string) error {
	dir, err := parent.Open(".")
	if err != nil {
		return ErrInvalidDocumentPath
	}
	defer dir.Close()
	opened, err := dir.Stat()
	if err != nil || !opened.IsDir() {
		return ErrInvalidDocumentPath
	}
	current, err := root.Lstat(filepath.FromSlash(path.Dir(relative)))
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) {
		return ErrInvalidDocumentPath
	}
	return nil
}

// Recover checks every durable intent before documents are opened. Conflicts
// retain the journal and draft for an explicit user decision.
func (s *DocumentStore) Recover() (RecoveryReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var report RecoveryReport
	if err := verifyStateDir(s.stateDir); err != nil {
		return report, err
	}
	entries, err := os.ReadDir(s.stateDir)
	if err != nil {
		return report, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "save-") || !strings.HasSuffix(entry.Name(), ".intent") {
			continue
		}
		intent, err := readSaveIntent(filepath.Join(s.stateDir, entry.Name()))
		if err != nil {
			return report, err
		}
		resolved, err := s.recoverIntent(intent)
		if err != nil {
			return report, err
		}
		if resolved {
			report.Resolved++
		} else {
			report.Conflicts = append(report.Conflicts, SaveConflict{intent.ProjectID, intent.RelativePath, intent.Draft})
		}
	}
	return report, nil
}

func (s *DocumentStore) recoverIntent(intent saveIntent) (bool, error) {
	project, err := s.resolver.ResolveProject(intent.ProjectID)
	if err != nil || project.Identity != intent.RootIdentity {
		return false, nil
	}
	root, err := openRegisteredRoot(project)
	if err != nil {
		return false, nil
	}
	defer root.Close()
	parent, err := root.OpenRoot(filepath.FromSlash(path.Dir(intent.RelativePath)))
	if err != nil {
		return false, nil
	}
	defer parent.Close()
	if err := verifyDocumentParent(root, parent, intent.RelativePath); err != nil {
		return false, nil
	}
	targetName := path.Base(intent.RelativePath)
	resolved, err := inspectSwapped(parent, targetName, intent)
	if err != nil {
		_ = restoreOriginalIfDisplaced(parent, targetName, intent)
		return false, nil
	}
	if resolved {
		if s.beforeRecoveryCommit != nil {
			if err := s.beforeRecoveryCommit(); err != nil {
				return false, err
			}
		}
		if err := verifyRegisteredRoot(root, project); err != nil {
			return false, nil
		}
		if err := verifyDocumentParent(root, parent, intent.RelativePath); err != nil {
			return false, nil
		}
		if final, err := inspectSwapped(parent, targetName, intent); err != nil || !final {
			return false, nil
		}
		if err := removeSaveIntent(intent.journalPath); err != nil {
			return false, err
		}
		_ = parent.Remove(intent.Candidate)
		_ = parent.Remove(intent.Draft)
		_ = syncDirectory(parent)
		return true, nil
	}
	if err := restoreIfCandidate(parent, targetName, intent); err != nil {
		return false, nil // preserve all paths; an operator must resolve them
	}
	return false, nil
}

func inspectSwapped(parent *os.Root, targetName string, intent saveIntent) (bool, error) {
	target, err := openRegularDocument(parent, targetName)
	if err != nil {
		return false, err
	}
	defer target.Close()
	targetID, err := identityOfFile(target)
	if err != nil || targetID != intent.CandidateIdentity {
		return false, err
	}
	targetBytes, err := readBoundedDocument(target)
	if err != nil || digestBytes(targetBytes) != intent.CandidateDigest {
		return false, err
	}
	displaced, err := openRegularDocument(parent, intent.Candidate)
	if err != nil {
		return false, err
	}
	defer displaced.Close()
	displacedID, err := identityOfFile(displaced)
	if err != nil || displacedID != intent.OriginalIdentity {
		return false, err
	}
	displacedBytes, err := readBoundedDocument(displaced)
	if err != nil {
		return false, err
	}
	return digestBytes(displacedBytes) == intent.ExpectedDigest, nil
}

func restoreIfCandidate(parent *os.Root, targetName string, intent saveIntent) error {
	target, err := openRegularDocument(parent, targetName)
	if err != nil {
		return err
	}
	defer target.Close()
	identity, err := identityOfFile(target)
	if err != nil {
		return err
	}
	if identity != intent.CandidateIdentity {
		return ErrDocumentSaveInDoubt
	}
	displaced, err := openRegularDocument(parent, intent.Candidate)
	if err != nil {
		return err
	}
	defer displaced.Close()
	displacedIdentity, err := identityOfFile(displaced)
	if err != nil || displacedIdentity != intent.OriginalIdentity {
		return ErrDocumentSaveInDoubt
	}
	if err := swapDocumentEntries(parent, targetName, intent.Candidate); err != nil {
		return err
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	return verifyRestoredOriginal(parent, targetName, intent)
}

func readBoundedDocument(file *os.File) ([]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxDocumentBytes {
		return nil, ErrDocumentTooLarge
	}
	return data, nil
}

func writeCandidate(parent *os.Root, name string, data []byte, mode os.FileMode) error {
	file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = parent.Remove(name)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		_ = parent.Remove(name)
		return err
	}
	if err := file.Close(); err != nil {
		_ = parent.Remove(name)
		return err
	}
	return nil
}

func verifyStateDir(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return ErrDocumentSaveUnavailable
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrDocumentSaveUnavailable
	}
	return nil
}

func writeSaveIntent(dir string, intent saveIntent) error {
	tmp, err := os.CreateTemp(dir, ".save-intent-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := json.NewEncoder(tmp).Encode(intent); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), intent.journalPath); err != nil {
		return err
	}
	return syncPath(dir)
}

func readSaveIntent(filename string) (saveIntent, error) {
	var intent saveIntent
	root, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return intent, err
	}
	defer root.Close()
	file, err := openRegularDocument(root, filepath.Base(filename))
	if err != nil {
		return intent, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 4096 || info.Mode().Perm()&0077 != 0 {
		return intent, ErrDocumentSaveInDoubt
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return intent, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return intent, ErrDocumentSaveInDoubt
	}
	name := filepath.Base(filename)
	id := strings.TrimSuffix(strings.TrimPrefix(name, "save-"), ".intent")
	if intent.Version != 1 || !validRelativeDocumentPath(intent.RelativePath) ||
		!validSaveID(id) || intent.Candidate != ".agentplane-save-"+id+".candidate" ||
		intent.Draft != ".agentplane-save-"+id+".draft" ||
		intent.ProjectID == "" || intent.RootIdentity == "" || intent.CandidateIdentity == "" ||
		intent.OriginalIdentity == "" || !validDigest(intent.ExpectedDigest) || !validDigest(intent.CandidateDigest) {
		return intent, ErrDocumentSaveInDoubt
	}
	intent.journalPath = filename
	return intent, nil
}

func validSaveID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil
}

func removeSaveIntent(filename string) error {
	if err := os.Remove(filename); err != nil {
		return err
	}
	return syncPath(filepath.Dir(filename))
}

func syncDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func syncPath(dirname string) error {
	dir, err := os.Open(dirname)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
