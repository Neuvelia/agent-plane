package desktop

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxRegistryBytes = 1 << 20
const grantLifetime = time.Minute

var ErrRegistryConflict = errors.New("local registry conflict")
var ErrRegistryUnavailable = errors.New("local registry unavailable")
var ErrRegistryInDoubt = errors.New("local registry commit requires restart")
var ErrRegistryNotFound = errors.New("local registry item not found")

type Availability string

const (
	AvailabilityReady        Availability = "ready"
	AvailabilityMissing      Availability = "missing"
	AvailabilityInaccessible Availability = "inaccessible"
)

type ProjectRecord struct {
	ProjectID     string          `json:"project_id"`
	DisplayName   string          `json:"display_name"`
	CanonicalPath string          `json:"canonical_path"`
	Identity      ProjectIdentity `json:"identity"`
	Availability  Availability    `json:"-"`
}

type WorkspaceRecord struct {
	WorkspaceID  string          `json:"workspace_id"`
	ProjectID    string          `json:"project_id"`
	RelativePath string          `json:"relative_path"`
	Identity     ProjectIdentity `json:"identity"`
	Availability Availability    `json:"-"`
}

// ResolvedWorkspace is a current, identity-checked view of a registered local
// workspace. Callers must resolve it again before starting execution because
// an external process can change filesystem entries after this check.
type ResolvedWorkspace struct {
	WorkspaceID   string
	ProjectID     string
	CanonicalPath string
	Identity      ProjectIdentity
}

type ProjectGrant struct {
	Token      string `json:"grant_token"`
	PathDigest string `json:"path_digest"`
}

type storedGrant struct {
	project ResolvedProject
	digest  string
	expires time.Time
}

type registryState struct {
	Version    int               `json:"version"`
	Projects   []ProjectRecord   `json:"projects"`
	Workspaces []WorkspaceRecord `json:"workspaces"`
	Settings   LocalSettings     `json:"settings"`
}

type Registry struct {
	mu       sync.Mutex
	stateDir string
	root     *os.Root
	rootID   ProjectIdentity
	lock     registryLock
	state    registryState
	grants   map[string]storedGrant
	now      func() time.Time
	inDoubt  bool
	closed   bool
	// Injected only by package tests after replacement, before directory sync.
	beforeDirectorySync func() error
	beforeWorkspaceOpen func() error // deterministic path race seam for package tests
}

func NewRegistry(stateDir string) (*Registry, error) {
	if !registryReplaceAvailable {
		return nil, ErrRegistryUnavailable
	}
	if err := verifyStateDir(stateDir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(stateDir)
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	rootID, err := identityOfFile(dir)
	dir.Close()
	if err != nil {
		root.Close()
		return nil, err
	}
	lock, err := acquireRegistryLock(stateDir)
	if err != nil {
		root.Close()
		return nil, err
	}
	r := &Registry{stateDir: stateDir, root: root, rootID: rootID, lock: lock,
		grants: make(map[string]storedGrant), now: time.Now}
	if err := r.verifyStatePath(); err != nil {
		r.Close()
		return nil, err
	}
	state, err := readRegistryState(root)
	if err != nil {
		r.Close()
		return nil, err
	}
	r.state = state
	return r, nil
}

func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	if r.lock != nil {
		errs = append(errs, r.lock.Close())
	}
	if r.root != nil {
		errs = append(errs, r.root.Close())
	}
	return errors.Join(errs...)
}

func (r *Registry) checkReady() error {
	if r.closed {
		return ErrRegistryUnavailable
	}
	if r.inDoubt {
		return ErrRegistryInDoubt
	}
	if err := r.lock.Verify(); err != nil {
		r.inDoubt = true
		return ErrRegistryInDoubt
	}
	return r.verifyStatePath()
}

func (r *Registry) verifyStatePath() error {
	dir, err := r.root.Open(".")
	if err != nil {
		return ErrRegistryUnavailable
	}
	defer dir.Close()
	identity, err := identityOfFile(dir)
	if err != nil || identity != r.rootID {
		return ErrRegistryUnavailable
	}
	current, err := os.Lstat(r.stateDir)
	opened, statErr := dir.Stat()
	if err != nil || statErr != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 ||
		current.Mode().Perm()&0077 != 0 || !os.SameFile(opened, current) {
		return ErrRegistryUnavailable
	}
	return nil
}

