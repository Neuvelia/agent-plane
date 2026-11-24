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

const secretShell = "      secret_env: {GH_TOKEN: gh_token}\n"

// relayNode is a pass-through node of the given type: one string input i, one
// string output o. Gate and human nodes use their own shapes.
func relayNode(id, nodeType string) string {
	switch nodeType {
	case "gate":
		return "  " + id + ":\n    type: gate\n    config: {check_id: c, kind: json_predicate}\n" + inPort + outPort
	case "human":
		return "  " + id + ":\n    type: human\n    inputs:\n      i: {type: string}\n    outputs:\n      decision: {type: string}\n"
	}
	return flowNode(id, "", inPort+outPort)
}

func flowEdge(from, to string) string {
	port := "o"
	if strings.HasPrefix(from, "h") {
		port = "decision"
	}
	return "  - {from: " + from + ".outputs." + port + ", to: " + to + ".inputs.i}\n"
}

func flowMessages(t *testing.T, nodes, edges string) []string {
	t.Helper()
	var out []string
	for _, d := range flowDiagnostics(t, validateText(t, flowGraph(nodes, edges))) {
		out = append(out, d.Message)
	}
	return out
}

func TestSecretFlowIsReportedThroughAnIntermediateNode(t *testing.T) {
	nodes := flowNode("a", secretShell, outPort) + relayNode("cat", "shell") + agentNode("z")
	got := flowMessages(t, nodes, flowEdge("a", "cat")+flowEdge("cat", "z"))
	if len(got) != 1 || !strings.Contains(got[0], "a -> cat -> z") || !strings.Contains(got[0], "a.outputs.o to cat.inputs.i") {
		t.Fatalf("want one path diagnostic naming a -> cat -> z and the first edge, got %q", got)
	}
}

func TestSecretFlowIsReportedOnALongPath(t *testing.T) {
	nodes := flowNode("a", secretShell, outPort) + relayNode("b", "shell") + relayNode("c", "shell") + agentNode("z")
	got := flowMessages(t, nodes, flowEdge("a", "b")+flowEdge("b", "c")+flowEdge("c", "z"))
	if len(got) != 1 || !strings.Contains(got[0], "a -> b -> c -> z") {
		t.Fatalf("got %q", got)
	}
}

func TestSecretFlowPropagatesThroughHumanAndGateNodes(t *testing.T) {
	for _, relay := range []string{"human", "gate"} {
		id := map[string]string{"human": "hum", "gate": "chk"}[relay]
		nodes := flowNode("a", secretShell, outPort) + relayNode(id, relay) + agentNode("z")
		got := flowMessages(t, nodes, flowEdge("a", id)+flowEdge(id, "z"))
		if len(got) != 1 || !strings.Contains(got[0], "a -> "+id+" -> z") {
			t.Errorf("%s must not stop propagation, got %q", relay, got)
		}
	}
}

func TestSecretFlowFromTwoSourcesToOneAgentReportsEachPair(t *testing.T) {
	nodes := flowNode("a", secretShell, outPort) + flowNode("b", secretShell, outPort) +
		"  z:\n    type: agent\n    config: {adapter: codex, workspace: /w, task: t}\n    inputs:\n      i: {type: string}\n      j: {type: string}\n"
	edges := flowEdge("a", "z") + "  - {from: b.outputs.o, to: z.inputs.j}\n"
	if got := flowMessages(t, nodes, edges); len(got) != 2 {
		t.Fatalf("want one diagnostic per source, got %q", got)
	}
}

func TestSecretFlowDiamondAndBranchingReportOncePerAgent(t *testing.T) {
	nodes := flowNode("a", secretShell, outPort) + relayNode("l", "shell") + relayNode("r", "shell") +
		"  z:\n    type: agent\n    config: {adapter: codex, workspace: /w, task: t}\n    inputs:\n      i: {type: string}\n      j: {type: string}\n" +
		agentNode("y")
	edges := flowEdge("a", "l") + flowEdge("a", "r") + flowEdge("l", "z") + "  - {from: r.outputs.o, to: z.inputs.j}\n" + flowEdge("r", "y")
	got := flowMessages(t, nodes, edges)
	if len(got) != 2 {
		t.Fatalf("want one diagnostic for z (two paths) and one for y, got %q", got)
	}
	if !strings.Contains(got[0]+got[1], "a -> l -> z") || !strings.Contains(got[0]+got[1], "a -> r -> y") {
		t.Errorf("shortest deterministic paths expected, got %q", got)
	}
}

func TestAgentReachableOnlyFromANodeWithoutSecretsIsAccepted(t *testing.T) {
	nodes := flowNode("a", "", outPort) + relayNode("b", "shell") + agentNode("z")
	if got := flowMessages(t, nodes, flowEdge("a", "b")+flowEdge("b", "z")); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestEmptySecretEnvDoesNotTriggerTheTransitiveRule(t *testing.T) {
	nodes := flowNode("a", "      secret_env: {}\n", outPort) + relayNode("b", "shell") + agentNode("z")
	if got := flowMessages(t, nodes, flowEdge("a", "b")+flowEdge("b", "z")); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestSecretFlowWalkTerminatesOnACycle(t *testing.T) {
	nodes := flowNode("a", secretShell, inPort+outPort) + relayNode("b", "shell") + agentNode("z")
	got := flowMessages(t, nodes, flowEdge("a", "b")+flowEdge("b", "a")+flowEdge("b", "z"))
	if len(got) != 1 || !strings.Contains(got[0], "a -> b -> z") {
		t.Fatalf("got %q", got)
	}
}

func TestSecretFlowToShellOnlyChainIsAccepted(t *testing.T) {
	nodes := flowNode("a", secretShell, outPort) + relayNode("b", "shell") + relayNode("c", "shell")
	if got := flowMessages(t, nodes, flowEdge("a", "b")+flowEdge("b", "c")); len(got) != 0 {
		t.Fatalf("got %q", got)
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
