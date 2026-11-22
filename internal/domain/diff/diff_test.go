package diff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mustLines(t *testing.T, before, after string, l Limits) Result {
	t.Helper()
	res, err := Lines(context.Background(), []byte(before), []byte(after), l)
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	return res
}

func TestLinesGolden(t *testing.T) {
	const marker = "\\ No newline at end of file\n"
	cases := []struct {
		name           string
		before, after  string
		patch          string
		added, deleted int
	}{
		{"identical", "a\nb\n", "a\nb\n", "", 0, 0},
		{"both empty", "", "", "", 0, 0},
		{"replace", "a\nb\nc\n", "a\nB\nc\n", "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n", 1, 1},
		{"empty to non-empty", "", "x\n", "@@ -0,0 +1 @@\n+x\n", 1, 0},
		{"non-empty to empty", "x\n", "", "@@ -1 +0,0 @@\n-x\n", 0, 1},
		{"append", "a\n", "a\nb\n", "@@ -1 +1,2 @@\n a\n+b\n", 1, 0},
		{"delete middle", "a\nb\nc\n", "a\nc\n", "@@ -1,3 +1,2 @@\n a\n-b\n c\n", 0, 1},
		{"no newline before", "a\nb", "a\nb\n", "@@ -1,2 +1,2 @@\n a\n-b\n" + marker + "+b\n", 1, 1},
		{"no newline after", "a\nb\n", "a\nb", "@@ -1,2 +1,2 @@\n a\n-b\n+b\n" + marker, 1, 1},
		{"no newline both, changed", "a\nb", "a\nc", "@@ -1,2 +1,2 @@\n a\n-b\n" + marker + "+c\n" + marker, 1, 1},
		{"no newline both, unchanged tail", "a\nz", "b\nz", "@@ -1,2 +1,2 @@\n-a\n+b\n z\n" + marker, 1, 1},
		{"crlf is part of the line", "a\r\nb\r\n", "a\r\nc\r\n", "@@ -1,2 +1,2 @@\n a\r\n-b\r\n+c\r\n", 1, 1},
		{"crlf versus lf differs", "a\r\n", "a\n", "@@ -1 +1 @@\n-a\r\n+a\n", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustLines(t, tc.before, tc.after, Limits{})
			if got.Patch != tc.patch || got.Added != tc.added || got.Deleted != tc.deleted || got.Truncated {
				t.Fatalf("got %+v\nwant patch %q added %d deleted %d", got, tc.patch, tc.added, tc.deleted)
			}
		})
	}
}

