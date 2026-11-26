package gate

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/testenv"
)

func node(kind string) graph.NodeDefinition {
	return graph.NodeDefinition{Type: "gate", Config: graph.NodeConfig{"kind": kind, "check_id": "check"}, Inputs: map[string]graph.PortDefinition{}, Outputs: map[string]graph.PortDefinition{"verdict": {Type: graph.ValueTypeString, Required: true}}}
}
func jsonNode() graph.NodeDefinition {
	n := node("json_predicate")
	n.Config["input"] = "data"
	n.Config["field"] = []string{"result"}
	n.Config["operator"] = "eq"
	n.Config["expected"] = true
	n.Inputs["data"] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
	return n
}
func jsonValue(t *testing.T, s string) graph.Value {
	t.Helper()
	v, e := graph.NewJSONValue([]byte(s))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func evaluate(t *testing.T, n graph.NodeDefinition, in map[string]graph.Value) (*events.GateCheckPayload, error) {
	t.Helper()
	var p *events.GateCheckPayload
	_, _, e := (Executor{}).ExecuteNodeObserved(context.Background(), "gate", n, in, runtime.ExecutionObservation{Check: func(v events.GateCheckPayload) {
		if v.Version != 1 || v.DurationNS < 0 || len(v.Scope.CheckedInputs) == 0 || v.Validate() != nil {
			t.Errorf("invalid full check result: %+v", v)
		}
		p = &v
	}})
	return p, e
}
func TestPredicateContracts(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		expected    any
		op          string
		red, fail   bool
	}{
		{"match", `{"result":true}`, true, "eq", false, false}, {"mismatch", `{"result":false}`, true, "eq", true, false}, {"missing", `{}`, true, "ne", true, false}, {"null", `{"result":null}`, nil, "eq", false, false}, {"null missing", `{}`, nil, "eq", true, false}, {"large unequal", `{"result":9007199254740993}`, json.Number("9007199254740992"), "eq", true, false}, {"large equal", `{"result":9007199254740993}`, json.Number("9007199254740993"), "eq", false, false}, {"numeric decimal", `{"result":1.0}`, json.Number("1"), "eq", false, false}, {"exponent", `{"result":1e3}`, json.Number("1000"), "eq", false, false}, {"no coercion", `{"result":"1"}`, json.Number("1"), "eq", true, false}, {"root wrong", `[]`, true, "eq", false, true}, {"field wrong", `{"result":[]}`, true, "eq", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := jsonNode()
			n.Config["expected"] = tc.expected
			n.Config["operator"] = tc.op
			p, e := evaluate(t, n, map[string]graph.Value{"data": jsonValue(t, tc.input)})
			var red RedError
			if tc.fail {
				if e == nil || p != nil {
					t.Fatal("expected input error")
				}
				return
			}
			if errors.As(e, &red) != tc.red || p == nil || (p.Verdict == "red") != tc.red {
				t.Fatalf("%+v %v", p, e)
			}
		})
	}
	n := jsonNode()
	if p, e := evaluate(t, n, nil); e == nil || p != nil {
		t.Fatal("missing input accepted")
	}
}
func TestReportedScope(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		red, fail   bool
	}{
		{"empty", `[]`, false, false}, {"allowed normalized", `[{"path":"src//./a.go","provenance":"observed"}]`, false, false}, {"segment boundary", `[{"path":"src2/a"}]`, true, false}, {"sort dedup", `[{"path":"z"},{"path":"a"},{"path":"z"}]`, true, false}, {"traversal", `[{"path":"src/../bad"}]`, false, true}, {"absolute", `[{"path":"/src/a"}]`, false, true}, {"backslash", `[{"path":"src\\a"}]`, false, true}, {"drive", `[{"path":"C:/a"}]`, false, true}, {"null", `null`, false, true}, {"bad record", `["a"]`, false, true}, {"missing path", `[{}]`, false, true}, {"unknown", `[{"path":"x","extra":true}]`, false, true}, {"bad provenance", `[{"path":"x","provenance":2}]`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := node("reported_file_scope")
			n.Config["input"] = "files"
			n.Config["allowed_paths"] = []string{"src/", "README.md"}
			n.Inputs["files"] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
			p, e := evaluate(t, n, map[string]graph.Value{"files": jsonValue(t, tc.input)})
			if tc.fail {
				if e == nil || p != nil {
					t.Fatal("accepted malformed scope")
				}
				return
			}
			if (e != nil) != tc.red || p == nil {
				t.Fatalf("%+v %v", p, e)
			}
			for _, ev := range p.Evidence {
				if ev.Source != "reported" {
					t.Fatal("wrong provenance")
				}
			}
			if tc.name == "sort dedup" && (len(p.Evidence) != 2 || p.Evidence[0].Path != "a" || p.Evidence[1].Path != "z") {
				t.Fatalf("%+v", p)
			}
		})
	}
	n := node("reported_file_scope")
	n.Config["input"] = "files"
	n.Config["allowed_paths"] = []string{}
	n.Inputs["files"] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
	a := []map[string]string{}
	for i := 0; i < 100; i++ {
		a = append(a, map[string]string{"path": strings.Repeat("a", i+1)})
	}
	b, _ := json.Marshal(a)
	p, e := evaluate(t, n, map[string]graph.Value{"files": jsonValue(t, string(b))})
	if e == nil || p == nil || len(p.Evidence) > 32 || p.Evidence[len(p.Evidence)-1].Omitted == 0 {
		t.Fatal("scope evidence not bounded")
	}
}
func diffNode(t *testing.T) graph.NodeDefinition {
	t.Helper()
	d, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	n := node("command")
	n.Config["cwd"] = d
	n.Config["command"] = "/usr/bin/diff"
	n.Config["left_input"] = "left"
	n.Config["right_input"] = "right"
	for _, k := range []string{"left", "right"} {
		n.Inputs[k] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
	}
	return n
}
func paths() map[string]graph.Value {
	return map[string]graph.Value{"left": graph.NewStringValue("left"), "right": graph.NewStringValue("right")}
}

