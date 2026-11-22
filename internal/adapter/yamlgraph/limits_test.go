package yamlgraph_test

import (
	"errors"
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
	"agentplane/internal/fields/fieldstest"
)

func intp(v int) *int { return &v }

func limitsDef(node graph.NodeDefinition, limits *graph.LimitsDefinition) *graph.Definition {
	return &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0, ID: "g",
		Inputs: map[string]graph.InputDefinition{}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{},
		Nodes:  map[string]graph.NodeDefinition{"n": node},
		Limits: limits,
	}
}

func timeoutDiagnostics(def *graph.Definition) []graph.Diagnostic {
	var out []graph.Diagnostic
	for _, d := range graph.Validate(def).Diagnostics {
		if d.Code == "E_TIMEOUT_INVALID" {
			out = append(out, d)
		}
	}
	return out
}

func TestTimeoutSecondsValidationTable(t *testing.T) {
	shell := func(v *int) graph.NodeDefinition {
		return graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "true"}, TimeoutSeconds: v}
	}
	human := graph.HumanOutputPorts()
	humanNode := graph.NodeDefinition{Type: "human", TimeoutSeconds: intp(30), Outputs: map[string]graph.PortDefinition{human[0].Name: {Type: graph.ValueTypeString, Required: true}}}
	for _, tt := range []struct {
		name string
		node graph.NodeDefinition
		ok   bool
	}{
		{"absent", shell(nil), true},
		{"minimum", shell(intp(1)), true},
		{"maximum", shell(intp(86400)), true},
		{"zero", shell(intp(0)), false},
		{"negative", shell(intp(-1)), false},
		{"above maximum", shell(intp(86401)), false},
		{"human node", humanNode, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := timeoutDiagnostics(limitsDef(tt.node, nil))
			if tt.ok {
				if len(got) != 0 {
					t.Fatalf("unexpected diagnostics %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("diagnostics = %+v, want one E_TIMEOUT_INVALID", got)
			}
			d := got[0]
			if d.Location.NodeID != "n" || d.Location.Path != "nodes.n.timeout_seconds" || d.Suggestion == "" || d.Severity != graph.SeverityError {
				t.Fatalf("diagnostic lacks node, path or suggestion: %+v", d)
			}
		})
	}
}

func TestRunLimitValidationTable(t *testing.T) {
	node := graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "true"}}
	for _, tt := range []struct {
		name   string
		limits *graph.LimitsDefinition
		ok     bool
	}{
		{"absent", nil, true},
		{"empty block", &graph.LimitsDefinition{}, true},
		{"minimum", &graph.LimitsDefinition{RunTimeoutSeconds: intp(1)}, true},
		{"maximum", &graph.LimitsDefinition{RunTimeoutSeconds: intp(86400)}, true},
		{"zero", &graph.LimitsDefinition{RunTimeoutSeconds: intp(0)}, false},
		{"negative", &graph.LimitsDefinition{RunTimeoutSeconds: intp(-5)}, false},
		{"above maximum", &graph.LimitsDefinition{RunTimeoutSeconds: intp(86401)}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := timeoutDiagnostics(limitsDef(node, tt.limits))
			if tt.ok != (len(got) == 0) {
				t.Fatalf("ok=%v, diagnostics %+v", tt.ok, got)
			}
			if !tt.ok && (len(got) != 1 || got[0].Location.Path != "limits.run_timeout_seconds" || got[0].Location.NodeID != "" || got[0].Suggestion == "") {
				t.Fatalf("diagnostic lacks path or suggestion: %+v", got)
			}
		})
	}
}

func TestLimitsChangeSemanticVersion(t *testing.T) {
	base := func() *graph.Definition {
		return limitsDef(graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "true"}}, nil)
	}
	version := func(d *graph.Definition) string {
		v, err := yamlgraph.SemanticVersion(d)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	none := version(base())
	withNode := base()
	n := withNode.Nodes["n"]
	n.TimeoutSeconds = intp(30)
	withNode.Nodes["n"] = n
	withRun := base()
	withRun.Limits = &graph.LimitsDefinition{RunTimeoutSeconds: intp(30)}
	other := base()
	other.Limits = &graph.LimitsDefinition{RunTimeoutSeconds: intp(31)}
	seen := map[string]string{"none": none}
	for name, d := range map[string]*graph.Definition{"node": withNode, "run": withRun, "other run": other} {
		v := version(d)
		for prev, pv := range seen {
			if v == pv {
				t.Fatalf("%s limits hash equals %s", name, prev)
			}
		}
		seen[name] = v
	}
	empty := base()
	empty.Limits = &graph.LimitsDefinition{}
	if version(empty) != none {
		t.Fatal("an empty limits block changed the version")
	}
}

