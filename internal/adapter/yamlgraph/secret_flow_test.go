package yamlgraph_test

import (
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

// F3.8 / ADR-0029 addendum: an edge from a node holding secret_env to an Agent
// node is rejected, because accepted output is immutable and flows raw.

const flowSecrets = "secrets:\n  gh_token:\n    secret_ref: env/GITHUB_TOKEN\n"

const (
	outPort = "    outputs:\n      o: {type: string}\n"
	inPort  = "    inputs:\n      i: {type: string}\n"
)

func flowNode(id, config, ports string) string {
	return "  " + id + ":\n    type: shell\n    config:\n      command: /bin/true\n" + config + ports
}

func flowGraph(nodes, edges string) string {
	return "schema_version: 0\nid: flow\n" + flowSecrets + "inputs: {}\nnodes:\n" + nodes + "edges:\n" + edges + "outputs: {}\n"
}

func agentNode(id string) string {
	return "  " + id + ":\n    type: agent\n    config: {adapter: codex, workspace: /w, task: t}\n" + inPort
}

func flowDiagnostics(t *testing.T, result graph.ValidationResult) []graph.Diagnostic {
	t.Helper()
	var out []graph.Diagnostic
	for _, d := range result.Diagnostics {
		if d.Code == "E_SECRET_FLOWS_TO_AGENT" {
			out = append(out, d)
		}
	}
	return out
}

func TestSecretNodeToShellEdgeIsAccepted(t *testing.T) {
	text := flowGraph(
		flowNode("a", "      secret_env: {GH_TOKEN: gh_token}\n", outPort)+flowNode("b", "", inPort),
		"  - {from: a.outputs.o, to: b.inputs.i}\n")
	if got := flowDiagnostics(t, validateText(t, text)); len(got) != 0 {
		t.Fatalf("secret node to shell must pass the flow rule: %+v", got)
	}
}

func TestSecretNodeToAgentEdgeIsRejectedNamingNodesAndEdge(t *testing.T) {
	text := flowGraph(
		flowNode("a", "      secret_env: {GH_TOKEN: gh_token}\n", outPort)+agentNode("b"),
		"  - {from: a.outputs.o, to: b.inputs.i}\n")
	result := validateText(t, text)
	got := flowDiagnostics(t, result)
	if len(got) != 1 {
		t.Fatalf("want one E_SECRET_FLOWS_TO_AGENT, got %+v", result.Diagnostics)
	}
	d := got[0]
	for _, want := range []string{`"a"`, `"b"`, "a.outputs.o", "b.inputs.i"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("message %q must contain %s", d.Message, want)
		}
	}
	if d.Severity != graph.SeverityError || result.Valid() {
		t.Errorf("must be a blocking error: %+v", d)
	}
	if d.Location.EdgeIndex == nil || *d.Location.EdgeIndex != 0 || d.Location.Path != "edges[0]" {
		t.Errorf("location = %+v, want edge 0", d.Location)
	}
	for _, advice := range []string{"edge", "without a secret", "secret_env"} {
		if !strings.Contains(d.Suggestion, advice) {
			t.Errorf("suggestion %q must mention %q", d.Suggestion, advice)
		}
	}
}

func TestNodeWithoutSecretEnvToAgentEdgeIsAccepted(t *testing.T) {
	text := flowGraph(
		flowNode("a", "", outPort)+agentNode("b"),
		"  - {from: a.outputs.o, to: b.inputs.i}\n")
	if got := flowDiagnostics(t, validateText(t, text)); len(got) != 0 {
		t.Fatalf("a node without secret_env must not trigger the rule: %+v", got)
	}
}

func TestEmptySecretEnvDoesNotTriggerTheFlowRule(t *testing.T) {
	text := flowGraph(
		flowNode("a", "      secret_env: {}\n", outPort)+agentNode("b"),
		"  - {from: a.outputs.o, to: b.inputs.i}\n")
	if got := flowDiagnostics(t, validateText(t, text)); len(got) != 0 {
		t.Fatalf("an empty secret_env assigns nothing: %+v", got)
	}
}

func TestEveryEdgeFromASecretNodeToAnAgentIsReported(t *testing.T) {
	nodes := "  a:\n    type: shell\n    config: {command: /bin/true, secret_env: {GH_TOKEN: gh_token}}\n    outputs:\n      o: {type: string}\n      p: {type: string}\n" +
		agentNode("b") + agentNode("c") + flowNode("d", "", inPort)
	edges := "  - {from: a.outputs.o, to: b.inputs.i}\n  - {from: a.outputs.p, to: c.inputs.i}\n  - {from: a.outputs.o, to: d.inputs.i}\n"
	got := flowDiagnostics(t, validateText(t, flowGraph(nodes, edges)))
	if len(got) != 2 {
		t.Fatalf("want two diagnostics (edges 0 and 1, not the shell target), got %+v", got)
	}
	if *got[0].Location.EdgeIndex != 0 || *got[1].Location.EdgeIndex != 1 {
		t.Errorf("edge indexes = %d, %d", *got[0].Location.EdgeIndex, *got[1].Location.EdgeIndex)
	}
}

func TestSecretFlowRuleLeavesTheTransitivePathUnreported(t *testing.T) {
	// Documented gap: only direct edges are rejected (F3.8 spec, transitive gap).
	nodes := flowNode("a", "      secret_env: {GH_TOKEN: gh_token}\n", outPort) +
		"  b:\n    type: shell\n    config: {command: /bin/true}\n" + inPort + outPort + agentNode("c")
	edges := "  - {from: a.outputs.o, to: b.inputs.i}\n  - {from: b.outputs.o, to: c.inputs.i}\n"
	if got := flowDiagnostics(t, validateText(t, flowGraph(nodes, edges))); len(got) != 0 {
		t.Fatalf("transitive flow is a documented gap, not rejected: %+v", got)
	}
}

func TestSecretFlowDiagnosticNeverEchoesAVariableOutsideTheGrammar(t *testing.T) {
	const pasted = "ghp_pastedLiteral=1"
	text := flowGraph(
		flowNode("a", "      secret_env: {\""+pasted+"\": gh_token}\n", outPort)+agentNode("b"),
		"  - {from: a.outputs.o, to: b.inputs.i}\n")
	for _, d := range validateText(t, text).Diagnostics {
		if strings.Contains(d.Message+d.Suggestion+d.Location.Path, pasted) {
			t.Fatalf("diagnostic %s echoes the variable text: %+v", d.Code, d)
		}
	}
}