func (r *Registry) GrantProjectFolder(selected string) (ProjectGrant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return ProjectGrant{}, err
	}
	project, err := RegisterProjectRoot(selected)
	if err != nil {
		return ProjectGrant{}, err
	}
	if !validProjectPath(project.CanonicalRoot) || !utf8.ValidString(filepath.Base(project.CanonicalRoot)) {
		return ProjectGrant{}, ErrInvalidDocumentPath
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return ProjectGrant{}, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	digest := pathDigest(project.CanonicalRoot)
	for key, grant := range r.grants {
		if !r.now().Before(grant.expires) {
			delete(r.grants, key)
		}
	}
	if len(r.grants) >= 64 {
		return ProjectGrant{}, ErrRegistryBusy
	}
	r.grants[token] = storedGrant{project: project, digest: digest, expires: r.now().Add(grantLifetime)}
	return ProjectGrant{Token: token, PathDigest: digest}, nil
}

func (r *Registry) AddProject(token, digest string) (ProjectRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return ProjectRecord{}, err
	}
	grant, ok := r.grants[token]
	delete(r.grants, token)
	if !ok || !r.now().Before(grant.expires) || digest != grant.digest {
		return ProjectRecord{}, ErrRegistryConflict
	}
	current, err := RegisterProjectRoot(grant.project.CanonicalRoot)
	if err != nil || current != grant.project {
		return ProjectRecord{}, ErrRegistryConflict
	}
	for _, project := range r.state.Projects {
		if project.Identity == current.Identity || project.CanonicalPath == current.CanonicalRoot {
			return ProjectRecord{}, ErrRegistryConflict
		}
	}
	id, err := randomDocumentID()
	if err != nil {
		return ProjectRecord{}, err
	}
	project := ProjectRecord{ProjectID: id, DisplayName: filepath.Base(current.CanonicalRoot),
		CanonicalPath: current.CanonicalRoot, Identity: current.Identity, Availability: AvailabilityReady}
	next := r.state
	next.Projects = append(append([]ProjectRecord(nil), next.Projects...), project)
	if err := r.commit(next); err != nil {
		return ProjectRecord{}, err
	}
	return project, nil
}

func (r *Registry) ResolveProject(projectID string) (ResolvedProject, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return ResolvedProject{}, err
	}
	for _, project := range r.state.Projects {
		if project.ProjectID == projectID {
			resolved := ResolvedProject{CanonicalRoot: project.CanonicalPath, Identity: project.Identity}
			root, err := openRegisteredRoot(resolved)
			if err != nil {
				return ResolvedProject{}, ErrRegistryUnavailable
			}
			root.Close()
			return resolved, nil
		}
	}
	return ResolvedProject{}, ErrRegistryNotFound
}

func (r *Registry) ResolveWorkspace(workspaceID string) (ResolvedWorkspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return ResolvedWorkspace{}, err
	}
	var workspace *WorkspaceRecord
	for i := range r.state.Workspaces {
		if r.state.Workspaces[i].WorkspaceID == workspaceID {
			workspace = &r.state.Workspaces[i]
			break
		}
	}
	if workspace == nil {
		return ResolvedWorkspace{}, ErrRegistryNotFound
	}
	var project *ProjectRecord
	for i := range r.state.Projects {
		if r.state.Projects[i].ProjectID == workspace.ProjectID {
			project = &r.state.Projects[i]
			break
		}
	}
	if project == nil || !validWorkspaceRelative(workspace.RelativePath) {
		return ResolvedWorkspace{}, ErrRegistryUnavailable
	}
	registered := ResolvedProject{CanonicalRoot: project.CanonicalPath, Identity: project.Identity}
	root, err := openRegisteredRoot(registered)
	if err != nil {
		return ResolvedWorkspace{}, ErrRegistryUnavailable
	}
	defer root.Close()
	identity, err := workspaceIdentity(root, registered, workspace.RelativePath, r.beforeWorkspaceOpen)
	if err != nil || identity != workspace.Identity {
		return ResolvedWorkspace{}, ErrRegistryUnavailable
	}
	canonical := registered.CanonicalRoot
	if workspace.RelativePath != "." {
		canonical = filepath.Join(canonical, filepath.FromSlash(workspace.RelativePath))
	}
	return ResolvedWorkspace{WorkspaceID: workspace.WorkspaceID, ProjectID: workspace.ProjectID,
		CanonicalPath: canonical, Identity: identity}, nil
}