func TestLayoutMetadataStillDoesNotChangeVersion(t *testing.T) {
	a := limitsDef(graph.NodeDefinition{Type: "shell", TimeoutSeconds: intp(5)}, &graph.LimitsDefinition{RunTimeoutSeconds: intp(60)})
	b := limitsDef(graph.NodeDefinition{Type: "shell", TimeoutSeconds: intp(5)}, &graph.LimitsDefinition{RunTimeoutSeconds: intp(60)})
	b.Name = "renamed"
	b.Metadata = graph.Metadata{"layout": map[string]any{"n": map[string]any{"x": 1, "y": 2}}}
	va, err := yamlgraph.SemanticVersion(a)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := yamlgraph.SemanticVersion(b)
	if err != nil {
		t.Fatal(err)
	}
	if va != vb {
		t.Fatalf("presentation changed the version: %s != %s", va, vb)
	}
}

const limitsDoc = `schema_version: 0
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

func TestYAMLRoundTripKeepsLimits(t *testing.T) {
	def, err := yamlgraph.Parse([]byte(limitsDoc))
	if err != nil {
		t.Fatal(err)
	}
	if def.Nodes["n"].TimeoutSeconds == nil || *def.Nodes["n"].TimeoutSeconds != 30 || def.Limits == nil || def.Limits.RunTimeoutSeconds == nil || *def.Limits.RunTimeoutSeconds != 600 {
		t.Fatalf("parsed limits lost: %+v %+v", def.Nodes["n"], def.Limits)
	}
	if d := timeoutDiagnostics(def); len(d) != 0 {
		t.Fatalf("valid limits rejected: %+v", d)
	}
	out, err := yamlgraph.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	again, err := yamlgraph.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := yamlgraph.SemanticVersion(def)
	v2, _ := yamlgraph.SemanticVersion(again)
	if v1 != v2 || *again.Limits.RunTimeoutSeconds != 600 || *again.Nodes["n"].TimeoutSeconds != 30 {
		t.Fatalf("round trip changed limits: %s %s", v1, v2)
	}
}

func TestMarshalOmitsAbsentLimits(t *testing.T) {
	out, err := yamlgraph.Marshal(pinnedDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "timeout") || strings.Contains(string(out), "limits") {
		t.Fatalf("absent limits were written:\n%s", out)
	}
}

func TestE_TIMEOUT_INVALID_ReportsPath(t *testing.T) {
	for _, tt := range []struct{ name, value, path string }{
		{"node float", "1.5", "nodes.n.timeout_seconds"},
		{"node string", `"30"`, "nodes.n.timeout_seconds"},
		{"node bool", "true", "nodes.n.timeout_seconds"},
		{"node null", "null", "nodes.n.timeout_seconds"},
		{"run float", "1.5", "limits.run_timeout_seconds"},
		{"run string", `"30"`, "limits.run_timeout_seconds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc := strings.Replace(limitsDoc, "timeout_seconds: 30", "timeout_seconds: "+tt.value, 1)
			if strings.HasPrefix(tt.name, "run") {
				doc = strings.Replace(limitsDoc, "run_timeout_seconds: 600", "run_timeout_seconds: "+tt.value, 1)
			}
			_, err := yamlgraph.Parse([]byte(doc))
			var invalid *yamlgraph.TimeoutInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v, want *TimeoutInvalidError", err)
			}
			if invalid.Path != tt.path || !strings.Contains(err.Error(), "E_TIMEOUT_INVALID") || !strings.Contains(err.Error(), tt.path) {
				t.Fatalf("error %q lacks code or path %s", err, tt.path)
			}
		})
	}
	if _, err := yamlgraph.Parse([]byte(strings.Replace(limitsDoc, "timeout_seconds: 30", "timeout_seconds: 0", 1))); err != nil {
		t.Fatalf("an out-of-range integer is a validation finding, not a parse error: %v", err)
	}
}

func TestTimeoutFieldDescriptorsEqualTheValidator(t *testing.T) {
	fieldstest.PinKeys(t, graph.NodeTimeoutFields(), "timeout_seconds")
	fieldstest.PinKeys(t, graph.RunLimitFields(), "run_timeout_seconds")
	for _, f := range append(graph.NodeTimeoutFields(), graph.RunLimitFields()...) {
		if f.Min == nil || *f.Min != 1 || f.Max == nil || *f.Max != 86400 || f.Default != nil {
			t.Fatalf("descriptor %+v does not match 1..86400 with no default", f)
		}
		check := func(v int, ok bool) {
			node := graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "true"}}
			var limits *graph.LimitsDefinition
			if f.Key == "timeout_seconds" {
				node.TimeoutSeconds = intp(v)
			} else {
				limits = &graph.LimitsDefinition{RunTimeoutSeconds: intp(v)}
			}
			if got := len(timeoutDiagnostics(limitsDef(node, limits))) == 0; got != ok {
				t.Fatalf("%s=%d accepted=%v, want %v", f.Key, v, got, ok)
			}
		}
		check(*f.Min, true)
		check(*f.Max, true)
		check(*f.Min-1, false)
		check(*f.Max+1, false)
	}
}
