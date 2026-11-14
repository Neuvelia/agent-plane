package edit

import (
	"regexp"
	"strconv"
	"strings"

	"agentplane/internal/graph"

	"go.yaml.in/yaml/v3"
)

// DiagnosticLines resolves the 1-based source line of every diagnostic in text,
// from the YAML tree and the diagnostic's location. The result has one entry per
// diagnostic; 0 means the diagnostic concerns the graph as a whole or its element
// cannot be found. It reads the in-memory tree only and never fails: text that does
// not parse yields all zeros. The line is an editor aid and is not part of
// graph.Diagnostic or the CLI JSON.
func DiagnosticLines(text []byte, diags []graph.Diagnostic) []int {
	lines := make([]int, len(diags))
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return lines
	}
	top := docMapping(&doc)
	for i, d := range diags {
		lines[i] = lineOf(top, d.Location)
	}
	return lines
}

// lineOf prefers the deepest part of the path that exists, then the edge or
// binding index, then the node.
func lineOf(top *yaml.Node, loc graph.DiagnosticLocation) int {
	if loc.Path != "" {
		if n := deepest(top, loc.Path); n != nil {
			return n.Line
		}
	}
	if loc.EdgeIndex != nil {
		if n := indexed(top, "edges", *loc.EdgeIndex); n != nil {
			return n.Line
		}
	}
	if loc.GraphInputBindingIndex != nil {
		if n := indexed(top, "graph_input_bindings", *loc.GraphInputBindingIndex); n != nil {
			return n.Line
		}
	}
	if loc.NodeID != "" {
		if n := deepest(top, "nodes."+loc.NodeID); n != nil {
			return n.Line
		}
	}
	return 0
}

var segment = regexp.MustCompile(`^([^\[\]]+)(?:\[(\d+)\])?$`)

// deepest walks a dotted path with optional [n] indexes and returns the last
// node it could reach (the key for mapping entries, the item for list entries),
// or nil when not even the first segment exists.
func deepest(top *yaml.Node, path string) *yaml.Node {
	cur := top
	var found *yaml.Node
	for _, seg := range strings.Split(path, ".") {
		m := segment.FindStringSubmatch(seg)
		if m == nil {
			break
		}
		v, i := get(cur, m[1])
		if i < 0 {
			break
		}
		found = cur.Content[i] // the key line
		cur = v
		if m[2] != "" {
			n, _ := strconv.Atoi(m[2])
			if cur.Kind != yaml.SequenceNode || n >= len(cur.Content) {
				break
			}
			found = cur.Content[n]
			cur = found
		}
	}
	return found
}

func indexed(top *yaml.Node, key string, n int) *yaml.Node {
	seq, i := get(top, key)
	if i < 0 || seq.Kind != yaml.SequenceNode || n < 0 || n >= len(seq.Content) {
		return nil
	}
	return seq.Content[n]
}

var parseLine = regexp.MustCompile(`line (\d+)`)

// ParseErrorLine extracts the 1-based line a YAML or decoding error names, or 0.
// The YAML library reports lines only, so there is no column.
func ParseErrorLine(message string) int {
	m := parseLine.FindStringSubmatch(message)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
