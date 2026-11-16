// Command yaml-fidelity is the E0 spike of the graph editor plan: it measures
// what survives when a graph file is edited through the yaml.Node tree instead of
// Parse/Marshal. It is a measurement tool, not product code: nothing imports it.
//
//	go run ./experiments/yaml-fidelity [graph.yaml ...]
//
// With no arguments it measures the repository's example graphs and the fixtures
// in testdata/ (hostile constructs, CRLF, BOM, long lines). It prints a markdown report. Needs the `diff` binary. NO_BLANKS=1
// measures the plain encoder; SHOW_DIFF=1 prints each diff.
package main

import (
	"agentplane/internal/adapter/yamlgraph"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"agentplane/internal/domain/graph"

	"go.yaml.in/yaml/v3"
)

// op is one editor action, expressed twice: on the YAML tree (what the editor
// would do to the file) and on the typed Definition (what it MEANS). The safety
// net of the plan compares the semantic version of the patched file with the one
// the typed edit predicts.
type op struct {
	name     string
	tree     func(root *yaml.Node) error
	def      func(d *graph.Definition) error
	sameSem  bool // the edit must not change the semantic version
	skipWhen func(d *graph.Definition) string
}

func main() {
	paths := os.Args[1:]
	if len(paths) == 0 {
		paths, _ = filepath.Glob(".agents/graphs/*.yaml")
		fixtures, _ := filepath.Glob("experiments/yaml-fidelity/testdata/*.yaml")
		paths = append(paths, fixtures...)
	}
	fmt.Println("# YAML fidelity spike (E0)")
	fmt.Println()
	for _, p := range paths {
		report(p)
	}
}

func report(path string) {
	orig, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("## %s\n\nunreadable: %v\n\n", path, err)
		return
	}
	fmt.Printf("## %s\n\n", path)
	def, parseErr := yamlgraph.Parse(orig)
	if parseErr != nil {
		fmt.Printf("`graph.Parse` refuses this file: %v\n\n", parseErr)
		fmt.Println("Tree patching is measured anyway (identity and edits) to show the behaviour, but the editor must not offer canvas edits for a file the engine cannot read.")
		fmt.Println()
	}
	var root yaml.Node
	if err := yaml.Unmarshal(orig, &root); err != nil {
		fmt.Printf("not YAML: %v\n\n", err)
		return
	}
	fmt.Printf("Unpatchable constructs (editor opens canvas read-only): %s\n\n", unpatchable(&root))
	fmt.Println("| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, o := range ops(def) {
		measure(path, orig, def, o)
	}
	fmt.Println()
}

func ops(def *graph.Definition) []op {
	first := ""
	if def != nil {
		first = firstNode(def)
	}
	return []op{
		{name: "identity (decode, encode)", tree: func(*yaml.Node) error { return nil }, def: func(*graph.Definition) error { return nil }, sameSem: true},
		{name: "set `config.command` of the first node", tree: func(r *yaml.Node) error { return setScalar(r, "changed", "nodes", first, "config", "command") },
			def: func(d *graph.Definition) error {
				n := d.Nodes[first]
				n.Config = copyConfig(n.Config)
				n.Config["command"] = "changed"
				d.Nodes[first] = n
				return nil
			}, skipWhen: noFirstCommand},
		{name: "replace `args` of the first node", tree: func(r *yaml.Node) error {
			n := path(r, "nodes", first, "config", "args")
			if n == nil {
				return fmt.Errorf("no config.args")
			}
			n.Content = []*yaml.Node{str("changed")}
			return nil
		}, def: func(d *graph.Definition) error {
			n := d.Nodes[first]
			n.Config = copyConfig(n.Config)
			n.Config["args"] = []any{"changed"}
			d.Nodes[first] = n
			return nil
		}, skipWhen: func(d *graph.Definition) string {
			if _, ok := d.Nodes[firstNode(d)].Config["args"]; !ok {
				return "first node has no config.args"
			}
			return ""
		}},
		{name: "add a node (with a head comment)", tree: addNode, def: func(d *graph.Definition) error {
			d.Nodes["spike_added"] = graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "true"}}
			return nil
		}},
		{name: "add an edge", tree: func(r *yaml.Node) error {
			return appendEdge(r, "spike_a.outputs.x", "spike_b.inputs.y")
		}, def: func(d *graph.Definition) error {
			d.Edges = append(d.Edges, graph.EdgeDefinition{From: "spike_a.outputs.x", To: "spike_b.inputs.y"})
			return nil
		}},
		{name: "remove a leaf node", tree: func(r *yaml.Node) error { return removeLeaf(r, def) }, def: func(d *graph.Definition) error {
			id := leaf(d)
			delete(d.Nodes, id)
			d.Edges = dropEdges(d.Edges, id)
			return nil
		}, skipWhen: func(d *graph.Definition) string {
			if leaf(d) == "" {
				return "no leaf node"
			}
			return ""
		}},
		{name: "move a node (create `metadata.layout`)", tree: func(r *yaml.Node) error { return setLayout(r, first, 120, 80) }, def: func(d *graph.Definition) error {
			d.Metadata = graph.Metadata{"layout": map[string]any{"nodes": map[string]any{first: map[string]any{"x": 120, "y": 80}}}}
			return nil
		}, sameSem: true},
		{name: "move the same node again", tree: func(r *yaml.Node) error {
			if err := setLayout(r, first, 120, 80); err != nil {
				return err
			}
			return setLayout(r, first, 200, 90)
		}, def: func(d *graph.Definition) error { return nil }, sameSem: true},
		{name: "rename a node (key and references)", tree: func(r *yaml.Node) error { return rename(r, first, first+"_renamed") }, def: func(d *graph.Definition) error {
			return renameDef(d, first, first+"_renamed")
		}},
	}
}

