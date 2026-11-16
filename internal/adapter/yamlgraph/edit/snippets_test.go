package edit

import "testing"

const snippetGraph = `schema_version: 0
id: s
inputs: {}
nodes:
  a:
    type: shell
    config:
      command: "true"
      extra:
        deep: {list: [1, two]} # kept
        flag: yes
      quoted: 'off'
    loop:
      gate:
        config: {check_id: c, kind: json_predicate, later: [x]}
      bindings: []
edges: []
outputs: {}
`

func TestSnippetsReadConfigValuesFromTheTree(t *testing.T) {
	s := ReadSnippets([]byte(snippetGraph))
	cases := []struct {
		node     string
		loopGate bool
		key      string
		want     string
		found    bool
	}{
		{"a", false, "command", `"true"`, true},
		{"a", false, "quoted", `'off'`, true},
		{"a", false, "extra", "deep: {list: [1, two]} # kept\nflag: yes", true},
		{"a", true, "later", "[x]", true},
		{"a", false, "missing", "", false},
		{"ghost", false, "command", "", false},
		{"a", true, "command", "", false},
	}
	for _, c := range cases {
		got, found := s.Config(c.node, c.loopGate, c.key)
		if got != c.want || found != c.found {
			t.Errorf("Config(%s, %v, %s) = %q, %v; want %q, %v", c.node, c.loopGate, c.key, got, found, c.want, c.found)
		}
	}
	if _, found := ReadSnippets([]byte("id: [\n")).Config("a", false, "x"); found {
		t.Error("unparseable text found a value")
	}
}