func (r *Registry) ListProjects() ([]ProjectRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return nil, err
	}
	projects := append([]ProjectRecord(nil), r.state.Projects...)
	for i := range projects {
		projects[i].Availability = projectAvailability(projects[i])
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].DisplayName != projects[j].DisplayName {
			return projects[i].DisplayName < projects[j].DisplayName
		}
		return projects[i].ProjectID < projects[j].ProjectID
	})
	return projects, nil
}

func projectAvailability(project ProjectRecord) Availability {
	resolved := ResolvedProject{CanonicalRoot: project.CanonicalPath, Identity: project.Identity}
	root, err := openRegisteredRoot(resolved)
	if err == nil {
		root.Close()
		return AvailabilityReady
	}
	if _, statErr := os.Lstat(project.CanonicalPath); errors.Is(statErr, os.ErrNotExist) {
		return AvailabilityMissing
	}
	return AvailabilityInaccessible
}

func (r *Registry) AddWorkspace(projectID, relative string) (WorkspaceRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return WorkspaceRecord{}, err
	}
	var registered *ProjectRecord
	for i := range r.state.Projects {
		if r.state.Projects[i].ProjectID == projectID {
			registered = &r.state.Projects[i]
			break
		}
	}
	if registered == nil {
		return WorkspaceRecord{}, ErrRegistryNotFound
	}
	if !validWorkspaceRelative(relative) {
		return WorkspaceRecord{}, ErrInvalidDocumentPath
	}
	project := ResolvedProject{CanonicalRoot: registered.CanonicalPath, Identity: registered.Identity}
	root, err := openRegisteredRoot(project)
	if err != nil {
		return WorkspaceRecord{}, ErrRegistryUnavailable
	}
	defer root.Close()
	identity, err := workspaceIdentity(root, project, relative, r.beforeWorkspaceOpen)
	if err != nil {
		return WorkspaceRecord{}, err
	}
	for _, workspace := range r.state.Workspaces {
		if workspace.ProjectID == projectID && (workspace.Identity == identity || workspace.RelativePath == relative) {
			return WorkspaceRecord{}, ErrRegistryConflict
		}
	}
	id, err := randomDocumentID()
	if err != nil {
		return WorkspaceRecord{}, err
	}
	workspace := WorkspaceRecord{WorkspaceID: id, ProjectID: projectID, RelativePath: relative,
		Identity: identity, Availability: AvailabilityReady}
	next := r.state
	next.Workspaces = append(append([]WorkspaceRecord(nil), next.Workspaces...), workspace)
	if err := r.commit(next); err != nil {
		return WorkspaceRecord{}, err
	}
	return workspace, nil
}

func (r *Registry) ListWorkspaces(projectID string) ([]WorkspaceRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return nil, err
	}
	var project *ProjectRecord
	for i := range r.state.Projects {
		if r.state.Projects[i].ProjectID == projectID {
			project = &r.state.Projects[i]
			break
		}
	}
	if project == nil {
		return nil, ErrRegistryNotFound
	}
	workspaces := make([]WorkspaceRecord, 0)
	for _, workspace := range r.state.Workspaces {
		if workspace.ProjectID == projectID {
			workspace.Availability = workspaceAvailability(*project, workspace)
			workspaces = append(workspaces, workspace)
		}
	}
	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].RelativePath != workspaces[j].RelativePath {
			return workspaces[i].RelativePath < workspaces[j].RelativePath
		}
		return workspaces[i].WorkspaceID < workspaces[j].WorkspaceID
	})
	return workspaces, nil
}

func workspaceAvailability(project ProjectRecord, workspace WorkspaceRecord) Availability {
	root, err := openRegisteredRoot(ResolvedProject{CanonicalRoot: project.CanonicalPath, Identity: project.Identity})
	if err != nil {
		return projectAvailability(project)
	}
	defer root.Close()
	identity, err := workspaceIdentity(root, ResolvedProject{CanonicalRoot: project.CanonicalPath, Identity: project.Identity}, workspace.RelativePath, nil)
	if err == nil && identity == workspace.Identity {
		return AvailabilityReady
	}
	if errors.Is(err, os.ErrNotExist) {
		return AvailabilityMissing
	}
	return AvailabilityInaccessible
}

func validWorkspaceRelative(relative string) bool {
	if relative == "." {
		return true
	}
	return validRelativeDocumentPath(relative)
}