func measure(path string, orig []byte, def *graph.Definition, o op) {
	if o.skipWhen != nil && def != nil {
		if why := o.skipWhen(def); why != "" {
			fmt.Printf("| %s | skipped: %s | | | | | | |\n", o.name, why)
			return
		}
	}
	var root yaml.Node
	if err := yaml.Unmarshal(orig, &root); err != nil {
		return
	}
	if err := o.tree(&root); err != nil {
		fmt.Printf("| %s | op failed: %v | | | | | | |\n", o.name, err)
		return
	}
	var patched []byte
	var err error
	if os.Getenv("NO_BLANKS") == "" {
		patched, err = encodeKeepingBlankLines(orig, &root)
	} else {
		patched, err = encode(&root)
	}
	if err != nil {
		fmt.Printf("| %s | encode failed: %v | | | | | | |\n", o.name, err)
		return
	}
	lost := lostComments(orig, patched)
	var re, sem string
	if got, perr := yamlgraph.Parse(patched); perr != nil {
		re, sem = "no: "+short(perr.Error()), "n/a"
	} else {
		re = "yes"
		sem = semantic(orig, def, got, o)
	}
	fmt.Printf("| %s | %d | %s | %d > %d | %s | %d > %d | %s | %s |\n", o.name, diffLines(orig, patched), lostText(lost), blank(orig), blank(patched),
		keyOrder(orig, patched, o.name), styled(orig), styled(patched), re, sem)
	if os.Getenv("SHOW_DIFF") != "" && o.name != "identity (decode, encode)" || o.name == "identity (decode, encode)" && os.Getenv("SHOW_DIFF") != "" {
		fmt.Printf("\n```diff\n%s```\n\n", unified(orig, patched))
	}
}

// semantic reports whether the patched file's semantic version is what the typed
// edit predicts: unchanged for identity and layout, equal to the typed result otherwise.
func semantic(orig []byte, def, got *graph.Definition, o op) string {
	if def == nil {
		return "n/a (original not parseable)"
	}
	base, _ := yamlgraph.SemanticVersion(def)
	have, err := yamlgraph.SemanticVersion(got)
	if err != nil {
		return "error"
	}
	if o.sameSem {
		if have == base {
			return "yes (unchanged)"
		}
		return "NO (changed)"
	}
	want := cloneDef(orig)
	if want == nil || o.def(want) != nil {
		return "n/a"
	}
	wantV, _ := yamlgraph.SemanticVersion(want)
	if have == wantV {
		return "yes (matches typed edit)"
	}
	return "NO (differs from typed edit)"
}

func cloneDef(src []byte) *graph.Definition {
	d, err := yamlgraph.Parse(src)
	if err != nil {
		return nil
	}
	return d
}

// ---- tree helpers -----------------------------------------------------------

func mapping(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return root.Content[0]
	}
	return root
}

