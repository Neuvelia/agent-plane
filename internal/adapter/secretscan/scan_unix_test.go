//go:build unix

package secretscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"agentplane/internal/domain/secrets"
)

const leak = "ghp_canaryValue0123456789"

func target(t *testing.T, name, value string) secrets.ScanTarget {
	t.Helper()
	got, err := secrets.NewScanTarget(name, secrets.NewValue(value))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, root string, paths []string, limits secrets.ScanLimits) secrets.ScanReport {
	t.Helper()
	report, err := Scanner{}.ScanFiles(context.Background(), root, paths, []secrets.ScanTarget{target(t, "gh_token", leak)}, limits)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	return report
}

func gapCount(report secrets.ScanReport, reason secrets.ScanGapReason) uint64 {
	for _, gap := range report.Gaps {
		if gap.Reason == reason {
			return gap.Count
		}
	}
	return 0
}

func TestScanNamesPathAndSecretForEachLeak(t *testing.T) {
	root := t.TempDir()
	write(t, root, "b/leak.txt", "token="+leak+"\n")
	write(t, root, "a.txt", "clean")
	write(t, root, "c.txt", leak)
	report := scan(t, root, []string{"c.txt", "b/leak.txt", "a.txt"}, secrets.DefaultScanLimits())
	want := []secrets.ScanFinding{{Path: "b/leak.txt", Secret: "gh_token"}, {Path: "c.txt", Secret: "gh_token"}}
	if !reflect.DeepEqual(report.Findings, want) || report.Examined != 3 || len(report.Gaps) != 0 {
		t.Errorf("report = %+v, want findings %v, 3 examined, no gaps", report, want)
	}
}

func TestScanBounds(t *testing.T) {
	t.Run("only the first 256 sorted paths are examined", func(t *testing.T) {
		root, paths := t.TempDir(), make([]string, 257)
		for i := range paths {
			paths[i] = fmt.Sprintf("f%03d.txt", i)
			write(t, root, paths[i], "x")
		}
		write(t, root, "f256.txt", leak)
		report := scan(t, root, paths, secrets.DefaultScanLimits())
		if report.Examined != 256 || gapCount(report, secrets.GapFileLimitReached) != 1 || len(report.Findings) != 0 {
			t.Errorf("report = %+v, want 256 examined, one file_limit_reached gap, no findings", report)
		}
	})
	t.Run("a file over the per-file limit is a gap and its tail is not read", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "big.txt", strings.Repeat("x", 64)+leak)
		report := scan(t, root, []string{"big.txt"}, secrets.ScanLimits{Files: 8, FileBytes: 64 + int64(len(leak)) - 1, TotalBytes: 1 << 20})
		if gapCount(report, secrets.GapFileTooLarge) != 1 || len(report.Findings) != 0 || report.Examined != 0 {
			t.Errorf("report = %+v, want one file_too_large gap", report)
		}
	})
	t.Run("a hit ending exactly at the per-file limit is found", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "edge.txt", strings.Repeat("x", 64)+leak)
		report := scan(t, root, []string{"edge.txt"}, secrets.ScanLimits{Files: 8, FileBytes: 64 + int64(len(leak)), TotalBytes: 1 << 20})
		if len(report.Findings) != 1 || len(report.Gaps) != 0 {
			t.Errorf("report = %+v, want one finding and no gap", report)
		}
	})
	t.Run("the total byte budget turns later files into gaps", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "a.txt", strings.Repeat("x", 60))
		write(t, root, "b.txt", leak+strings.Repeat("x", 60))
		report := scan(t, root, []string{"a.txt", "b.txt"}, secrets.ScanLimits{Files: 8, FileBytes: 1 << 10, TotalBytes: 100})
		if report.Examined != 1 || gapCount(report, secrets.GapTotalBudgetReached) != 1 || len(report.Findings) != 0 {
			t.Errorf("report = %+v, want 1 examined and one total_budget_exhausted gap", report)
		}
	})
}

func TestScanFindsValueSpanningReadChunks(t *testing.T) {
	root := t.TempDir()
	content := bytes.Repeat([]byte("x"), 3*readChunk)
	copy(content[readChunk-len(leak)/2:], leak)
	write(t, root, "span.bin", string(content))
	if report := scan(t, root, []string{"span.bin"}, secrets.DefaultScanLimits()); len(report.Findings) != 1 {
		t.Errorf("a value straddling a read chunk boundary was missed: %+v", report)
	}
}

func TestScanBinaryFileFindsRawValue(t *testing.T) {
	root := t.TempDir()
	write(t, root, "blob.bin", "\x00\x01\xff"+leak+"\x00\x02")
	if report := scan(t, root, []string{"blob.bin"}, secrets.DefaultScanLimits()); len(report.Findings) != 1 {
		t.Errorf("binary content was not scanned bytewise: %+v", report)
	}
}

