package diff

import (
	"strconv"
	"strings"
)

const (
	contextLines = 3
	noNewline    = "\\ No newline at end of file\n"
)

// hunk is a half-open range of ops plus the line numbers it starts at.
type hunk struct {
	from, to        int
	beforeStart     int // lines of the before side consumed ahead of the hunk
	afterStart      int
	beforeN, afterN int
}

func render(ops []op, maxBytes int) Result {
	var res Result
	for _, o := range ops {
		switch o.kind {
		case '+':
			res.Added++
		case '-':
			res.Deleted++
		}
	}
	if res.Added+res.Deleted == 0 {
		return res
	}
	w := &patchWriter{max: maxBytes}
	for _, h := range hunks(ops) {
		if !w.writeHunk(ops, h) {
			res.Truncated = true
			break
		}
	}
	res.Patch = w.buf.String()
	return res
}

// hunks groups changes with context; changes separated by at most twice the
// context are merged, as Git does.
func hunks(ops []op) []hunk {
	var ranges [][2]int
	first, last := -1, -1
	flush := func() {
		if first >= 0 {
			ranges = append(ranges, [2]int{max(first-contextLines, 0), min(last+contextLines+1, len(ops))})
		}
	}
	for i, o := range ops {
		if o.kind == ' ' {
			continue
		}
		if first >= 0 && i-last-1 <= 2*contextLines {
			last = i
			continue
		}
		flush()
		first, last = i, i
	}
	flush()
	return describe(ops, ranges)
}

func describe(ops []op, ranges [][2]int) []hunk {
	out := make([]hunk, 0, len(ranges))
	before, after, at := 0, 0, 0
	for _, r := range ranges {
		for ; at < r[0]; at++ {
			before, after = advanceLines(before, after, ops[at].kind)
		}
		h := hunk{from: r[0], to: r[1], beforeStart: before, afterStart: after}
		for _, o := range ops[r[0]:r[1]] {
			h.beforeN, h.afterN = advanceLines(h.beforeN, h.afterN, o.kind)
		}
		out = append(out, h)
		for ; at < r[1]; at++ {
			before, after = advanceLines(before, after, ops[at].kind)
		}
	}
	return out
}

func advanceLines(before, after int, kind byte) (int, int) {
	if kind != '+' {
		before++
	}
	if kind != '-' {
		after++
	}
	return before, after
}

func span(start, n int) string {
	switch n {
	case 0:
		return strconv.Itoa(start) + ",0"
	case 1:
		return strconv.Itoa(start + 1)
	}
	return strconv.Itoa(start+1) + "," + strconv.Itoa(n)
}

// patchWriter appends whole units only, so the patch always ends on a line
// boundary and never exceeds max.
type patchWriter struct {
	buf strings.Builder
	max int
}

func (w *patchWriter) add(s string) bool {
	if w.buf.Len()+len(s) > w.max {
		return false
	}
	w.buf.WriteString(s)
	return true
}

func (w *patchWriter) writeHunk(ops []op, h hunk) bool {
	if !w.add("@@ -" + span(h.beforeStart, h.beforeN) + " +" + span(h.afterStart, h.afterN) + " @@\n") {
		return false
	}
	for _, o := range ops[h.from:h.to] {
		unit := string(o.kind) + o.text
		if !strings.HasSuffix(o.text, "\n") {
			unit += "\n" + noNewline
		}
		if !w.add(unit) {
			return false
		}
	}
	return true
}