func get(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func path(root *yaml.Node, keys ...string) *yaml.Node {
	n := mapping(root)
	for _, k := range keys {
		if n == nil {
			return nil
		}
		n = get(n, k)
	}
	return n
}

func setScalar(root *yaml.Node, value string, keys ...string) error {
	n := path(root, keys...)
	if n == nil || n.Kind != yaml.ScalarNode {
		return fmt.Errorf("no scalar at %s", strings.Join(keys, "."))
	}
	n.Value = value
	n.Style = 0
	n.Tag = "!!str"
	return nil
}

func addNode(root *yaml.Node) error {
	nodes := path(root, "nodes")
	if nodes == nil || nodes.Kind != yaml.MappingNode {
		return fmt.Errorf("no nodes mapping")
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "spike_added", HeadComment: "added by the editor"}
	val := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		str("type"), str("shell"),
		str("config"), {Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("command"), str("true")}},
	}}
	nodes.Content = append(nodes.Content, key, val)
	return nil
}

func appendEdge(root *yaml.Node, from, to string) error {
	edges := path(root, "edges")
	if edges == nil || edges.Kind != yaml.SequenceNode {
		return fmt.Errorf("no edges sequence")
	}
	edge := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{str("from"), str(from), str("to"), str(to)}}
	// An empty flow sequence (`edges: []`) must become a block sequence to stay readable.
	if len(edges.Content) == 0 {
		edges.Style &^= yaml.FlowStyle
	}
	edges.Content = append(edges.Content, edge)
	return nil
}

func removeLeaf(root *yaml.Node, def *graph.Definition) error {
	if def == nil {
		return fmt.Errorf("no definition")
	}
	id := leaf(def)
	nodes := path(root, "nodes")
	for i := 0; i+1 < len(nodes.Content); i += 2 {
		if nodes.Content[i].Value == id {
			nodes.Content = append(nodes.Content[:i], nodes.Content[i+2:]...)
			break
		}
	}
	if edges := path(root, "edges"); edges != nil {
		kept := edges.Content[:0]
		for _, e := range edges.Content {
			if !touches(e, id) {
				kept = append(kept, e)
			}
		}
		edges.Content = kept
	}
	return nil
}

func touches(edge *yaml.Node, id string) bool {
	for _, k := range []string{"from", "to"} {
		if v := get(edge, k); v != nil && strings.HasPrefix(v.Value, id+".") {
			return true
		}
	}
	return false
}

func setLayout(root *yaml.Node, id string, x, y int) error {
	m := mapping(root)
	meta := get(m, "metadata")
	if meta == nil {
		meta = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		// Insert before `inputs` (after id/name) rather than at the end of the file.
		at := len(m.Content)
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == "inputs" {
				at = i
				break
			}
		}
		pair := []*yaml.Node{str("metadata"), meta}
		m.Content = append(m.Content[:at], append(pair, m.Content[at:]...)...)
	}
	layout := get(meta, "layout")
	if layout == nil {
		layout = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		meta.Content = append(meta.Content, str("layout"), layout)
	}
	nodes := get(layout, "nodes")
	if nodes == nil {
		nodes = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		layout.Content = append(layout.Content, str("nodes"), nodes)
	}
	pos := get(nodes, id)
	if pos == nil {
		pos = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
		nodes.Content = append(nodes.Content, str(id), pos)
	}
	pos.Content = []*yaml.Node{str("x"), num(x), str("y"), num(y)}
	return nil
}

func rename(root *yaml.Node, from, to string) error {
	nodes := path(root, "nodes")
	if nodes == nil {
		return fmt.Errorf("no nodes")
	}
	found := false
	for i := 0; i+1 < len(nodes.Content); i += 2 {
		if nodes.Content[i].Value == from {
			nodes.Content[i].Value = to
			found = true
		}
	}
	if !found {
		return fmt.Errorf("no node %s", from)
	}
	rewrite := func(n *yaml.Node, keys ...string) {
		if n == nil {
			return
		}
		for _, item := range n.Content {
			for _, k := range keys {
				if v := get(item, k); v != nil && strings.HasPrefix(v.Value, from+".") {
					v.Value = to + strings.TrimPrefix(v.Value, from)
				}
			}
		}
	}
	rewrite(path(root, "edges"), "from", "to")
	rewrite(path(root, "graph_input_bindings"), "from", "to")
	if outs := path(root, "outputs"); outs != nil {
		for i := 1; i < len(outs.Content); i += 2 {
			if v := get(outs.Content[i], "from"); v != nil && strings.HasPrefix(v.Value, from+".") {
				v.Value = to + strings.TrimPrefix(v.Value, from)
			}
		}
	}
	if units := path(root, "units"); units != nil {
		for _, u := range units.Content {
			if u.Value == from {
				u.Value = to
			}
		}
	}
	return nil
}

