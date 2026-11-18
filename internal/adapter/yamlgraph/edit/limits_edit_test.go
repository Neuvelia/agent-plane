package edit

import (
	"strings"
	"testing"
)

const limitedDoc = `schema_version: 0
id: limited
limits:
  run_timeout_seconds: 600
inputs: {}
nodes:
  n:
    type: shell
    timeout_seconds: 30
    config: {command: /bin/true}
    outputs: {stdout: {type: string}}
edges: []
outputs:
  r: {from: n.outputs.stdout}
`

func TestEditorSavePreservesLimits(t *testing.T) {
	for name, ops := range map[string][]Op{
		"no ops":     nil,
		"presenting": {{Op: OpSetName, Name: ptr("renamed")}, {Op: OpSetLayout, Nodes: map[string]Position{"n": {X: 1, Y: 2}}}},
		"add node":   {{Op: OpAddNode, ID: "extra", Type: "shell"}},
		"set config": {{Op: OpSetConfig, Node: "n", Path: []string{"command"}, Value: []byte(`"/bin/echo"`)}},
	} {
		t.Run(name, func(t *testing.T) {
			out := mustApply(t, limitedDoc, ops...)
			for _, line := range []string{"  run_timeout_seconds: 600", "    timeout_seconds: 30"} {
				if !strings.Contains(out, line) {
					t.Fatalf("saved text lost %q:\n%s", line, out)
				}
			}
			def := parse(t, out)
			if def.Nodes["n"].TimeoutSeconds == nil || *def.Nodes["n"].TimeoutSeconds != 30 || def.Limits == nil || *def.Limits.RunTimeoutSeconds != 600 {
				t.Fatalf("parsed limits changed: %+v %+v", def.Nodes["n"], def.Limits)
			}
		})
	}
}
