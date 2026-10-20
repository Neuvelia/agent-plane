//go:build darwin || linux

package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var errSimulatedSaveCrash = errors.New("simulated save crash")

func TestDocumentSavePreservesExactTextAndRevision(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(validGraphText, "# user comment", "# retained comment", 1)
	updated, err := store.Update(doc.DocumentID, doc.Revision, changed)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != changed || saved.Revision != updated.Revision || saved.DiskDigest != digestBytes(data) {
		t.Fatalf("save changed text/revision/digest: %#v, %q", saved, data)
	}
}

func TestDocumentSaveRejectsExternalEditBeforeExchange(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(doc.DocumentID, 1, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	external := []byte(strings.Replace(validGraphText, "id: local", "id: external", 1))
	if err := os.WriteFile(filename, external, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(doc.DocumentID, 2, doc.DiskDigest, false); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("external edit error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != string(external) {
		t.Fatalf("external edit overwritten: %q, %v", data, err)
	}
}

func TestDocumentSaveRestoresEditRacingWithExchange(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(doc.DocumentID, 1, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	external := []byte(strings.Replace(validGraphText, "id: local", "id: racing-editor", 1))
	store.beforeExchange = func() error { return os.WriteFile(filename, external, 0600) }
	if _, err := store.Save(doc.DocumentID, 2, doc.DiskDigest, false); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("racing edit error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != string(external) {
		t.Fatalf("racing edit lost: %q, %v", data, err)
	}
	if matches, err := os.ReadDir(store.stateDir); err != nil || len(matches) == 0 {
		t.Fatalf("conflict intent was not preserved: %v, %v", matches, err)
	}
}

func TestDocumentSaveRecoversCrashAfterExchange(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(validGraphText, "id: local", "id: candidate", 1)
	_, err = store.Update(doc.DocumentID, 1, changed)
	if err != nil {
		t.Fatal(err)
	}
	store.afterExchange = func() error { return errSimulatedSaveCrash }
	if _, err := store.Save(doc.DocumentID, 2, doc.DiskDigest, false); !errors.Is(err, errSimulatedSaveCrash) {
		t.Fatalf("simulated crash error = %v", err)
	}
	restarted := NewDocumentStore(store.resolver, store.stateDir)
	report, err := restarted.Recover()
	if err != nil || report.Resolved != 1 || len(report.Conflicts) != 0 {
		t.Fatalf("recovery report = %#v, %v", report, err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != changed {
		t.Fatalf("recovered graph = %q, %v", data, err)
	}
}

func TestDocumentSaveRecoversKilledWriter(t *testing.T) {
	store, filename := documentFixture(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestDocumentSaveCrashChild$")
	cmd.Env = append(os.Environ(), "AGENTPLANE_SAVE_CRASH_CHILD=1", "AGENTPLANE_SAVE_CRASH_ROOT="+filepath.Dir(filepath.Dir(filepath.Dir(filename))), "AGENTPLANE_SAVE_CRASH_STATE="+store.stateDir)
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Fatalf("writer exit = %v", err)
	}
	restarted := NewDocumentStore(store.resolver, store.stateDir)
	report, err := restarted.Recover()
	if err != nil || report.Resolved != 1 || len(report.Conflicts) != 0 {
		t.Fatalf("recovery report = %#v, %v", report, err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || !strings.Contains(string(data), "id: candidate") {
		t.Fatalf("recovered graph = %q, %v", data, err)
	}
}

func TestDocumentSaveRecoveryRestoresSymlinkedTargetAfterCrash(t *testing.T) {
	store, filename := documentFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDocumentSaveCrashChild$")
	cmd.Env = append(os.Environ(), "AGENTPLANE_SAVE_CRASH_CHILD=1", "AGENTPLANE_SAVE_CRASH_ROOT="+filepath.Dir(filepath.Dir(filepath.Dir(filename))), "AGENTPLANE_SAVE_CRASH_STATE="+store.stateDir, "AGENTPLANE_SAVE_CRASH_SYMLINK="+outside)
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Fatalf("writer exit = %v", err)
	}
	restarted := NewDocumentStore(store.resolver, store.stateDir)
	report, err := restarted.Recover()
	if err != nil || report.Resolved != 0 || len(report.Conflicts) != 1 {
		t.Fatalf("recovery report = %#v, %v", report, err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != validGraphText {
		t.Fatalf("original not restored: %q, %v", data, err)
	}
}

func TestDocumentSaveCrashChild(t *testing.T) {
	if os.Getenv("AGENTPLANE_SAVE_CRASH_CHILD") != "1" {
		return
	}
	project, err := RegisterProjectRoot(os.Getenv("AGENTPLANE_SAVE_CRASH_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewDocumentStore(fixedProjectRoot(project), os.Getenv("AGENTPLANE_SAVE_CRASH_STATE"))
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	store.afterExchange = func() error {
		if outside := os.Getenv("AGENTPLANE_SAVE_CRASH_SYMLINK"); outside != "" {
			target := filepath.Join(project.CanonicalRoot, ".agents", "graphs", "local.yaml")
			if err := os.Remove(target); err != nil {
				return err
			}
			if err := os.Symlink(outside, target); err != nil {
				return err
			}
		}
		os.Exit(42)
		return nil
	}
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); err != nil {
		t.Fatal(err)
	}
	t.Fatal("writer did not exit at exchange")
}

func TestDocumentSaveRecoveryPreservesPostExchangeEdit(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(doc.DocumentID, 1, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	external := strings.Replace(validGraphText, "id: local", "id: post-exchange", 1)
	store.afterExchange = func() error {
		if err := os.WriteFile(filename, []byte(external), 0600); err != nil {
			return err
		}
		return errSimulatedSaveCrash
	}
	if _, err := store.Save(doc.DocumentID, 2, doc.DiskDigest, false); !errors.Is(err, errSimulatedSaveCrash) {
		t.Fatal(err)
	}
	restarted := NewDocumentStore(store.resolver, store.stateDir)
	report, err := restarted.Recover()
	if err != nil || report.Resolved != 0 || len(report.Conflicts) != 1 {
		t.Fatalf("recovery = %#v, %v", report, err)
	}
	target, err := os.ReadFile(filename)
	if err != nil || string(target) != validGraphText {
		t.Fatalf("original was not restored: %q, %v", target, err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(filename), ".agentplane-save-*.candidate"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("conflict candidate = %v, %v", matches, err)
	}
	retained, err := os.ReadFile(matches[0])
	if err != nil || string(retained) != external {
		t.Fatalf("external bytes lost: %q, %v", retained, err)
	}
}

func TestDocumentSaveRejectsUnprivateStateDirectory(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(doc.DocumentID, doc.Revision, doc.DiskDigest, false); !errors.Is(err, ErrDocumentSaveUnavailable) {
		t.Fatalf("unprivate state directory error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != validGraphText {
		t.Fatalf("graph changed: %q, %v", data, err)
	}
}

func TestDocumentSaveCleansPartialWriteBeforeIntent(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	writeFailure := errors.New("injected write failure")
	store.candidateWriter = func(parent *os.Root, name string, _ []byte, _ os.FileMode) error {
		file, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err := file.Write([]byte("partial")); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return writeFailure
	}
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); !errors.Is(err, writeFailure) {
		t.Fatalf("partial write error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != validGraphText {
		t.Fatalf("graph changed: %q, %v", data, err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(filename), ".agentplane-save-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("unjournaled artifacts remain: %v, %v", matches, err)
	}
	entries, err := os.ReadDir(store.stateDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected intent after write failure: %v, %v", entries, err)
	}
}

func TestDocumentSaveRejectsParentReplacementBeforeExchange(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(doc.DocumentID, 1, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(filename)
	moved := parent + "-moved"
	store.beforeExchange = func() error {
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		if err := os.Mkdir(parent+"-other", 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(parent+"-other", "local.yaml"), []byte(validGraphText), 0600); err != nil {
			return err
		}
		return os.Symlink(parent+"-other", parent)
	}
	if _, err := store.Save(doc.DocumentID, 2, doc.DiskDigest, false); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("replaced parent error = %v", err)
	}
	for _, candidate := range []string{filepath.Join(moved, "local.yaml"), filepath.Join(parent+"-other", "local.yaml")} {
		data, err := os.ReadFile(candidate)
		if err != nil || string(data) != validGraphText {
			t.Fatalf("modified %s: %q, %v", candidate, data, err)
		}
	}
}

func TestDocumentSaveRejectsCandidateSymlinkBeforeExchange(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	store.beforeExchange = func() error {
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(filename), ".agentplane-save-*.candidate"))
		if err != nil || len(matches) != 1 {
			return errors.New("candidate missing")
		}
		if err := os.Remove(matches[0]); err != nil {
			return err
		}
		return os.Symlink(outside, matches[0])
	}
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("candidate symlink error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != validGraphText {
		t.Fatalf("target changed: %q, %v", data, err)
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("target type = %v, %v", info, err)
	}
}

func TestDocumentSaveRestoresUnexpectedTargetAfterExchange(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	store.afterExchange = func() error {
		if err := os.Remove(filename); err != nil {
			return err
		}
		return os.Symlink(outside, filename)
	}
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("unexpected target error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != validGraphText {
		t.Fatalf("original not restored: %q, %v", data, err)
	}
}

func TestDocumentSaveRejectsProjectRootMoveBeforeExchange(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.resolver.ResolveProject("project-1")
	if err != nil {
		t.Fatal(err)
	}
	moved := project.CanonicalRoot + "-moved"
	store.beforeExchange = func() error {
		if err := os.Rename(project.CanonicalRoot, moved); err != nil {
			return err
		}
		return os.Mkdir(project.CanonicalRoot, 0700)
	}
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); !errors.Is(err, ErrInvalidDocumentPath) {
		t.Fatalf("moved root error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(moved, ".agents/graphs/local.yaml"))
	if err != nil || string(data) != validGraphText {
		t.Fatalf("detached graph changed: %q, %v", data, err)
	}
}

func TestDocumentSaveRejectsCandidateReplacementBeforeRestore(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	external := strings.Replace(validGraphText, "id: local", "id: external", 1)
	store.beforeExchange = func() error { return os.WriteFile(filename, []byte(external), 0600) }
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	store.beforeRestore = func() error {
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(filename), ".agentplane-save-*.candidate"))
		if err != nil || len(matches) != 1 {
			return errors.New("displaced path missing")
		}
		if err := os.Rename(matches[0], matches[0]+".moved"); err != nil {
			return err
		}
		return os.Symlink(outside, matches[0])
	}
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); !errors.Is(err, ErrDocumentSaveInDoubt) {
		t.Fatalf("candidate replacement error = %v", err)
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("graph target type = %v, %v", info, err)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(filename), ".agentplane-save-*.candidate.moved"))
	if len(matches) != 1 {
		t.Fatalf("external version missing: %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || string(data) != external {
		t.Fatalf("external version lost: %q, %v", data, err)
	}
}

func TestDocumentRecoveryDoesNotResolveDetachedRoot(t *testing.T) {
	store, _ := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	store.afterExchange = func() error { return errSimulatedSaveCrash }
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); !errors.Is(err, errSimulatedSaveCrash) {
		t.Fatal(err)
	}
	project, err := store.resolver.ResolveProject("project-1")
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewDocumentStore(store.resolver, store.stateDir)
	restarted.beforeRecoveryCommit = func() error {
		if err := os.Rename(project.CanonicalRoot, project.CanonicalRoot+"-moved"); err != nil {
			return err
		}
		return os.Mkdir(project.CanonicalRoot, 0700)
	}
	report, err := restarted.Recover()
	if err != nil || report.Resolved != 0 || len(report.Conflicts) != 1 {
		t.Fatalf("detached recovery = %#v, %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(project.CanonicalRoot+"-moved", ".agents", "graphs", "local.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentRecoveryKeepsDraftWhenTargetChangesBeforeCommit(t *testing.T) {
	store, filename := documentFixture(t)
	doc, err := store.Open("project-1", ".agents/graphs/local.yaml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(doc.DocumentID, doc.Revision, strings.Replace(validGraphText, "id: local", "id: candidate", 1))
	if err != nil {
		t.Fatal(err)
	}
	store.afterExchange = func() error { return errSimulatedSaveCrash }
	if _, err := store.Save(doc.DocumentID, updated.Revision, doc.DiskDigest, false); !errors.Is(err, errSimulatedSaveCrash) {
		t.Fatal(err)
	}
	external := []byte(strings.Replace(validGraphText, "id: local", "id: external", 1))
	restarted := NewDocumentStore(store.resolver, store.stateDir)
	restarted.beforeRecoveryCommit = func() error { return os.WriteFile(filename, external, 0600) }
	report, err := restarted.Recover()
	if err != nil || report.Resolved != 0 || len(report.Conflicts) != 1 {
		t.Fatalf("recovery after target change = %#v, %v", report, err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != string(external) {
		t.Fatalf("external edit lost: %q, %v", data, err)
	}
	intents, err := filepath.Glob(filepath.Join(store.stateDir, "save-*.intent"))
	if err != nil || len(intents) != 1 {
		t.Fatalf("intent lost: %v, %v", intents, err)
	}
	drafts, err := filepath.Glob(filepath.Join(filepath.Dir(filename), ".agentplane-save-*.draft"))
	if err != nil || len(drafts) != 1 {
		t.Fatalf("draft lost: %v, %v", drafts, err)
	}
	draft, err := os.ReadFile(drafts[0])
	if err != nil || string(draft) != updated.Text {
		t.Fatalf("draft changed: %q, %v", draft, err)
	}
}