func str(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
func num(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(n)}
}

func encode(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}

// encodeKeepingBlankLines encodes like encode, then puts back the blank lines the
// encoder drops: a blank line that sat above a key (or sequence item, and above
// the comment block directly over it) in the original is re-inserted above the
// same key path in the output. Keys that did not exist before get none.
func encodeKeepingBlankLines(orig []byte, root *yaml.Node) ([]byte, error) {
	out, err := encode(root)
	if err != nil {
		return nil, err
	}
	want := blankAbove(orig)
	var node yaml.Node
	if yaml.Unmarshal(out, &node) != nil {
		return out, nil
	}
	lines := strings.Split(string(out), "\n")
	var inserts []int
	for path, line := range positions(&node) {
		if !want[path] {
			continue
		}
		at := line - 1 // 0-based index of the key line
		for at > 0 && strings.HasPrefix(strings.TrimSpace(lines[at-1]), "#") {
			at--
		}
		if at > 0 && strings.TrimSpace(lines[at-1]) != "" {
			inserts = append(inserts, at)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(inserts)))
	for _, at := range inserts {
		lines = append(lines[:at], append([]string{""}, lines[at:]...)...)
	}
	return restoreEnvelope(orig, []byte(strings.Join(lines, "\n"))), nil
}

// restoreEnvelope re-applies what is not part of the YAML tree: a UTF-8 byte order
// mark and CRLF line endings. Without it a CRLF file would be rewritten line by line.
func restoreEnvelope(orig, out []byte) []byte {
	if bytes.Contains(orig, []byte("\r\n")) {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	}
	if bytes.HasPrefix(orig, []byte("\xef\xbb\xbf")) && !bytes.HasPrefix(out, []byte("\xef\xbb\xbf")) {
		out = append([]byte("\xef\xbb\xbf"), out...)
	}
	return out
}

// positions maps a path (`nodes.left`, `edges[0]`) to the 1-based line of its key
// or item, for every mapping key and sequence item below the top level.
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

// blankAbove reports, per path, whether a blank line sits above the key (skipping
// the comment block directly over it).
func blankAbove(src []byte) map[string]bool {
	var node yaml.Node
	out := map[string]bool{}
	if yaml.Unmarshal(src, &node) != nil {
		return out
	}
	lines := strings.Split(string(src), "\n")
	for path, line := range positions(&node) {
		at := line - 1
		for at > 0 && strings.HasPrefix(strings.TrimSpace(lines[at-1]), "#") {
			at--
		}
		out[path] = at > 0 && strings.TrimSpace(lines[at-1]) == ""
	}
	return out
}

// ---- definition helpers -----------------------------------------------------

