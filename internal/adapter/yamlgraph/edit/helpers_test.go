package edit

import (
	"agentplane/internal/adapter/yamlgraph"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

// repoGraphs are the example graphs the engine ships.
func repoGraphs(t *testing.T) map[string][]byte {
	t.Helper()
	return readAll(t, "../../../../.agents/graphs/*.yaml")
}

// fixtures are the E0 spike fixtures (hostile, CRLF, BOM, long lines, comments).
func fixtures(t *testing.T) map[string][]byte {
	t.Helper()
	return readAll(t, "testdata/*.yaml")
}

func readAll(t *testing.T, pattern string) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil || len(paths) == 0 {
		t.Fatalf("no files for %s", pattern)
	}
	out := map[string][]byte{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(p)] = data
	}
	return out
}

func mustApply(t *testing.T, src string, ops ...Op) string {
	t.Helper()
	res, err := Apply([]byte(src), ops)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return string(res.Text)
}

func applyErr(t *testing.T, src string, ops ...Op) *Error {
	t.Helper()
	_, err := Apply([]byte(src), ops)
	if err == nil {
		t.Fatalf("Apply succeeded, expected an error")
	}
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *Error: %v", err, err)
	}
	return e
}

// lineDiff returns the lines removed from a and added in b (an LCS diff; the
// graphs are small).
func lineDiff(a, b string) (removed, added []string) {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			removed = append(removed, x[i])
			i++
		default:
			added = append(added, y[j])
			j++
		}
	}
	removed = append(removed, x[i:]...)
	added = append(added, y[j:]...)
	return removed, added
}

func parse(t *testing.T, src string) *graph.Definition {
	t.Helper()
	def, err := yamlgraph.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	return def
}

func version(t *testing.T, src string) string {
	t.Helper()
	v, err := yamlgraph.SemanticVersion(parse(t, src))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func ptr(s string) *string { return &s }

// twoNodes is a small valid graph with unbound ports, in block style.
const twoNodes = `schema_version: 0
id: pair
inputs: {}

nodes:
  a:
    type: shell
    config:
      command: "true"
    outputs:
      stdout: {type: string}
      count: {type: number}

  b:
    type: shell
    config:
      command: "true"
    inputs:
      text: {type: string}
      other: {type: string}

edges: []
outputs: {}
`
