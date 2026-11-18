package attemptdiff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
)

func TestDiffBinaryAndNonUTF8HashesOnly(t *testing.T) {
	dir := newRepo(t, nil)
	got := compare(t, newRecorder(t), dir, nil, func() {
		write(t, dir, "bin.dat", "a\x00b")
		write(t, dir, "latin.txt", "caf\xe9\n")
	})
	files := byPath(got)
	for _, p := range []string{"bin.dat", "latin.txt"} {
		f := files[p]
		if !f.Binary || f.OmittedReason != events.DiffOmitBinary || f.Patch != "" || len(f.AfterSHA256) != 64 || f.AfterSize == 0 {
			t.Errorf("%s = %+v", p, f)
		}
	}
}

func TestDiffTwoMiBTextTooLargeHashesOnly(t *testing.T) {
	dir := newRepo(t, nil)
	got := compare(t, newRecorder(t), dir, nil, func() { write(t, dir, "big.txt", strings.Repeat("line\n", 2<<20/5)) })
	f := byPath(got)["big.txt"]
	if f.OmittedReason != events.DiffOmitTooLarge || f.Patch != "" || f.AfterSize < 2<<20-5 || len(f.AfterSHA256) != 64 {
		t.Fatalf("big.txt = %+v", f)
	}
}

func TestDiffEditLimitIsTooLarge(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": lines("old", 6000)})
	got := compare(t, newRecorder(t), dir, nil, func() { write(t, dir, "a.txt", lines("new", 6000)) })
	if f := byPath(got)["a.txt"]; f.OmittedReason != events.DiffOmitTooLarge || f.Patch != "" {
		t.Fatalf("a.txt = %+v", f)
	}
}

func TestDiffPerAttemptBudgetOmitsRemainderAsBudget(t *testing.T) {
	dir := newRepo(t, nil)
	got := compare(t, newRecorder(t), dir, nil, func() {
		for i := 0; i < 24; i++ {
			write(t, dir, fmt.Sprintf("f%02d.txt", i), strings.Repeat("0123456789abcdef\n", 800))
		}
	})
	budgeted, patchBytes := 0, 0
	for _, f := range got.Files {
		patchBytes += len(f.Patch)
		if f.OmittedReason == events.DiffOmitBudget {
			budgeted++
			if f.Patch != "" || f.AddedLines != 800 {
				t.Errorf("budget-omitted file keeps counts and no patch: %+v", f)
			}
		}
	}
	if budgeted == 0 || patchBytes > events.MaxAttemptDiffAttemptBytes || got.Summary.PatchBytes != patchBytes || got.Summary.Files != 24 {
		t.Fatalf("budgeted=%d bytes=%d summary=%+v", budgeted, patchBytes, got.Summary)
	}
}

func TestDiffFileListCap(t *testing.T) {
	dir := newRepo(t, nil)
	got := compare(t, newRecorder(t), dir, nil, func() {
		for i := 0; i < 300; i++ {
			write(t, dir, fmt.Sprintf("d/f%03d.txt", i), "x\n")
		}
	})
	if len(got.Files) != 256 || got.Summary.Files != 256 || got.Summary.OmittedFiles != 44 {
		t.Fatalf("files=%d summary=%+v", len(got.Files), got.Summary)
	}
	if got.Files[0].Path != "d/f000.txt" || got.Files[255].Path != "d/f255.txt" {
		t.Fatalf("listed paths must be the first 256 sorted: %s .. %s", got.Files[0].Path, got.Files[255].Path)
	}
}

func TestDiffScratchTotalCapOmitsAsBudget(t *testing.T) {
	dir := newRepo(t, nil)
	r := newRecorder(t)
	r.scratch.limit = 10
	got := compare(t, r, dir, nil, func() { write(t, dir, "u.txt", "more than ten bytes\n") })
	f := byPath(got)["u.txt"]
	if f.OmittedReason != events.DiffOmitBudget || f.Patch != "" || len(f.AfterSHA256) != 64 {
		t.Fatalf("u.txt = %+v", f)
	}
}

func TestDiffIgnoredListLimit(t *testing.T) {
	dir := newRepo(t, map[string]string{".gitignore": "ignored/\n"})
	r := newRecorder(t)
	r.ignoredCap = 2
	got := compare(t, r, dir, nil, func() {
		for i := 0; i < 5; i++ {
			write(t, dir, fmt.Sprintf("ignored/f%d", i), "x")
		}
	})
	if got.Summary.DiagnosticCode != events.DiffIgnoredLimit || got.Summary.OmittedReason != events.DiffOmitUnreadable || len(got.Files) != 0 {
		t.Fatalf("summary = %+v files = %v", got.Summary, paths(got))
	}
}

func TestDiffIgnoredChangedFileListedByPathOnly(t *testing.T) {
	dir := newRepo(t, map[string]string{".gitignore": "out/\n"})
	write(t, dir, "out/cache.bin", "one")
	got := compare(t, newRecorder(t), dir, nil, func() { write(t, dir, "out/cache.bin", "twotwo") })
	f := byPath(got)["out/cache.bin"]
	if f.OmittedReason != events.DiffOmitIgnored || f.Patch != "" || f.AfterSHA256 != "" || f.Change != events.DiffModified {
		t.Fatalf("ignored file = %+v", f)
	}
}

func TestDiffCancelledContextReturnsWithoutPartialPayloads(t *testing.T) {
	dir := newRepo(t, nil)
	r := newRecorder(t)
	before := snap(t, r, dir)
	write(t, dir, "x.txt", "x\n")
	after := snap(t, r, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := r.Diff(ctx, before, after, nil)
	if err == nil || len(got.Files) != 0 || got.Summary.Files != 0 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := r.Snapshot(ctx, dir); err == nil {
		t.Fatal("snapshot must fail on a cancelled context")
	}
}

func TestDiffUnrepresentablePathIsCountedNotListed(t *testing.T) {
	dir := newRepo(t, nil)
	got := compare(t, newRecorder(t), dir, nil, func() {
		write(t, dir, "ok.txt", "x\n")
		write(t, dir, "colon:name.txt", "x\n")
	})
	if !equalStrings(paths(got), []string{"ok.txt"}) || got.Summary.DiagnosticCode != events.DiffUnsupportedEntry {
		t.Fatalf("files=%v summary=%+v", paths(got), got.Summary)
	}
}

func equalStrings(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

func TestReadFileRejectsPathSwappedForSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	_ = os.WriteFile(target, []byte("t"), 0o644)
	regular := filepath.Join(dir, "f")
	_ = os.WriteFile(regular, []byte("r"), 0o644)
	info, _ := os.Lstat(regular)
	_ = os.Remove(regular)
	if err := os.Symlink(target, regular); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := readFile(regular, info, ""); err == nil {
		t.Fatal("a path swapped for a symlink after Lstat must be rejected")
	}
}