func firstNode(d *graph.Definition) string {
	ids := make([]string, 0, len(d.Nodes))
	for id := range d.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func noFirstCommand(d *graph.Definition) string {
	if _, ok := d.Nodes[firstNode(d)].Config["command"]; !ok {
		return "first node has no config.command"
	}
	return ""
}

func copyConfig(c graph.NodeConfig) graph.NodeConfig {
	out := graph.NodeConfig{}
	for k, v := range c {
		out[k] = v
	}
	return out
}

func leaf(d *graph.Definition) string {
	hasDownstream := map[string]bool{}
	for _, e := range d.Edges {
		hasDownstream[strings.SplitN(string(e.From), ".", 2)[0]] = true
	}
	ids := make([]string, 0)
	for id := range d.Nodes {
		if !hasDownstream[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 || len(d.Nodes) < 2 {
		return ""
	}
	return ids[len(ids)-1]
}

func dropEdges(edges []graph.EdgeDefinition, id string) []graph.EdgeDefinition {
	var out []graph.EdgeDefinition
	for _, e := range edges {
		if strings.HasPrefix(string(e.From), id+".") || strings.HasPrefix(string(e.To), id+".") {
			continue
		}
		out = append(out, e)
	}
	return out
}

func renameDef(d *graph.Definition, from, to string) error {
	n, ok := d.Nodes[from]
	if !ok {
		return fmt.Errorf("no node %s", from)
	}
	delete(d.Nodes, from)
	d.Nodes[to] = n
	fix := func(s string) string {
		if strings.HasPrefix(s, from+".") {
			return to + strings.TrimPrefix(s, from)
		}
		return s
	}
	for i := range d.Edges {
		d.Edges[i].From, d.Edges[i].To = graph.NodeOutputReference(fix(string(d.Edges[i].From))), graph.NodeInputReference(fix(string(d.Edges[i].To)))
	}
	for i := range d.GraphInputBindings {
		d.GraphInputBindings[i].To = graph.NodeInputReference(fix(string(d.GraphInputBindings[i].To)))
	}
	for k, o := range d.Outputs {
		o.From = graph.NodeOutputReference(fix(string(o.From)))
		d.Outputs[k] = o
	}
	for i, u := range d.Units {
		if u == from {
			d.Units[i] = to
		}
	}
	return nil
}

// ---- measurements -----------------------------------------------------------

func unpatchable(root *yaml.Node) string {
	var found []string
	seen := map[string]bool{}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		add := func(s string) {
			if !seen[s] {
				seen[s] = true
				found = append(found, s)
			}
		}
		if n.Kind == yaml.AliasNode {
			add("alias")
		}
		if n.Anchor != "" {
			add("anchor")
		}
		if n.Kind == yaml.ScalarNode && n.Value == "<<" && n.Tag == "!!merge" {
			add("merge key")
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
	if len(found) == 0 {
		return "none"
	}
	return strings.Join(found, ", ")
}

func comments(src []byte) []string {
	var out []string
	for _, line := range strings.Split(string(src), "\n") {
		if i := commentStart(line); i >= 0 {
			out = append(out, strings.TrimSpace(line[i:]))
		}
	}
	return out
}

// commentStart finds a `#` that begins a comment (outside quotes).
func commentStart(line string) int {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return i
		}
	}
	return -1
}

func lostComments(orig, patched []byte) []string {
	have := string(patched)
	var lost []string
	for _, c := range comments(orig) {
		if !strings.Contains(have, c) {
			lost = append(lost, c)
		}
	}
	return lost
}

func lostText(l []string) string {
	if len(l) == 0 {
		return "0"
	}
	return fmt.Sprintf("%d (%s)", len(l), short(strings.Join(l, " / ")))
}

func blank(src []byte) int {
	n := 0
	for _, line := range strings.Split(string(src), "\n") {
		if strings.TrimSpace(line) == "" {
			n++
		}
	}
	return n
}

func styled(src []byte) int {
	var root yaml.Node
	if yaml.Unmarshal(src, &root) != nil {
		return -1
	}
	n := 0
	var walk func(*yaml.Node)
	walk = func(x *yaml.Node) {
		if x.Style&(yaml.FlowStyle|yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			n++
		}
		for _, c := range x.Content {
			walk(c)
		}
	}
	walk(&root)
	return n
}

func orderOf(src []byte) []string {
	var root yaml.Node
	if yaml.Unmarshal(src, &root) != nil {
		return nil
	}
	m := mapping(&root)
	var out []string
	for i := 0; i+1 < len(m.Content); i += 2 {
		out = append(out, m.Content[i].Value)
		if m.Content[i].Value == "nodes" && m.Content[i+1].Kind == yaml.MappingNode {
			for j := 0; j+1 < len(m.Content[i+1].Content); j += 2 {
				out = append(out, "nodes."+m.Content[i+1].Content[j].Value)
			}
		}
	}
	return out
}

// keyOrder compares the order of the keys both files share (top level and nodes),
// so an added, removed, renamed or inserted key does not count as a reorder.
func keyOrder(orig, patched []byte, _ string) string {
	a, b := orderOf(orig), orderOf(patched)
	inB := map[string]bool{}
	for _, k := range b {
		inB[k] = true
	}
	inA := map[string]bool{}
	for _, k := range a {
		inA[k] = true
	}
	var as, bs []string
	for _, k := range a {
		if inB[k] {
			as = append(as, k)
		}
	}
	for _, k := range b {
		if inA[k] {
			bs = append(bs, k)
		}
	}
	if reflect.DeepEqual(as, bs) {
		return "yes"
	}
	return "NO"
}

func diffLines(a, b []byte) int {
	out := unified(a, b)
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if (strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-")) && !strings.HasPrefix(l, "+++") && !strings.HasPrefix(l, "---") {
			n++
		}
	}
	return n
}

func unified(a, b []byte) string {
	dir, err := os.MkdirTemp("", "yamlfid")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	pa, pb := filepath.Join(dir, "before"), filepath.Join(dir, "after")
	_ = os.WriteFile(pa, a, 0o644)
	_ = os.WriteFile(pb, b, 0o644)
	out, _ := exec.Command("diff", "-u", pa, pb).Output()
	return string(out)
}

func short(s string) string {
	s = strings.ReplaceAll(s, "|", "/")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
