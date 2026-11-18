package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

type testReport struct {
	ChecksExecuted bool `json:"checks_executed"`
	Changes        []struct {
		Path    string   `json:"path"`
		Sources []string `json:"sources"`
	} `json:"changes"`
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.name", "Test User")
	git(t, dir, "config", "user.email", "test@example.invalid")
	git(t, dir, "commit", "--allow-empty", "-qm", "base")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getReport(t *testing.T, dir string) testReport {
	t.Helper()
	var out bytes.Buffer
	if err := run(&out, dir); err != nil {
		t.Fatal(err)
	}
	var got testReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", out.String(), err)
	}
	if got.ChecksExecuted {
		t.Fatal("inventory must not claim checks were executed")
	}
	return got
}

func assertChanges(t *testing.T, got testReport, want map[string][]string) {
	t.Helper()
	if len(got.Changes) != len(want) {
		t.Fatalf("changes = %+v, want %v", got.Changes, want)
	}
	previous := ""
	for i, change := range got.Changes {
		if i > 0 && change.Path <= previous {
			t.Fatalf("changes are not sorted: %+v", got.Changes)
		}
		previous = change.Path
		if !reflect.DeepEqual(change.Sources, want[change.Path]) {
			t.Errorf("%q sources = %v, want %v", change.Path, change.Sources, want[change.Path])
		}
	}
}

func TestInventoryCombinesSourcesAndIgnoresExcludedFiles(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".gitignore", "ignored.txt\n")
	writeFile(t, dir, "both.go", "base")
	writeFile(t, dir, "work.go", "base")
	git(t, dir, "add", ".gitignore", "both.go", "work.go")
	git(t, dir, "commit", "-qm", "fixtures")

	writeFile(t, dir, "both.go", "staged")
	git(t, dir, "add", "both.go")
	writeFile(t, dir, "both.go", "working")
	writeFile(t, dir, "work.go", "working")
	writeFile(t, dir, "new file.go", "new")
	writeFile(t, dir, "ignored.txt", "ignored")

	assertChanges(t, getReport(t, dir), map[string][]string{
		"both.go":     {"staged", "unstaged"},
		"new file.go": {"untracked"},
		"work.go":     {"unstaged"},
	})
}

func TestInventoryKeepsStagedEditRevertedInWorkingTree(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "file.go", "base")
	git(t, dir, "add", "file.go")
	git(t, dir, "commit", "-qm", "fixture")
	writeFile(t, dir, "file.go", "staged")
	git(t, dir, "add", "file.go")
	writeFile(t, dir, "file.go", "base")

	assertChanges(t, getReport(t, dir), map[string][]string{
		"file.go": {"staged", "unstaged"},
	})
}

func TestInventoryReportsBothSidesOfStagedRename(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "old.go", "base")
	git(t, dir, "add", "old.go")
	git(t, dir, "commit", "-qm", "fixture")
	git(t, dir, "mv", "old.go", "new.go")

	assertChanges(t, getReport(t, dir), map[string][]string{
		"new.go": {"staged"},
		"old.go": {"staged"},
	})
}

func TestInventoryFromSubdirectoryPreservesUnusualNames(t *testing.T) {
	testenv.SkipOnWindows(t, "Windows file names cannot contain a newline")
	dir := newRepo(t)
	writeFile(t, dir, "nested/line\nbreak.go", "new")
	writeFile(t, dir, "nested/with space.go", "new")

	assertChanges(t, getReport(t, filepath.Join(dir, "nested")), map[string][]string{
		"nested/line\nbreak.go": {"untracked"},
		"nested/with space.go":  {"untracked"},
	})
}

func TestInventoryEmptyRepository(t *testing.T) {
	dir := newRepo(t)
	got := getReport(t, dir)
	if got.Changes == nil || len(got.Changes) != 0 {
		t.Fatalf("changes = %#v, want empty array", got.Changes)
	}
}

func TestInventoryOutsideRepositoryDoesNotEmitJSON(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := run(&out, dir)
	if err == nil || !strings.Contains(err.Error(), "Git") {
		t.Fatalf("error = %v, want Git failure", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestInventoryRejectsInvalidUTF8PathWithoutJSON(t *testing.T) {
	testenv.SkipOnWindows(t, "Windows argv is UTF-16, so git cannot receive the invalid byte")
	dir := newRepo(t)
	name := string([]byte{'b', 'a', 'd', 0xff})
	hashCommand := exec.Command("git", "-C", dir, "hash-object", "-w", "--stdin")
	hashCommand.Stdin = strings.NewReader("content")
	hash, err := hashCommand.Output()
	if err != nil {
		t.Fatalf("create fixture blob: %v", err)
	}
	git(t, dir, "update-index", "--add", "--cacheinfo", "100644,"+strings.TrimSpace(string(hash))+","+name)
	var out bytes.Buffer
	err = run(&out, dir)
	if err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("error = %v, want invalid UTF-8 path", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output: %q", out.String())
	}
}