func numbered(n int, edit map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if s, ok := edit[i]; ok {
			b.WriteString(s)
		} else {
			fmt.Fprintf(&b, "line %d", i)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestLinesHunkMergingAndContext(t *testing.T) {
	before := numbered(30, nil)
	one := mustLines(t, before, numbered(30, map[int]string{15: "X"}), Limits{})
	want := "@@ -12,7 +12,7 @@\n line 12\n line 13\n line 14\n-line 15\n+X\n line 16\n line 17\n line 18\n"
	if one.Patch != want {
		t.Fatalf("three lines of context each side:\n%s", one.Patch)
	}
	merged := mustLines(t, before, numbered(30, map[int]string{2: "X", 9: "Y"}), Limits{})
	if n := strings.Count(merged.Patch, "@@ -"); n != 1 {
		t.Fatalf("a gap of exactly six unchanged lines merges into one hunk, got %d:\n%s", n, merged.Patch)
	}
	split := mustLines(t, before, numbered(30, map[int]string{2: "X", 10: "Y"}), Limits{})
	if n := strings.Count(split.Patch, "@@ -"); n != 2 {
		t.Fatalf("a gap of seven unchanged lines stays two hunks, got %d:\n%s", n, split.Patch)
	}
}

func TestLinesDeterministic(t *testing.T) {
	a, b := numbered(40, map[int]string{3: "p", 20: "q"}), numbered(40, map[int]string{5: "r", 20: "s"})
	first := mustLines(t, a, b, Limits{})
	for i := 0; i < 5; i++ {
		if again := mustLines(t, a, b, Limits{}); again != first {
			t.Fatal("same input produced a different result")
		}
	}
}

// applyHunks is a test-local unified patch applier, independent of the code
// under test, used to prove the patch reproduces the target.
func hunkStart(header string) (int, error) {
	f := strings.Fields(header)
	if len(f) < 4 {
		return 0, fmt.Errorf("bad header %q", header)
	}
	parse := func(s string) (int, int, error) {
		s = strings.TrimLeft(s, "-+")
		start, count := s, "1"
		if i := strings.IndexByte(s, ','); i >= 0 {
			start, count = s[:i], s[i+1:]
		}
		a, err := strconv.Atoi(start)
		if err != nil {
			return 0, 0, err
		}
		c, err := strconv.Atoi(count)
		return a, c, err
	}
	bs, bl, err := parse(f[1])
	if err != nil {
		return 0, err
	}
	if bl == 0 {
		return bs, nil
	}
	return bs - 1, nil
}

func splitForTest(s string) []string {
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

type patchLine struct {
	prefix byte
	text   string
}

func applyHunks(src []string, patch string) (string, error) {
	var lines []patchLine
	var starts []int
	for _, raw := range splitForTest(patch) {
		switch {
		case strings.HasPrefix(raw, "@@ "):
			s, err := hunkStart(raw)
			if err != nil {
				return "", err
			}
			lines = append(lines, patchLine{'@', ""})
			starts = append(starts, s)
		case strings.HasPrefix(raw, "\\ "):
			last := &lines[len(lines)-1]
			last.text = strings.TrimSuffix(last.text, "\n")
		default:
			lines = append(lines, patchLine{raw[0], raw[1:]})
		}
	}
	var out []string
	pos, hunk := 0, -1
	for _, pl := range lines {
		if pl.prefix == '@' {
			hunk++
			for pos < starts[hunk] {
				out = append(out, src[pos])
				pos++
			}
			continue
		}
		if pl.prefix == '+' {
			out = append(out, pl.text)
			continue
		}
		if pos >= len(src) || src[pos] != pl.text {
			return "", fmt.Errorf("patch does not match source at line %d", pos+1)
		}
		if pl.prefix == ' ' {
			out = append(out, src[pos])
		}
		pos++
	}
	out = append(out, src[pos:]...)
	return strings.Join(out, ""), nil
}

func randomText(r *rand.Rand, maxLines int) string {
	alphabet := []string{"a", "b", "c", "dd", "e e", "", "}", "x\r"}
	n := r.Intn(maxLines + 1)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(alphabet[r.Intn(len(alphabet))])
		if i < n-1 || r.Intn(4) != 0 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func mutate(r *rand.Rand, s string) string {
	lines := splitForTest(s)
	for i, edits := 0, r.Intn(6); i < edits; i++ {
		switch r.Intn(3) {
		case 0:
			if len(lines) > 0 {
				k := r.Intn(len(lines))
				lines = append(lines[:k], lines[k+1:]...)
			}
		case 1:
			k := r.Intn(len(lines) + 1)
			lines = append(lines[:k], append([]string{randomText(r, 1) + "\n"}, lines[k:]...)...)
		default:
			if len(lines) > 0 {
				lines[r.Intn(len(lines))] = randomText(r, 1) + "\n"
			}
		}
	}
	return strings.Join(lines, "")
}

func lcsLen(a, b []string) int {
	prev := make([]int, len(b)+1)
	for i := range a {
		cur := make([]int, len(b)+1)
		for j := range b {
			switch {
			case a[i] == b[j]:
				cur[j+1] = prev[j] + 1
			case prev[j+1] >= cur[j]:
				cur[j+1] = prev[j+1]
			default:
				cur[j+1] = cur[j]
			}
		}
		prev = cur
	}
	return prev[len(b)]
}

func TestLinesRoundTripProperty(t *testing.T) {
	r := rand.New(rand.NewSource(20261117))
	for i := 0; i < 600; i++ {
		before := randomText(r, 40)
		after := mutate(r, before)
		if i%5 == 0 {
			after = randomText(r, 40)
		}
		res := mustLines(t, before, after, Limits{MaxPatchBytes: 1 << 20})
		got, err := applyHunks(splitForTest(before), res.Patch)
		if err != nil || got != after {
			t.Fatalf("case %d: patch does not reproduce the target (err %v)\nbefore %q\nafter %q\npatch\n%s", i, err, before, after, res.Patch)
		}
		a, b := splitForTest(before), splitForTest(after)
		lcs := lcsLen(a, b)
		if res.Added != len(b)-lcs || res.Deleted != len(a)-lcs {
			t.Fatalf("case %d: counts +%d -%d are not minimal (lcs %d, %d vs %d lines)", i, res.Added, res.Deleted, lcs, len(a), len(b))
		}
		if (before == after) != (res.Patch == "") {
			t.Fatalf("case %d: empty patch iff identical", i)
		}
	}
}

func TestLinesCountsSurviveTruncation(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&before, "old %d\n", i)
		fmt.Fprintf(&after, "new %d\n", i)
	}
	full := mustLines(t, before.String(), after.String(), Limits{MaxPatchBytes: 1 << 20, MaxEdits: 1000})
	cut := mustLines(t, before.String(), after.String(), Limits{MaxPatchBytes: 512, MaxEdits: 1000})
	if !cut.Truncated || full.Truncated {
		t.Fatalf("truncated flags: cut %v full %v", cut.Truncated, full.Truncated)
	}
	if cut.Added != 400 || cut.Deleted != 400 || cut.Added != full.Added || cut.Deleted != full.Deleted {
		t.Fatalf("counts changed by truncation: %+v vs %+v", cut, full)
	}
}

func TestLinesTruncatesAtLineBoundary(t *testing.T) {
	before := numbered(200, nil)
	after := numbered(200, map[int]string{50: "changed", 120: "also changed"})
	for max := 1; max < 400; max += 7 {
		res := mustLines(t, before, after, Limits{MaxPatchBytes: max})
		if len(res.Patch) > max {
			t.Fatalf("max %d: patch is %d bytes", max, len(res.Patch))
		}
		if res.Patch != "" && !strings.HasSuffix(res.Patch, "\n") {
			t.Fatalf("max %d: cut inside a line: %q", max, res.Patch)
		}
		if !res.Truncated && res.Patch == "" {
			t.Fatalf("max %d: an empty patch for a changed file must be flagged truncated", max)
		}
	}
	whole := mustLines(t, before, after, Limits{})
	if whole.Truncated {
		t.Fatalf("a small patch must fit the default budget (%d bytes)", len(whole.Patch))
	}
	if exact := mustLines(t, before, after, Limits{MaxPatchBytes: len(whole.Patch)}); exact.Truncated || exact.Patch != whole.Patch {
		t.Fatal("a patch of exactly the budget must not be truncated")
	}
	if over := mustLines(t, before, after, Limits{MaxPatchBytes: len(whole.Patch) - 1}); !over.Truncated {
		t.Fatal("one byte over the budget must truncate")
	}
}

func TestLinesEditLimitReturnsErrTooLarge(t *testing.T) {
	var before, after bytes.Buffer
	for before.Len() < 1<<20 {
		fmt.Fprintf(&before, "before line %d\n", before.Len())
		fmt.Fprintf(&after, "after line %d\n", after.Len())
	}
	start := time.Now()
	_, err := Lines(context.Background(), before.Bytes(), after.Bytes(), Limits{MaxEdits: 500})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("the edit limit did not bound the work: %v", d)
	}
}

func TestLinesAtTheEditLimit(t *testing.T) {
	before, after := numbered(50, nil), numbered(50, map[int]string{10: "x", 11: "y"}) // D = 4
	if _, err := Lines(context.Background(), []byte(before), []byte(after), Limits{MaxEdits: 4}); err != nil {
		t.Fatalf("a distance equal to the limit is allowed: %v", err)
	}
	if _, err := Lines(context.Background(), []byte(before), []byte(after), Limits{MaxEdits: 3}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a distance over the limit is too large, got %v", err)
	}
}

func TestLinesHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var before, after strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&before, "b%d\n", i)
		fmt.Fprintf(&after, "a%d\n", i)
	}
	_, err := Lines(ctx, []byte(before.String()), []byte(after.String()), Limits{MaxEdits: 1 << 20})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestLinesOutputIsValidUTF8ForValidInput(t *testing.T) {
	res := mustLines(t, "héllo\nwörld\n日本\n", "héllo\nwörld!\n日本語\n", Limits{})
	if !strings.Contains(res.Patch, "+日本語\n") {
		t.Fatalf("multi-byte text must survive: %q", res.Patch)
	}
	for max := 1; max < len(res.Patch); max++ {
		cut := mustLines(t, "héllo\nwörld\n日本\n", "héllo\nwörld!\n日本語\n", Limits{MaxPatchBytes: max})
		if !validUTF8(cut.Patch) {
			t.Fatalf("max %d: invalid UTF-8 %q", max, cut.Patch)
		}
	}
}

