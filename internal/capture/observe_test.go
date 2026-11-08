package capture

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureRejectsUnsafeEnumeratedName(t *testing.T) {
	workspace, resolve := workspaceFixture(t)
	name := "bad\\name"
	if err := os.WriteFile(filepath.Join(workspace, name), []byte("hidden"), 0600); err != nil {
		t.Skipf("filesystem cannot create unsafe fixture: %v", err)
	}
	if _, err := Take(context.Background(), resolve, []string{"./"}, &Budget{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("unsafe child silently omitted: %v", err)
	}
}

func TestSortedDirectoryInventoryBeforeBudgetAllocation(t *testing.T) {
	workspace, resolve := workspaceFixture(t)
	// Create reverse lexical order. A one-byte budget leaves only the first
	// path available, independently of the filesystem's enumeration order.
	for _, name := range []string{"z.txt", "m.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	budget := &Budget{}
	if !budget.Reserve(MaxRunBytes - 1) {
		t.Fatal("budget setup")
	}
	snapshot, err := Take(context.Background(), resolve, []string{"./"}, budget)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "m.txt", "z.txt"} {
		image := snapshot.Images[name]
		if name == "a.txt" && image.Unavailable != "" {
			t.Fatalf("first lexical path unavailable: %+v", image)
		}
		if name != "a.txt" && image.Unavailable != "size_limit" {
			t.Fatalf("later lexical path unexpectedly available: %+v", image)
		}
	}
}

func workspaceFixture(t *testing.T) (string, Resolver) {
	t.Helper()
	workspace := t.TempDir()
	identity, err := DirectoryIdentity(workspace)
	if err != nil {
		t.Skipf("native workspace identity fixture unavailable: %v", err)
	}
	return workspace, func() (Workspace, error) { return Workspace{Path: workspace, Identity: identity}, nil }
}

func TestAttemptImagesIncludeIgnoredAndUntrackedChanges(t *testing.T) {
	workspace, resolve := workspaceFixture(t)
	if err := os.Mkdir(filepath.Join(workspace, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "src", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", ".git", "config"), []byte("metadata secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"src/main.txt": "before\n", "src/ignored.txt": "ignored before\n", ".gitignore": "src/ignored.txt\n"} {
		if err := os.WriteFile(filepath.Join(workspace, filepath.FromSlash(name)), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	budget := &Budget{}
	before, err := Take(context.Background(), resolve, []string{"src/"}, budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src/main.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src/ignored.txt"), []byte("ignored after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src/new.txt"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := Take(context.Background(), resolve, []string{"src/"}, budget)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := Changes(before, after)
	if err != nil || len(changes) != 3 {
		t.Fatalf("changes=%+v error=%v", changes, err)
	}
	if _, present := before.Images["src/.git/config"]; present {
		t.Fatal("nested repository metadata captured")
	}
	if changes[0].Path != "src/ignored.txt" || changes[1].Path != "src/main.txt" || changes[2].Path != "src/new.txt" || changes[2].Before.Present {
		t.Fatalf("unexpected changes: %+v", changes)
	}
	if string(changes[0].Before.Content) != "ignored before\n" {
		t.Fatal("attempt-start preimage lost")
	}
	if err := os.WriteFile(filepath.Join(workspace, "src/ignored.txt"), []byte("retry mutation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if string(changes[0].Before.Content) != "ignored before\n" || string(changes[0].After.Content) != "ignored after\n" {
		t.Fatal("later correction mutated previous attempt images")
	}
}

func TestCaptureRejectsSymlinksAndWorkspaceChange(t *testing.T) {
	workspace, resolve := workspaceFixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	snapshot, err := Take(context.Background(), resolve, []string{"link"}, &Budget{})
	if err != nil || snapshot.Images["link"].Unavailable != "unsafe_path" || len(snapshot.Images["link"].Content) != 0 {
		t.Fatalf("symlink image=%+v error=%v", snapshot.Images["link"], err)
	}
	mutatingCalls := 0
	actualIdentity, err := DirectoryIdentity(workspace)
	if err != nil {
		t.Skipf("native workspace identity fixture unavailable: %v", err)
	}
	changed := func() (Workspace, error) {
		mutatingCalls++
		identity := actualIdentity
		if mutatingCalls > 1 {
			identity = "two"
		}
		return Workspace{Path: workspace, Identity: identity}, nil
	}
	if _, err := Take(context.Background(), changed, []string{"link"}, &Budget{}); !errors.Is(err, ErrWorkspaceChanged) {
		t.Fatalf("changed selection accepted: %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "secret outside" {
		t.Fatalf("outside changed: %q %v", got, err)
	}
}

func TestCaptureRejectsReplacedRegisteredWorkspace(t *testing.T) {
	workspace, resolve := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "work.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := Take(context.Background(), resolve, []string{"work.txt"}, &Budget{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(workspace, workspace+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "work.txt"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(context.Background(), resolve, []string{"work.txt"}, &Budget{}); !errors.Is(err, ErrWorkspaceChanged) {
		t.Fatalf("replacement workspace read: %v", err)
	}
	if string(before.Images["work.txt"].Content) != "before" {
		t.Fatal("pinned preimage mutated")
	}
}

func TestAttemptImageBudgetAndUnavailablePreimage(t *testing.T) {
	workspace, resolve := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "large"), bytes.Repeat([]byte("x"), MaxFileBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := Take(context.Background(), resolve, []string{"large"}, &Budget{})
	if err != nil || first.Images["large"].Unavailable != "size_limit" || !first.Images["large"].Present {
		t.Fatalf("oversize image=%+v error=%v", first.Images["large"], err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "large"), []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := Take(context.Background(), resolve, []string{"large"}, &Budget{})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := Changes(first, second)
	if err != nil || len(changes) != 1 || changes[0].Before.Unavailable != "size_limit" || !changes[0].Before.Present {
		t.Fatalf("missing preimage treated as creation: %+v %v", changes, err)
	}
	budget := &Budget{}
	if !budget.Reserve(MaxRunBytes-1) || budget.Reserve(2) || budget.Used() != MaxRunBytes-1 {
		t.Fatalf("run budget=%d", budget.Used())
	}
}

func TestTextDiffIsBoundedAndRejectsBinary(t *testing.T) {
	diff, ok := TextDiff([]byte("same\nold\ntail\n"), []byte("same\nnew\ntail\n"))
	if !ok || string(diff) != "--- before\n+++ after\n@@\n-old\n+new\n" {
		t.Fatalf("diff=%q ok=%v", diff, ok)
	}
	if _, ok := TextDiff([]byte{'a', 0}, []byte("b")); ok {
		t.Fatal("binary diff accepted")
	}
	if _, ok := TextDiff([]byte(strings.Repeat("a", MaxFileBytes+1)), []byte("b")); ok {
		t.Fatal("oversized diff accepted")
	}
}
