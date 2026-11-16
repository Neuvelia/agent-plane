package edit

import (
	"bytes"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Snippets reads config values of a document as YAML text, for the
// inspector's Advanced section (F2.2 requirement 4). It reads the in-memory
// tree only and never changes the document.
type Snippets struct {
	top *yaml.Node
}

// ReadSnippets parses text once. Text that does not parse yields Snippets that
// find nothing.
func ReadSnippets(text []byte) Snippets {
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return Snippets{}
	}
	return Snippets{top: docMapping(&doc)}
}

// Config returns the YAML text of the value under key in node's config (or,
// with loopGate, in its loop's embedded Gate config), and whether it exists.
// The value is encoded from the parsed tree with its own style, nesting and
// comments; the document text itself is never re-encoded.
func (s Snippets) Config(node string, loopGate bool, key string) (string, bool) {
	n, _ := get(s.top, "nodes")
	n, _ = get(n, node)
	if loopGate {
		n, _ = get(n, "loop")
		n, _ = get(n, "gate")
	}
	n, _ = get(n, "config")
	v, i := get(n, key)
	if i < 0 {
		return "", false
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	if err := enc.Close(); err != nil {
		return "", false
	}
	return strings.TrimSuffix(buf.String(), "\n"), true
}
