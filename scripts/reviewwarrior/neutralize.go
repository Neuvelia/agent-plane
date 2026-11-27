package main

import (
	"regexp"
	"strings"
)

// The model's review is untrusted text published under the workflow's
// identity. Outside code, GitHub would turn `@name` into a notification and
// `<tag>` into HTML (images that call out, hidden comments, details blocks
// that hide a finding). Inside code spans and fences both are inert and are
// left as written, so file paths and snippets stay exact.

var mention = regexp.MustCompile(`@([A-Za-z0-9])`)

// zeroWidthSpace breaks a mention without changing how the text reads.
const zeroWidthSpace = "&#8203;"

// neutralize escapes mentions and HTML outside Markdown code.
func neutralize(text string) string {
	lines := strings.Split(text, "\n")
	fence := ""
	for i, line := range lines {
		if marker := fenceMarker(line); marker != "" {
			fence = toggleFence(fence, marker)
			continue
		}
		if fence == "" {
			lines[i] = neutralizeLine(line)
		}
	}
	return strings.Join(lines, "\n")
}

var fenceLine = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

func fenceMarker(line string) string {
	match := fenceLine.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	return match[1]
}

// toggleFence opens a fence, or closes the open one when marker is made of
// the same character and is at least as long. Anything else is fence content.
func toggleFence(open, marker string) string {
	if open == "" {
		return marker
	}
	if marker[0] == open[0] && len(marker) >= len(open) {
		return ""
	}
	return open
}

// neutralizeLine escapes the text between inline code spans. An unclosed
// backtick run is ordinary text, so its tail is escaped too.
func neutralizeLine(line string) string {
	var out strings.Builder
	rest := line
	for {
		start := strings.Index(rest, "`")
		if start < 0 {
			out.WriteString(escapeText(rest))
			return out.String()
		}
		run := backtickRunAt(rest[start:])
		closing := strings.Index(rest[start+len(run):], run)
		if closing < 0 {
			out.WriteString(escapeText(rest))
			return out.String()
		}
		end := start + len(run) + closing + len(run)
		out.WriteString(escapeText(rest[:start]))
		out.WriteString(rest[start:end])
		rest = rest[end:]
	}
}

func backtickRunAt(text string) string {
	return text[:len(text)-len(strings.TrimLeft(text, "`"))]
}

func escapeText(text string) string {
	text = strings.ReplaceAll(text, "<", "&lt;")
	return mention.ReplaceAllString(text, "@"+zeroWidthSpace+"$1")
}
