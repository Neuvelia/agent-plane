package edit

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

var utf8BOM = []byte("\xef\xbb\xbf")

// encode writes the tree with two-space indentation, then puts back what the
// encoder drops or changes and is not part of the tree: blank lines above keys,
// a byte order mark and CRLF line endings. orig is the file the tree came from;
// renames maps old element paths to new ones so a renamed key keeps its blank line.
func encode(orig []byte, root *yaml.Node, renames [][2]string) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("encode graph: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode graph: %w", err)
	}
	out := restoreBlankLines(orig, buf.Bytes(), renames)
	return restoreEnvelope(orig, out), nil
}

func restoreEnvelope(orig, out []byte) []byte {
	if bytes.Contains(orig, []byte("\r\n")) {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	}
	if bytes.HasPrefix(orig, utf8BOM) && !bytes.HasPrefix(out, utf8BOM) {
		out = append(append([]byte{}, utf8BOM...), out...)
	}
	return out
}

// restoreBlankLines re-inserts a blank line above every key or sequence item that
// had one in the original (looking above the comment block directly over it),
// matching elements by path. Elements that did not exist before get none.
func restoreBlankLines(orig, out []byte, renames [][2]string) []byte {
	want := blankAbove(orig)
	if len(want) == 0 {
		return out
	}
	var doc yaml.Node
	if yaml.Unmarshal(out, &doc) != nil {
		return out
	}
	lines := strings.Split(string(out), "\n")
	insert := map[int]bool{}
	for path, line := range positions(&doc) {
		if !wantsBlank(want, path, renames) {
			continue
		}
		at := line - 1 // 0-based index of the key line
		for at > 0 && strings.HasPrefix(strings.TrimSpace(lines[at-1]), "#") {
			at--
		}
		if at > 0 && strings.TrimSpace(lines[at-1]) != "" {
			insert[at] = true // a set: several keys can share a line
		}
	}
	at := make([]int, 0, len(insert))
	for i := range insert {
		at = append(at, i)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(at)))
	for _, i := range at {
		lines = append(lines[:i], append([]string{""}, lines[i:]...)...)
	}
	return []byte(strings.Join(lines, "\n"))
}

// wantsBlank looks the path up as is, and under its old name if a rename moved it.
func wantsBlank(want map[string]bool, path string, renames [][2]string) bool {
	if want[path] {
		return true
	}
	for _, r := range renames {
		if path == r[1] || strings.HasPrefix(path, r[1]+".") || strings.HasPrefix(path, r[1]+"[") {
			if want[r[0]+strings.TrimPrefix(path, r[1])] {
				return true
			}
		}
	}
	return false
}

// positions maps every mapping key and sequence item path (`nodes.left`,
// `edges[0]`) to its 1-based line.
func positions(root *yaml.Node) map[string]int {
	out := map[string]int{}
	var walk func(n *yaml.Node, prefix string)
	walk = func(n *yaml.Node, prefix string) {
		switch n.Kind {
		case yaml.DocumentNode:
			for _, c := range n.Content {
				walk(c, prefix)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				p := n.Content[i].Value
				if prefix != "" {
					p = prefix + "." + p
				}
				out[p] = n.Content[i].Line
				walk(n.Content[i+1], p)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				p := fmt.Sprintf("%s[%d]", prefix, i)
				out[p] = c.Line
				walk(c, p)
			}
		}
	}
	walk(root, "")
	return out
}

func blankAbove(src []byte) map[string]bool {
	var doc yaml.Node
	out := map[string]bool{}
	if yaml.Unmarshal(src, &doc) != nil {
		return out
	}
	lines := strings.Split(string(src), "\n")
	for path, line := range positions(&doc) {
		at := line - 1
		for at > 0 && strings.HasPrefix(strings.TrimSpace(lines[at-1]), "#") {
			at--
		}
		if at > 0 && strings.TrimSpace(lines[at-1]) == "" {
			out[path] = true
		}
	}
	return out
}
