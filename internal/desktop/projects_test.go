package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func registryFixture(t *testing.T) (*Registry, string, string) {
	t.Helper()
	base := t.TempDir()
	stateDir := filepath.Join(base, "state")
	projectDir := filepath.Join(base, "project with spaces")
	if err := os.Mkdir(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry, stateDir, projectDir
}

func TestProjectGrantOneUseAndRestartIdentity(t *testing.T) {
	registry, stateDir, projectDir := registryFixture(t)
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil || grant.Token == "" || grant.PathDigest == "" {
		t.Fatalf("grant = %#v, %v", grant, err)
	}
	if _, err := registry.AddProject(grant.Token, "sha256:incorrect"); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("wrong digest = %v", err)
	}
	if _, err := registry.AddProject(grant.Token, grant.PathDigest); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("consumed grant = %v", err)
	}
	grant, err = registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil || project.ProjectID == "" || project.Availability != AvailabilityReady {
		t.Fatalf("project = %#v, %v", project, err)
	}
	if _, err := registry.AddProject(grant.Token, grant.PathDigest); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("grant reused: %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	projects, err := reopened.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ProjectID != project.ProjectID || projects[0].CanonicalPath != project.CanonicalPath {
		t.Fatalf("restart projects = %#v", projects)
	}
	resolved, err := reopened.ResolveProject(project.ProjectID)
	if err != nil || resolved.Identity != project.Identity {
		t.Fatalf("resolved = %#v, %v", resolved, err)
	}
}

func TestProjectGrantExpiryAndRootMove(t *testing.T) {
	registry, _, projectDir := registryFixture(t)
	now := time.Now()
	registry.now = func() time.Time { return now }
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Second)
	if _, err := registry.AddProject(grant.Token, grant.PathDigest); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("expired grant = %v", err)
	}
	grant, err = registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(projectDir, projectDir+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddProject(grant.Token, grant.PathDigest); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("moved root accepted: %v", err)
	}
}

func TestWorkspaceAvailabilityAndNoTraversal(t *testing.T) {
	registry, _, projectDir := registryFixture(t)
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	workspaceDir := filepath.Join(projectDir, "work area")
	if err := os.Mkdir(workspaceDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"../outside", "/tmp", "work area/../work area", `work area\child`} {
		if _, err := registry.AddWorkspace(project.ProjectID, relative); !errors.Is(err, ErrInvalidDocumentPath) {
			t.Errorf("path %q error = %v", relative, err)
		}
	}
	workspace, err := registry.AddWorkspace(project.ProjectID, "work area")
	if err != nil || workspace.Availability != AvailabilityReady {
		t.Fatalf("workspace = %#v, %v", workspace, err)
	}
	if _, err := registry.AddWorkspace(project.ProjectID, "work area"); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("duplicate workspace = %v", err)
	}
	link := filepath.Join(projectDir, "linked")
	if err := os.Symlink(workspaceDir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddWorkspace(project.ProjectID, "linked"); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("symlink workspace = %v", err)
	}
	if err := os.Rename(workspaceDir, workspaceDir+"-moved"); err != nil {
		t.Fatal(err)
	}
	listed, err := registry.ListWorkspaces(project.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].WorkspaceID != workspace.WorkspaceID || listed[0].Availability != AvailabilityMissing {
		t.Fatalf("workspace after external removal = %#v", listed)
	}
}

func TestRegistryRejectsInsecureAndMalformedState(t *testing.T) {
	base := t.TempDir()
	stateDir := filepath.Join(base, "state")
	if err := os.Mkdir(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "registry.json"), []byte(`{"version":1,"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(stateDir); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
	if err := os.Remove(filepath.Join(stateDir, "registry.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(stateDir, "registry.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(stateDir); err == nil {
		t.Fatal("registry symlink accepted")
	}
	if err := os.Remove(filepath.Join(stateDir, "registry.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "registry.json"), make([]byte, maxRegistryBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(stateDir); err == nil {
		t.Fatal("oversized registry accepted")
	}
	if err := os.WriteFile(filepath.Join(stateDir, "registry.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(stateDir, "registry.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(stateDir); err == nil {
		t.Fatal("world-readable registry accepted")
	}
}

func TestRegistryCommitInDoubtBlocksFurtherUse(t *testing.T) {
	registry, stateDir, projectDir := registryFixture(t)
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	registry.beforeDirectorySync = func() error { return errors.New("injected sync error") }
	if _, err := registry.AddProject(grant.Token, grant.PathDigest); !errors.Is(err, ErrRegistryInDoubt) {
		t.Fatalf("post-replace failure = %v", err)
	}
	if _, err := registry.ListProjects(); !errors.Is(err, ErrRegistryInDoubt) {
		t.Fatalf("read after in-doubt commit = %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	projects, err := reopened.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("restart after in-doubt commit = %#v, %v", projects, err)
	}
}

func TestProjectAliasCannotRegisterSameDirectoryTwice(t *testing.T) {
	registry, _, projectDir := registryFixture(t)
	first, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddProject(first.Token, first.PathDigest); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(projectDir), "project alias")
	if err := os.Symlink(projectDir, alias); err != nil {
		t.Fatal(err)
	}
	second, err := registry.GrantProjectFolder(alias)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddProject(second.Token, second.PathDigest); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("alias registration = %v", err)
	}
}

func TestWorkspaceRejectsIntermediateSymlinkSwap(t *testing.T) {
	registry, _, projectDir := registryFixture(t)
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(projectDir, "nested")
	if err := os.MkdirAll(filepath.Join(nested, "leaf"), 0700); err != nil {
		t.Fatal(err)
	}
	registry.beforeWorkspaceOpen = func() error {
		if err := os.Rename(nested, nested+"-moved"); err != nil {
			return err
		}
		return os.Symlink(nested+"-moved", nested)
	}
	if _, err := registry.AddWorkspace(project.ProjectID, "nested/leaf"); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("intermediate symlink swap = %v", err)
	}
	workspaces, err := registry.ListWorkspaces(project.ProjectID)
	if err != nil || len(workspaces) != 0 {
		t.Fatalf("workspace registered through symlink: %#v, %v", workspaces, err)
	}
}

func TestRegistryStopsWhenLockPathIsReplaced(t *testing.T) {
	first, stateDir, _ := registryFixture(t)
	if err := os.Remove(filepath.Join(stateDir, registryLockName)); err != nil {
		t.Fatal(err)
	}
	second, err := NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := first.ListProjects(); !errors.Is(err, ErrRegistryInDoubt) {
		t.Fatalf("old lock holder stayed active: %v", err)
	}
	if _, err := second.ListProjects(); err != nil {
		t.Fatalf("new lock holder unavailable: %v", err)
	}
}
