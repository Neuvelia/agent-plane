package api

import "testing"

const limitedGraph = `schema_version: 0
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

func TestOpenReturnsLimitsInTheDefinition(t *testing.T) {
	_, ts, dir := docServer(t)
	g := grantFor(t, ts, writeGraph(t, dir, "limited.yaml", limitedGraph), "open")
	status, out := call(t, ts, "POST", "/graphs/open", map[string]string{"grant": g})
	if status != 200 {
		t.Fatalf("open: %d %v", status, out)
	}
	def := out["definition"].(map[string]any)
	limits, _ := def["limits"].(map[string]any)
	node := def["nodes"].(map[string]any)["n"].(map[string]any)
	if limits["run_timeout_seconds"] != float64(600) || node["timeout_seconds"] != float64(30) {
		t.Errorf("limits missing from the definition DTO: %v", def)
	}
	if out["text"] != limitedGraph {
		t.Errorf("open changed the document text")
	}
}