// roadmap: F1.4-AC2
func TestRealReadOnlyDiffDescriptorGreenRedAndBounded(t *testing.T) {
	testenv.RequireDarwinOrLinux(t, "the command Gate")
	for _, same := range []bool{true, false} {
		n := diffNode(t)
		cwd := n.Config["cwd"].(string)
		left := strings.Repeat("left\n", 5000)
		right := left
		if !same {
			right = strings.Repeat("right\n", 5000)
		}
		for k, v := range map[string]string{"left": left, "right": right} {
			if e := os.WriteFile(filepath.Join(cwd, k), []byte(v), 0400); e != nil {
				t.Fatal(e)
			}
		}
		p, e := evaluate(t, n, paths())
		if p == nil || (e == nil) != same {
			t.Fatalf("%+v %v", p, e)
		}
		ev := p.Evidence[0]
		if (*ev.ExitCode == 0) != same || len(ev.Stdout) > 8192 || (!same && ev.StdoutOmitted == 0) {
			t.Fatalf("%+v", ev)
		}
		for k, v := range map[string]string{"left": left, "right": right} {
			b, _ := os.ReadFile(filepath.Join(cwd, k))
			if string(b) != v {
				t.Fatal("diff modified file")
			}
		}
	}
}
func TestUnsafeCommandsConfigAndFilesRejected(t *testing.T) {
	for _, mutate := range []func(graph.NodeDefinition){func(n graph.NodeDefinition) { n.Config["command"] = "/bin/sh" }, func(n graph.NodeDefinition) { n.Config["args"] = []string{"-o", "write"} }, func(n graph.NodeDefinition) { n.Config["environment"] = map[string]string{"LD_PRELOAD": "evil"} }, func(n graph.NodeDefinition) { n.Config["cwd"] = "relative" }, func(n graph.NodeDefinition) { n.Config["zzz"] = true; n.Config["aaa"] = true }, func(n graph.NodeDefinition) { n.Inputs["left"] = graph.PortDefinition{Type: graph.ValueTypeString} }, func(n graph.NodeDefinition) { n.Outputs["red"] = graph.PortDefinition{Type: graph.ValueTypeJSON} }} {
		n := diffNode(t)
		mutate(n)
		if _, e := parse(n); e == nil {
			t.Fatal("unsafe config accepted")
		}
	}
	for _, kind := range []string{"symlink", "parent symlink", "fifo", "directory", "traversal", "device", "missing"} {
		t.Run(kind, func(t *testing.T) {
			n := diffNode(t)
			cwd := n.Config["cwd"].(string)
			os.WriteFile(filepath.Join(cwd, "right"), nil, 0600)
			in := paths()
			switch kind {
			case "symlink":
				os.Symlink("right", filepath.Join(cwd, "left"))
			case "parent symlink":
				os.Symlink(".", filepath.Join(cwd, "alias"))
				in["left"] = graph.NewStringValue("alias/right")
			case "fifo":
				makeFIFO(t, filepath.Join(cwd, "left"))
			case "directory":
				os.Mkdir(filepath.Join(cwd, "left"), 0700)
			case "traversal":
				in["left"] = graph.NewStringValue("../right")
			case "device":
				in["left"] = graph.NewStringValue("/dev/null")
			}
			if p, e := evaluate(t, n, in); p != nil || e == nil {
				t.Fatal("unsafe file accepted")
			}
		})
	}
}
func TestCancellationAndDiffOperationalErrorsHaveNoVerdict(t *testing.T) {
	n := diffNode(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var fact bool
	_, _, e := (Executor{}).ExecuteNodeObserved(ctx, "gate", n, paths(), runtime.ExecutionObservation{Check: func(events.GateCheckPayload) { fact = true }})
	if !errors.Is(e, context.Canceled) || fact {
		t.Fatalf("%v %t", e, fact)
	}
	if p, e := evaluate(t, n, paths()); e == nil || p != nil {
		t.Fatal("missing files fabricated verdict")
	}
}

func TestExactLiteralConfiguration(t *testing.T) {
	for _, tc := range []struct {
		literal, input string
		red            bool
	}{{"1.00000000000000000001", `{"result":1.00000000000000000001}`, false}, {"1.00000000000000000001", `{"result":1.0}`, true}, {"123456789012345678901234567890", `{"result":123456789012345678901234567890}`, false}, {"1e1000000000", `{"result":10e999999999}`, false}, {"-0.0", `{"result":0}`, false}} {
		n := jsonNode()
		delete(n.Config, "expected")
		n.Config["expected_json"] = tc.literal
		p, e := evaluate(t, n, map[string]graph.Value{"data": jsonValue(t, tc.input)})
		if p == nil || (e != nil) != tc.red {
			t.Fatalf("%s %+v %v", tc.literal, p, e)
		}
	}
	for _, value := range []any{"{}", "[]", "1 2", "", "not json", nil, 2} {
		n := jsonNode()
		delete(n.Config, "expected")
		n.Config["expected_json"] = value
		if _, e := parse(n); e == nil {
			t.Fatalf("accepted literal %v", value)
		}
	}
	n := jsonNode()
	n.Config["expected_json"] = "true"
	if _, e := parse(n); e == nil {
		t.Fatal("both expected sources accepted")
	}
	delete(n.Config, "expected_json")
	delete(n.Config, "expected")
	if _, e := parse(n); e == nil {
		t.Fatal("missing expected accepted")
	}
	n.Config["expected"] = 1.01
	if _, e := parse(n); e == nil || !strings.Contains(e.Error(), "expected_json") {
		t.Fatal("rounded float shorthand accepted")
	}
}

type cancellationWriter struct {
	cancel  context.CancelFunc
	started chan struct{}
	once    bool
}

func (w *cancellationWriter) Write(b []byte) (int, error) {
	if !w.once {
		w.once = true
		close(w.started)
		w.cancel()
	}
	return len(b), nil
}
func TestDiffLifecycleStartOperationalAndDuringExecutionCancellation(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/diff")
	if _, e := runDiff(context.Background(), exec.CommandContext(context.Background(), filepath.Join(t.TempDir(), "missing"))); e == nil {
		t.Fatal("start failure hidden")
	}
	if _, e := runDiff(context.Background(), exec.CommandContext(context.Background(), "/usr/bin/diff", filepath.Join(t.TempDir(), "missing"), "/dev/null")); e == nil {
		t.Fatal("exit2 hidden")
	}
	n := diffNode(t)
	cwd := n.Config["cwd"].(string)
	os.WriteFile(filepath.Join(cwd, "left"), []byte(strings.Repeat("left\n", 100000)), 0400)
	os.WriteFile(filepath.Join(cwd, "right"), []byte(strings.Repeat("right\n", 100000)), 0400)
	left, e := openCompared(cwd, "left")
	if e != nil {
		t.Fatal(e)
	}
	defer left.Close()
	right, e := openCompared(cwd, "right")
	if e != nil {
		t.Fatal(e)
	}
	defer right.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &cancellationWriter{cancel: cancel, started: make(chan struct{})}
	cmd := exec.CommandContext(ctx, "/usr/bin/diff", "/dev/fd/3", "/dev/fd/4")
	cmd.ExtraFiles = []*os.File{left, right}
	cmd.Env = []string{"LC_ALL=C"}
	cmd.Dir = cwd
	cmd.Stdout = w
	cmd.WaitDelay = time.Second
	_, err := runDiff(ctx, cmd)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("during cancellation = %v", err)
	}
	select {
	case <-w.started:
	default:
		t.Fatal("process never emitted output before cancel")
	}
	if cmd.ProcessState == nil {
		t.Fatal("process not reaped")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("capture failed") }
func TestDiffDrainErrorDoesNotBecomeRed(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/diff")
	for _, lines := range []int{1, 100000} {
		n := diffNode(t)
		cwd := n.Config["cwd"].(string)
		os.WriteFile(filepath.Join(cwd, "left"), []byte(strings.Repeat("left\n", lines)), 0400)
		os.WriteFile(filepath.Join(cwd, "right"), []byte(strings.Repeat("right\n", lines)), 0400)
		cmd := exec.CommandContext(context.Background(), "/usr/bin/diff", filepath.Join(cwd, "left"), filepath.Join(cwd, "right"))
		cmd.Env = []string{"LC_ALL=C"}
		cmd.Stdout = failingWriter{}
		if _, e := runDiff(context.Background(), cmd); e == nil || !strings.Contains(e.Error(), "drain diff evidence") {
			t.Fatalf("copy failure hidden by exit1: %v", e)
		}
	}
}
func TestConfigDiagnosticsAndExactYAMLRoundTrip(t *testing.T) {
	for _, mutate := range []func(graph.NodeDefinition){func(n graph.NodeDefinition) { n.Config["check_id"] = "" }, func(n graph.NodeDefinition) { n.Config["kind"] = "unknown" }, func(n graph.NodeDefinition) { n.Config["field"] = []string{} }, func(n graph.NodeDefinition) { n.Config["field"] = []string{""} }, func(n graph.NodeDefinition) { n.Config["operator"] = "eval" }, func(n graph.NodeDefinition) { n.Config["expected"] = []string{"no"} }, func(n graph.NodeDefinition) { n.Config["aaa"] = true; n.Config["zzz"] = true }, func(n graph.NodeDefinition) {
		n.Inputs["data"] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
	}} {
		n := jsonNode()
		mutate(n)
		_, _, e := (Executor{}).ExecuteNodeObserved(context.Background(), "named-gate", n, nil, runtime.ExecutionObservation{})
		if e == nil || !strings.Contains(e.Error(), `gate node "named-gate"`) {
			t.Fatal("config error lacks node context")
		}
		if _, ok := n.Config["aaa"]; ok && !strings.Contains(e.Error(), `"aaa"`) {
			t.Fatal("unknown fields not sorted")
		}
	}
	source := []byte(`schema_version: 0
id: precise
nodes:
  gate:
    type: gate
    config:
      check_id: precise
      kind: json_predicate
      input: data
      field: [result]
      operator: eq
      expected_json: '1.00000000000000000001'
    inputs:
      data: {type: json, required: true}
`)
	d, e := yamlgraph.Parse(source)
	if e != nil {
		t.Fatal(e)
	}
	n := d.Nodes["gate"]
	if p, e := evaluate(t, n, map[string]graph.Value{"data": jsonValue(t, `{"result":1.00000000000000000001}`)}); e != nil || p == nil {
		t.Fatalf("exact authored literal: %v", e)
	}
	version, e := yamlgraph.SemanticVersion(d)
	if e != nil {
		t.Fatal(e)
	}
	b, e := yamlgraph.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	again, e := yamlgraph.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	v, e := yamlgraph.SemanticVersion(again)
	if e != nil || v != version {
		t.Fatal("opaque exact literal roundtrip changed hash")
	}
}

func TestReportedScopeEvidenceBudgetIncludesJSONEscaping(t *testing.T) {
	n := node("reported_file_scope")
	n.Config["input"] = "files"
	n.Config["allowed_paths"] = []string{}
	n.Inputs["files"] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
	a := []map[string]string{}
	for i := 0; i < 30; i++ {
		a = append(a, map[string]string{"path": strings.Repeat("\x01", 500) + fmt.Sprintf("%03d", i)})
	}
	b, _ := json.Marshal(a)
	p, e := evaluate(t, n, map[string]graph.Value{"files": jsonValue(t, string(b))})
	var red RedError
	if !errors.As(e, &red) || p == nil {
		t.Fatalf("valid out-of-scope paths must remain red with bounded evidence: %v", e)
	}
	b, _ = json.Marshal(p)
	if len(b) > events.MaxGatePayload || p.Evidence[len(p.Evidence)-1].Omitted == 0 {
		t.Fatal("escaped evidence missing bounded omission marker")
	}
}

type gatedSink struct {
	Executor
	started bool
}

func (e *gatedSink) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	if n.Type == "shell" {
		e.started = true
		return map[string]graph.Value{}, nil, nil
	}
	return e.Executor.ExecuteNodeObserved(ctx, id, n, in, o)
}
func TestEveryGateKindTypedTraceAndDownstream(t *testing.T) {
	for _, kind := range []string{"command", "json_predicate", "reported_file_scope"} {
		for _, green := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%t", kind, green), func(t *testing.T) {
				n := jsonNode()
				in := map[string]graph.Value{"data": jsonValue(t, fmt.Sprintf(`{"result":%t}`, green))}
				switch kind {
				case "command":
					testenv.RequireDarwinOrLinux(t, "the command Gate")
					n = diffNode(t)
					in = paths()
					cwd := n.Config["cwd"].(string)
					os.WriteFile(filepath.Join(cwd, "left"), []byte("left"), 0600)
					right := "right"
					if green {
						right = "left"
					}
					os.WriteFile(filepath.Join(cwd, "right"), []byte(right), 0600)
				case "reported_file_scope":
					n = node(kind)
					n.Config["input"] = "files"
					n.Config["allowed_paths"] = []string{"allowed"}
					n.Inputs["files"] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
					path := "outside"
					if green {
						path = "allowed"
					}
					in = map[string]graph.Value{"files": jsonValue(t, fmt.Sprintf(`[{"path":%q}]`, path))}
				}
				d := &graph.Definition{ID: "full-check", Outputs: map[string]graph.OutputDefinition{}, Nodes: map[string]graph.NodeDefinition{"gate": n, "sink": {Type: "shell", Inputs: map[string]graph.PortDefinition{"accepted": {Type: graph.ValueTypeString, Required: true}}}}, Inputs: map[string]graph.InputDefinition{}, Edges: []graph.EdgeDefinition{{From: "gate.outputs.verdict", To: "sink.inputs.accepted"}}}
				for name, port := range n.Inputs {
					d.Inputs[name] = graph.InputDefinition{Type: port.Type, Required: true}
					d.GraphInputBindings = append(d.GraphInputBindings, graph.GraphInputBindingDefinition{From: graph.GraphInputReference("inputs." + name), To: graph.NodeInputReference("gate.inputs." + name)})
				}
				exec := &gatedSink{}
				r, err := runtime.Run(context.Background(), d, in, exec, runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
				if err != nil || (r.State == run.RunSucceeded) != green || exec.started != green {
					t.Fatalf("state/downstream: %+v %v", r, err)
				}
				var gateRun events.NodeRunID
				for _, node := range r.Nodes {
					if node.NodeID == "gate" {
						gateRun = node.NodeRunID
						if !green && len(node.Outputs) != 0 {
							t.Fatal("accepted red")
						}
					}
				}
				got := events.CheckResults(r.Events, r.RunID, gateRun)
				if len(got) != 1 || got[0].Version != 1 || got[0].DurationNS < 0 || (got[0].Verdict == events.CheckGreen) != green || got[0].Reason == "" || len(got[0].Evidence) == 0 {
					t.Fatal("missing typed result")
				}
				if len(got[0].Scope.CheckedInputs) != len(in) {
					t.Fatal("checked input scope")
				}
				for _, name := range got[0].Scope.CheckedInputs {
					if _, ok := in[name]; !ok {
						t.Fatal("invented scope")
					}
				}
			})
		}
	}
}