func workspaceIdentity(root *os.Root, project ResolvedProject, relative string, beforeOpen func() error) (ProjectIdentity, error) {
	if !validWorkspaceRelative(relative) {
		return "", ErrInvalidDocumentPath
	}
	current := project.CanonicalRoot
	if relative != "." {
		for _, part := range strings.Split(relative, "/") {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", ErrInvalidDocumentPath
			}
		}
	}
	if beforeOpen != nil {
		if err := beforeOpen(); err != nil {
			return "", err
		}
	}
	subroot, err := root.OpenRoot(filepath.FromSlash(relative))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return "", ErrInvalidDocumentPath
	}
	defer subroot.Close()
	dir, err := subroot.Open(".")
	if err != nil {
		return "", err
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil || !info.IsDir() {
		return "", ErrInvalidDocumentPath
	}
	currentInfo, err := os.Lstat(current)
	if err != nil || !os.SameFile(info, currentInfo) {
		return "", ErrInvalidDocumentPath
	}
	if relative != "." {
		current = project.CanonicalRoot
		for _, part := range strings.Split(relative, "/") {
			current = filepath.Join(current, part)
			component, err := os.Lstat(current)
			if err != nil || component.Mode()&os.ModeSymlink != 0 || !component.IsDir() {
				return "", ErrInvalidDocumentPath
			}
		}
	}
	if err := verifyRegisteredRoot(root, project); err != nil {
		return "", err
	}
	return identityOfFile(dir)
}

func (r *Registry) Settings() (LocalSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return LocalSettings{}, err
	}
	return cloneSettings(r.state.Settings), nil
}

func (r *Registry) StateDigest() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return "", err
	}
	data, err := json.Marshal(r.state)
	if err != nil {
		return "", ErrRegistryUnavailable
	}
	return digestBytes(data), nil
}

func (r *Registry) UpdateSettings(expected uint64, patch SettingsPatch) (LocalSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReady(); err != nil {
		return LocalSettings{}, err
	}
	if expected != r.state.Settings.Revision {
		return LocalSettings{}, ErrRegistryConflict
	}
	updated, err := applySettingsPatch(r.state.Settings, patch)
	if err != nil {
		return LocalSettings{}, err
	}
	next := r.state
	next.Settings = updated
	if err := r.commit(next); err != nil {
		return LocalSettings{}, err
	}
	return cloneSettings(updated), nil
}

func (r *Registry) DetectTool(toolID string) (ToolStatus, error) {
	settings, err := r.Settings()
	if err != nil {
		return ToolStatus{}, err
	}
	return detectToolPath(settings, toolID)
}

func (r *Registry) commit(next registryState) error {
	if err := r.checkReady(); err != nil {
		return err
	}
	if err := writeRegistryState(r.root, next, r.beforeDirectorySync); err != nil {
		if errors.Is(err, ErrRegistryInDoubt) {
			if loaded, loadErr := readRegistryState(r.root); loadErr == nil {
				r.state = loaded
			}
			r.inDoubt = true
		}
		return err
	}
	if err := r.lock.Verify(); err != nil {
		r.inDoubt = true
		return ErrRegistryInDoubt
	}
	r.state = next
	return nil
}

