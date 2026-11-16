package edit

import (
	"regexp"
	"strconv"
	"strings"

	"agentplane/internal/domain/graph"

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

var parseLine = regexp.MustCompile(`line (\d+)(?:: ([^\n]*))?`)

// firstParseEntry returns the line and the entry text of the first `line N`
// the message names; line and column must come from the same entry.
func firstParseEntry(message string) (line int, entry string) {
	m := parseLine.FindStringSubmatch(message)
	if m == nil {
		return 0, ""
	}
	line, _ = strconv.Atoi(m[1])
	return line, m[2]
}

// ParseErrorLine extracts the 1-based line a YAML or decoding error names, or 0.
func ParseErrorLine(message string) int {
	line, _ := firstParseEntry(message)
	return line
}

var (
	unknownField    = regexp.MustCompile(`^field (.+) not found in type `)
	cannotUnmarshal = regexp.MustCompile("^cannot unmarshal (!!\\w+)(?: (`.*`))? into ")
)

// ParseErrorColumn returns the 1-based column (in characters) of the node a
// decoding error names, or 0 when the library does not allow it. yaml.v3 gives a
// column for no error: syntax errors name a line only, and decoding errors carry
// the line in text. For the decoding errors that name the failing node (unknown
// field, value of the wrong kind or type) the column is read from that node in
// the YAML tree; it is reported only when exactly one node on the named line
// fits the message, never guessed.
func ParseErrorColumn(text []byte, message string) int {
	line, entry := firstParseEntry(message)
	if line == 0 {
		return 0
	}
	fits := fitter(line, entry)
	if fits == nil {
		return 0
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return 0
	}
	var found []*yaml.Node
	var walk func(n *yaml.Node, isKey bool)
	walk = func(n *yaml.Node, isKey bool) {
		if fits(n, isKey) {
			found = append(found, n)
		}
		for i, c := range n.Content {
			walk(c, n.Kind == yaml.MappingNode && i%2 == 0)
		}
	}
	walk(&doc, false)
	if len(found) != 1 {
		return 0
	}
	return found[0].Column
}

// fitter builds the predicate for the node a decoding error entry names, or nil
// for entries (syntax errors, duplicate keys) whose node cannot be identified.
func fitter(line int, entry string) func(n *yaml.Node, isKey bool) bool {
	if m := unknownField.FindStringSubmatch(entry); m != nil {
		return func(n *yaml.Node, isKey bool) bool {
			return isKey && n.Line == line && n.Value == m[1]
		}
	}
	if m := cannotUnmarshal.FindStringSubmatch(entry); m != nil {
		return func(n *yaml.Node, _ bool) bool {
			if n.Kind == yaml.DocumentNode || n.Line != line || n.ShortTag() != m[1] {
				return false
			}
			if n.Kind != yaml.ScalarNode {
				return m[2] == ""
			}
			return m[2] == shortValue(n.Value)
		}
	}
	return nil
}

// shortValue renders a scalar the way yaml.v3 quotes it in a decoding error.
func shortValue(v string) string {
	if len(v) > 10 {
		return "`" + v[:7] + "...`"
	}
	return "`" + v + "`"
}
