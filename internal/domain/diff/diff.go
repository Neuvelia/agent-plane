package diff

import (
	"context"
	"errors"
	"fmt"
)

const (
	// DefaultMaxPatchBytes applies when Limits.MaxPatchBytes is not positive.
	DefaultMaxPatchBytes = 16 << 10
	// DefaultMaxEdits applies when Limits.MaxEdits is not positive.
	DefaultMaxEdits = 2048
)

// ErrTooLarge means the edit distance exceeded Limits.MaxEdits; no patch is produced.
var ErrTooLarge = errors.New("diff exceeds edit limit")

// Limits bound the work and the output. Non-positive fields take the defaults.
type Limits struct {
	MaxPatchBytes int
	MaxEdits      int
}

// Result is a unified patch without a file header. Added and Deleted count
// every changed line, whether or not the patch was truncated.
type Result struct {
	Patch     string
	Added     int
	Deleted   int
	Truncated bool
}

func (l Limits) withDefaults() Limits {
	if l.MaxPatchBytes <= 0 {
		l.MaxPatchBytes = DefaultMaxPatchBytes
	}
	if l.MaxEdits <= 0 {
		l.MaxEdits = DefaultMaxEdits
	}
	return l
}

type op struct {
	kind byte // ' ' unchanged, '-' deleted, '+' inserted
	text string
}

// Lines diffs two byte sequences by "\n"-separated lines (a CRLF stays part of
// its line). The result is deterministic and minimal in changed lines.
func Lines(ctx context.Context, before, after []byte, l Limits) (Result, error) {
	l = l.withDefaults()
	a, b := splitLines(before), splitLines(after)
	pre, suf := commonAffixes(a, b)
	middle, err := shortestEdit(ctx, a[pre:len(a)-suf], b[pre:len(b)-suf], l.MaxEdits)
	if err != nil {
		return Result{}, err
	}
	ops := make([]op, 0, pre+len(middle)+suf)
	for _, line := range a[:pre] {
		ops = append(ops, op{' ', line})
	}
	ops = append(ops, middle...)
	for _, line := range a[len(a)-suf:] {
		ops = append(ops, op{' ', line})
	}
	return render(ops, l.MaxPatchBytes), nil
}

// splitLines keeps each line's terminator, so a final line without "\n" stays
// distinguishable from one with it.
func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, c := range data {
		if c == '\n' {
			lines = append(lines, string(data[start:i+1]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}

func commonAffixes(a, b []string) (prefix, suffix int) {
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return prefix, suffix
}

// frontier is the Myers furthest-reaching x per diagonal k, stored at k+off.
type frontier struct {
	x   []int
	off int
}

// snapshot copies diagonals -d-1..d+1 (the ones round d reads).
func (f *frontier) snapshot(d int) []int {
	return append([]int(nil), f.x[f.off-d-1:f.off+d+2]...)
}

func (f *frontier) pickStart(d, k int) int {
	if k == -d || (k != d && f.x[f.off+k-1] < f.x[f.off+k+1]) {
		return f.x[f.off+k+1]
	}
	return f.x[f.off+k-1] + 1
}

// advance runs round d and reports whether the end of both sequences was reached.
func (f *frontier) advance(d int, a, b []string) bool {
	for k := -d; k <= d; k += 2 {
		x := f.pickStart(d, k)
		y := x - k
		for x < len(a) && y < len(b) && a[x] == b[y] {
			x++
			y++
		}
		f.x[f.off+k] = x
		if x >= len(a) && y >= len(b) {
			return true
		}
	}
	return false
}

// shortestEdit is Myers' O(ND) algorithm. Memory is O(D^2) snapshots, which
// maxEdits bounds; ctx is polled once per round.
func shortestEdit(ctx context.Context, a, b []string, maxEdits int) ([]op, error) {
	if len(a) == 0 && len(b) == 0 {
		return nil, nil
	}
	limit := min(len(a)+len(b), maxEdits)
	f := &frontier{x: make([]int, 2*limit+3), off: limit + 1}
	trace := make([][]int, 0, 16)
	for d := 0; d <= limit; d++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("diffing lines: %w", err)
		}
		trace = append(trace, f.snapshot(d))
		if f.advance(d, a, b) {
			return backtrack(trace, a, b), nil
		}
	}
	return nil, ErrTooLarge
}

func backtrack(trace [][]int, a, b []string) []op {
	var rev []op
	x, y := len(a), len(b)
	for d := len(trace) - 1; d >= 0; d-- {
		at := func(k int) int { return trace[d][k+d+1] }
		k := x - y
		prevK := k - 1
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			prevK = k + 1
		}
		prevX := at(prevK)
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			rev = append(rev, op{' ', a[x]})
		}
		if d > 0 {
			if x == prevX {
				rev = append(rev, op{'+', b[prevY]})
			} else {
				rev = append(rev, op{'-', a[prevX]})
			}
		}
		x, y = prevX, prevY
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}