func readRegistryState(root *os.Root) (registryState, error) {
	defaultState := registryState{Version: 1, Projects: []ProjectRecord{}, Workspaces: []WorkspaceRecord{}, Settings: defaultSettings()}
	info, err := root.Lstat("registry.json")
	if errors.Is(err, os.ErrNotExist) {
		return defaultState, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxRegistryBytes {
		return registryState{}, ErrRegistryUnavailable
	}
	file, err := openRegularDocument(root, "registry.json")
	if err != nil {
		return registryState{}, ErrRegistryUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRegistryBytes+1))
	if err != nil || len(data) > maxRegistryBytes {
		return registryState{}, ErrRegistryUnavailable
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 ||
		opened.Size() > maxRegistryBytes || !os.SameFile(info, opened) {
		return registryState{}, ErrRegistryUnavailable
	}
	if current, err := root.Lstat("registry.json"); err != nil || !current.Mode().IsRegular() ||
		current.Mode().Perm()&0077 != 0 || !os.SameFile(info, current) {
		return registryState{}, ErrRegistryUnavailable
	}
	return decodeRegistryState(data)
}

func decodeRegistryState(data []byte) (registryState, error) {
	var state registryState
	fields, err := strictObject(data, "version", "projects", "workspaces", "settings")
	if err != nil || requiredField(fields, "version", &state.Version) != nil || state.Version != 1 ||
		requiredField(fields, "projects", &state.Projects) != nil ||
		requiredField(fields, "workspaces", &state.Workspaces) != nil ||
		requiredField(fields, "settings", &state.Settings) != nil {
		return registryState{}, ErrRegistryUnavailable
	}
	if err := validateSettings(&state.Settings); err != nil {
		return registryState{}, ErrRegistryUnavailable
	}
	var rawProjects, rawWorkspaces []json.RawMessage
	if json.Unmarshal(fields["projects"], &rawProjects) != nil || json.Unmarshal(fields["workspaces"], &rawWorkspaces) != nil ||
		len(rawProjects) != len(state.Projects) || len(rawWorkspaces) != len(state.Workspaces) {
		return registryState{}, ErrRegistryUnavailable
	}
	projectIDs := make(map[string]bool)
	projectIdentity := make(map[ProjectIdentity]bool)
	for i, raw := range rawProjects {
		parts, err := strictObject(raw, "project_id", "display_name", "canonical_path", "identity")
		project := state.Projects[i]
		if err != nil || len(parts) != 4 || !validSaveID(project.ProjectID) || projectIDs[project.ProjectID] ||
			project.DisplayName == "" || len(project.DisplayName) > 256 || !utf8.ValidString(project.DisplayName) ||
			!validProjectPath(project.CanonicalPath) ||
			project.Identity == "" || projectIdentity[project.Identity] {
			return registryState{}, ErrRegistryUnavailable
		}
		projectIDs[project.ProjectID] = true
		projectIdentity[project.Identity] = true
	}
	workspaceIDs := make(map[string]bool)
	workspaceKeys := make(map[string]bool)
	for i, raw := range rawWorkspaces {
		parts, err := strictObject(raw, "workspace_id", "project_id", "relative_path", "identity")
		workspace := state.Workspaces[i]
		key := workspace.ProjectID + "\x00" + string(workspace.Identity)
		if err != nil || len(parts) != 4 || !validSaveID(workspace.WorkspaceID) || workspaceIDs[workspace.WorkspaceID] ||
			!projectIDs[workspace.ProjectID] || !validWorkspaceRelative(workspace.RelativePath) ||
			workspace.Identity == "" || workspaceKeys[key] {
			return registryState{}, ErrRegistryUnavailable
		}
		workspaceIDs[workspace.WorkspaceID] = true
		workspaceKeys[key] = true
	}
	settingsFields, err := strictObject(fields["settings"], "revision", "theme", "codex_binary_path", "git_binary_path", "max_concurrency", "history_retention_days", "keep_all_history")
	if err != nil || settingsFields["revision"] == nil || settingsFields["theme"] == nil ||
		settingsFields["max_concurrency"] == nil || settingsFields["keep_all_history"] == nil {
		return registryState{}, ErrRegistryUnavailable
	}
	return state, nil
}

func writeRegistryState(root *os.Root, state registryState, beforeSync func() error) error {
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxRegistryBytes {
		return ErrRegistryUnavailable
	}
	if old, err := root.Lstat("registry.json"); err == nil {
		if !old.Mode().IsRegular() || old.Mode().Perm()&0077 != 0 {
			return ErrRegistryUnavailable
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrRegistryUnavailable
	}
	id, err := randomDocumentID()
	if err != nil {
		return err
	}
	tempName := ".registry-" + id + ".tmp"
	file, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tempName)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Rename(tempName, "registry.json"); err != nil {
		return err
	}
	if beforeSync != nil {
		if err := beforeSync(); err != nil {
			return fmt.Errorf("registry replacement not durable: %w: %v", ErrRegistryInDoubt, err)
		}
	}
	if err := syncDirectory(root); err != nil {
		return fmt.Errorf("registry replacement not durable: %w: %v", ErrRegistryInDoubt, err)
	}
	return nil
}

// A valid registered root must have the same path and directory identity on
// each use. The grant digest is transport correlation, not an access token.
func validProjectPath(value string) bool {
	return value != "" && len(value) <= MaxRelativePathBytes && utf8.ValidString(value) &&
		filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsAny(value, "\x00\n\r")
}

func pathDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