func TestScanNeverEmitsValue(t *testing.T) {
	root := t.TempDir()
	write(t, root, "leak.txt", leak)
	if err := os.Symlink(leak+"-target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("leak.txt", filepath.Join(root, "ancestor")); err != nil {
		t.Fatal(err)
	}
	paths := []string{"leak.txt", "link", "ancestor/x", "../escape"}
	report, err := Scanner{}.ScanFiles(context.Background(), root, paths, []secrets.ScanTarget{target(t, "gh_token", leak)}, secrets.DefaultScanLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	renderings := []string{string(encoded)}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		renderings = append(renderings, fmt.Sprintf(verb, report))
	}
	for _, got := range renderings {
		if strings.Contains(got, leak) {
			t.Errorf("report rendering leaked the value: %q", got)
		}
	}
	if len(report.Findings) != 2 {
		t.Errorf("findings = %+v, want leak.txt and link", report.Findings)
	}
}

func TestScanFailureNeverCarriesValue(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	_, err := Scanner{}.ScanFiles(context.Background(), missing, []string{"a"}, []secrets.ScanTarget{target(t, "gh_token", leak)}, secrets.DefaultScanLimits())
	if err == nil {
		t.Fatal("scanning a missing root succeeded")
	}
	if strings.Contains(err.Error(), leak) {
		t.Errorf("error leaked the value: %v", err)
	}
}

func TestScanDoesNotFollowSymlinks(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.txt", leak)
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	report := scan(t, root, []string{"link"}, secrets.DefaultScanLimits())
	if len(report.Findings) != 0 {
		t.Errorf("the target of a symlink was read: %+v", report)
	}
}

func TestScanScansSymlinkTargetText(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("prefix-"+leak, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	report := scan(t, root, []string{"link"}, secrets.DefaultScanLimits())
	if want := []secrets.ScanFinding{{Path: "link", Secret: "gh_token"}}; !reflect.DeepEqual(report.Findings, want) {
		t.Errorf("findings = %+v, want %+v", report.Findings, want)
	}
}

func TestScanRefusesTraversalAndSymlinkedAncestors(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.txt", leak)
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	paths := []string{"dir/secret.txt", "../" + filepath.Base(outside) + "/secret.txt", filepath.Join(outside, "secret.txt")}
	report := scan(t, root, paths, secrets.DefaultScanLimits())
	if len(report.Findings) != 0 || report.Examined != 0 || gapCount(report, secrets.GapUnreadable) != 3 {
		t.Errorf("report = %+v, want three unreadable gaps and nothing examined", report)
	}
}

func TestScanRejectsMalformedPaths(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".git/config", leak)
	paths := []string{"", ".git/config", "a/./b", "a//b", "./a", "a/../b", "bad\x00name", "bad\xffname"}
	report := scan(t, root, paths, secrets.DefaultScanLimits())
	if len(report.Findings) != 0 || gapCount(report, secrets.GapUnreadable) != uint64(len(paths)) {
		t.Errorf("report = %+v, want every malformed path rejected as unreadable", report)
	}
}

func TestScanSkipsFifoWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("cannot create a FIFO: %v", err)
	}
	report := scan(t, root, []string{"pipe"}, secrets.DefaultScanLimits())
	if gapCount(report, secrets.GapNotRegularFile) != 1 || report.Examined != 0 {
		t.Errorf("report = %+v, want one not_regular_file gap", report)
	}
}

func TestScanReportsDirectoryAsNotRegular(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if report := scan(t, root, []string{"dir"}, secrets.DefaultScanLimits()); gapCount(report, secrets.GapNotRegularFile) != 1 {
		t.Errorf("report = %+v, want one not_regular_file gap", report)
	}
}

func TestScanDeletedPathIsNotAGap(t *testing.T) {
	root := t.TempDir()
	write(t, root, "keep/x.txt", "x")
	report := scan(t, root, []string{"gone.txt", "gone/dir/x.txt"}, secrets.DefaultScanLimits())
	if len(report.Gaps) != 0 || report.Examined != 0 || len(report.Findings) != 0 {
		t.Errorf("report = %+v, want an empty report for deleted paths", report)
	}
}

func TestScanHonoursContextCancellation(t *testing.T) {
	root := t.TempDir()
	write(t, root, "leak.txt", leak)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := Scanner{}.ScanFiles(ctx, root, []string{"leak.txt"}, []secrets.ScanTarget{target(t, "gh_token", leak)}, secrets.DefaultScanLimits())
	if !errors.Is(err, context.Canceled) || len(report.Findings) != 0 {
		t.Errorf("got (%+v, %v), want a canceled error and no findings", report, err)
	}
}

func TestScanWithoutTargetsExaminesNothing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", leak)
	report, err := Scanner{}.ScanFiles(context.Background(), root, []string{"a.txt"}, nil, secrets.DefaultScanLimits())
	if err != nil || !reflect.DeepEqual(report, secrets.ScanReport{}) {
		t.Errorf("got (%+v, %v), want an empty report", report, err)
	}
}