func validUTF8(s string) bool { return strings.ToValidUTF8(s, "\ufffd") == s }

func TestLinesMatchesGitReference(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 25; i++ {
		before := randomText(r, 30)
		after := mutate(r, before)
		res := mustLines(t, before, after, Limits{MaxPatchBytes: 1 << 20})
		added, deleted, ok := gitNumstat(t, gitPath, dir, before, after)
		if ok && (res.Added != added || res.Deleted != deleted) {
			t.Fatalf("case %d: +%d -%d, git says +%d -%d\nbefore %q\nafter %q", i, res.Added, res.Deleted, added, deleted, before, after)
		}
	}
}

func gitNumstat(t *testing.T, gitPath, dir, before, after string) (int, int, bool) {
	t.Helper()
	for name, content := range map[string]string{"a.txt": before, "b.txt": after} {
		if err := os.WriteFile(dir+string(os.PathSeparator)+name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitPath, "-c", "core.autocrlf=false", "diff", "--no-index", "--no-ext-diff", "--minimal", "--numstat", "--", "a.txt", "b.txt")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		t.Fatalf("git diff: %v", err)
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		return 0, 0, before == after
	}
	a, errA := strconv.Atoi(f[0])
	d, errD := strconv.Atoi(f[1])
	return a, d, errA == nil && errD == nil
}
