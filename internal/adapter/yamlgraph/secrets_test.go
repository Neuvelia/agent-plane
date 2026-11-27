package yamlgraph_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
)

const secretsGraphHead = `
schema_version: 0
id: secrets-test
inputs: {}
edges: []
outputs: {}
`

func secretsGraph(secrets, nodes string) string {
	return secretsGraphHead + secrets + "nodes:\n" + nodes
}

const oneSecret = `secrets:
  gh_token:
    secret_ref: env/GITHUB_TOKEN
`

func shellNode(id, config string) string {
	return "  " + id + ":\n    type: shell\n    config:\n      command: /bin/true\n" + config
}

func validateText(t *testing.T, text string) graph.ValidationResult {
	t.Helper()
	return graph.Validate(parseDefinition(t, text))
}

func diagnosticFor(t *testing.T, result graph.ValidationResult, code string) graph.Diagnostic {
	t.Helper()
	for _, d := range result.Diagnostics {
		if d.Code == code {
			return d
		}
	}
	t.Fatalf("no %s diagnostic in %#v", code, result.Diagnostics)
	return graph.Diagnostic{}
}

func TestValidateSecretsAcceptReferencesAndAssignment(t *testing.T) {
	result := validateText(t, secretsGraph(oneSecret, shellNode("a", "      secret_env: {GH_TOKEN: gh_token}\n")+shellNode("b", "")))
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestValidateSecretsRejectsValueAndExtraFields(t *testing.T) {
	const leaked = "hunter2-literal"
	result := validateText(t, secretsGraph("secrets:\n  gh_token:\n    secret_ref: env/GITHUB_TOKEN\n    value: "+leaked+"\n", shellNode("a", "")))
	d := diagnosticFor(t, result, "E_SECRET_ENTRY_INVALID")
	if !strings.Contains(d.Message, `"gh_token"`) || !strings.Contains(d.Message, `"value"`) || strings.Contains(d.Message+d.Suggestion, leaked) {
		t.Fatalf("diagnostic must name the secret and the key, never the content: %+v", d)
	}
	if d.Location.Path != "secrets.gh_token" || result.Valid() {
		t.Fatalf("location %q valid=%v", d.Location.Path, result.Valid())
	}
}

func TestValidateSecretsRejectsAScalarEntryWithoutEchoingIt(t *testing.T) {
	const leaked = "ghp_literalTokenPastedByMistake"
	text := secretsGraph("secrets:\n  gh_token: "+leaked+"\n", shellNode("a", ""))
	definition, err := yamlgraph.Parse([]byte(text))
	if err != nil {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("parse error echoes the literal: %v", err)
		}
		t.Fatalf("a scalar entry must parse and fail validation naming the secret: %v", err)
	}
	d := diagnosticFor(t, graph.Validate(definition), "E_SECRET_ENTRY_INVALID")
	if !strings.Contains(d.Message, `"gh_token"`) || strings.Contains(d.Message+d.Suggestion, leaked) {
		t.Fatalf("diagnostic = %+v", d)
	}
}

func TestValidateSecretMissingReferenceIsAClearError(t *testing.T) {
	result := validateText(t, secretsGraph("secrets:\n  gh_token: {}\n", shellNode("a", "")))
	d := diagnosticFor(t, result, "E_SECRET_REF_REQUIRED")
	if !strings.Contains(d.Message, `"gh_token"`) || !strings.Contains(d.Suggestion, "secret_ref") || result.Valid() {
		t.Fatalf("diagnostic = %+v", d)
	}
}

func TestValidateSecretNamesReferencesAndProviders(t *testing.T) {
	for _, c := range []struct{ name, entry, code string }{
		{"bad name", "  Gh-Token:\n    secret_ref: env/X\n", "E_SECRET_NAME_INVALID"},
		{"no provider", "  gh_token:\n    secret_ref: JUSTATOKENVALUE123\n", "E_SECRET_REF_INVALID"},
		{"bad grammar", "  gh_token:\n    secret_ref: env/a b\n", "E_SECRET_REF_INVALID"},
		{"unknown provider", "  gh_token:\n    secret_ref: vault/token\n", "E_SECRET_PROVIDER_UNKNOWN"},
		{"reserved provider", "  gh_token:\n    secret_ref: local/token\n", "E_SECRET_PROVIDER_UNAVAILABLE"},
	} {
		result := validateText(t, secretsGraph("secrets:\n"+c.entry, shellNode("a", "")))
		d := diagnosticFor(t, result, c.code)
		if result.Valid() || strings.Contains(d.Message+d.Suggestion, "JUSTATOKENVALUE123") {
			t.Errorf("%s: valid=%v diagnostic=%+v", c.name, result.Valid(), d)
		}
	}
}

func TestValidateSecretEnvUndeclaredInvalidNameAndNodeTypes(t *testing.T) {
	for _, c := range []struct{ name, nodes, code string }{
		{"undeclared secret", shellNode("a", "      secret_env: {GH_TOKEN: nope}\n"), "E_SECRET_ENV_UNDECLARED"},
		{"non-string secret", shellNode("a", "      secret_env: {GH_TOKEN: 7}\n"), "E_SECRET_ENV_INVALID"},
		{"not a mapping", shellNode("a", "      secret_env: [gh_token]\n"), "E_SECRET_ENV_INVALID"},
		{"invalid env name", shellNode("a", "      secret_env: {\"A=B\": gh_token}\n"), "E_SECRET_ENV_NAME_INVALID"},
		{"reserved env name", shellNode("a", "      secret_env: {agentplane_attempt: gh_token}\n"), "E_SECRET_ENV_NAME_INVALID"},
		{"duplicate env name", shellNode("a", "      secret_env: {GH_TOKEN: gh_token, gh_token: gh_token}\n"), "E_SECRET_ENV_DUPLICATE"},
		{"agent node", "  a:\n    type: agent\n    config:\n      adapter: codex\n      workspace: /w\n      task: t\n      secret_env: {GH_TOKEN: gh_token}\n", "E_SECRET_ENV_AGENT_UNSUPPORTED"},
		{"gate node", "  a:\n    type: gate\n    config:\n      secret_env: {GH_TOKEN: gh_token}\n", "E_SECRET_ENV_NODE_TYPE"},
		{"human node", "  a:\n    type: human\n    config:\n      prompt: ok\n      secret_env: {GH_TOKEN: gh_token}\n", "E_SECRET_ENV_NODE_TYPE"},
		{"loop gate", "  a:\n    type: shell\n    loop:\n      gate:\n        config:\n          kind: command\n          secret_env: {GH_TOKEN: gh_token}\n      bindings: []\n    config:\n      command: /bin/true\n", "E_SECRET_ENV_NODE_TYPE"},
	} {
		result := validateText(t, secretsGraph(oneSecret, c.nodes))
		d := diagnosticFor(t, result, c.code)
		if result.Valid() || !strings.Contains(d.Message, `"a"`) {
			t.Errorf("%s: valid=%v, diagnostic must name node a: %+v", c.name, result.Valid(), d)
		}
	}
}

func TestValidateSecretEnvCollisionsCaseInsensitive(t *testing.T) {
	for name, config := range map[string]string{
		"env":           "      env: {gh_token: x}\n",
		"env_allowlist": "      env_allowlist: [Gh_Token]\n",
		"input_env":     "      input_env: {name: GH_TOKEN}\n",
	} {
		nodes := "  a:\n    type: shell\n    inputs:\n      name: {type: string}\n    config:\n      command: /bin/true\n      secret_env: {GH_TOKEN: gh_token}\n" + config
		result := validateText(t, secretsGraph(oneSecret, nodes))
		d := diagnosticFor(t, result, "E_SECRET_ENV_COLLISION")
		if result.Valid() || !strings.Contains(d.Message, `"a"`) || !strings.Contains(d.Message, name) {
			t.Errorf("%s: %+v", name, d)
		}
	}
	agent := "  a:\n    type: agent\n    config:\n      adapter: codex\n      workspace: /w\n      task: t\n      environment_refs: [gh_token]\n      secret_env: {GH_TOKEN: gh_token}\n"
	if !hasDiagnosticCode(validateText(t, secretsGraph(oneSecret, agent)), "E_SECRET_ENV_AGENT_UNSUPPORTED") {
		t.Error("an agent node cannot take secret_env yet")
	}
}

func TestValidateSecretAmbientBypassIsAnErrorInEveryMode(t *testing.T) {
	for name, thief := range map[string]string{
		"env_allowlist":    shellNode("thief", "      env_allowlist: [github_token]\n"),
		"env key":          shellNode("thief", "      env: {GITHUB_TOKEN: x}\n"),
		"input_env":        "  thief:\n    type: shell\n    inputs:\n      t: {type: string}\n    config:\n      command: /bin/true\n      input_env: {t: GITHUB_TOKEN}\n",
		"environment_refs": "  thief:\n    type: agent\n    config:\n      adapter: codex\n      workspace: /w\n      task: t\n      environment_refs: [GITHUB_TOKEN]\n",
	} {
		nodes := shellNode("a", "      secret_env: {GH_TOKEN: gh_token}\n") + thief
		result := validateText(t, secretsGraph(oneSecret, nodes))
		d := diagnosticFor(t, result, "E_SECRET_AMBIENT_BYPASS")
		if result.Valid() || !strings.Contains(d.Message, `"thief"`) || !strings.Contains(d.Message, `"gh_token"`) {
			t.Errorf("%s: %+v", name, d)
		}
	}
	// The assigned node itself may name its own backing variable.
	own := shellNode("a", "      secret_env: {GH_TOKEN: gh_token}\n      env_allowlist: [GITHUB_TOKEN]\n")
	if hasDiagnosticCode(validateText(t, secretsGraph(oneSecret, own)), "E_SECRET_AMBIENT_BYPASS") {
		t.Error("the assigned node is not a bypass")
	}
}

func TestSecretsAreSemanticContentAndRoundTrip(t *testing.T) {
	with := parseDefinition(t, secretsGraph(oneSecret, shellNode("a", "      secret_env: {GH_TOKEN: gh_token}\n")))
	without := parseDefinition(t, secretsGraph("", shellNode("a", "")))
	a, err := yamlgraph.SemanticVersion(with)
	if err != nil {
		t.Fatal(err)
	}
	b, err := yamlgraph.SemanticVersion(without)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("secrets are execution-relevant and must change the semantic version")
	}
	out, err := yamlgraph.Marshal(with)
	if err != nil {
		t.Fatal(err)
	}
	again, err := yamlgraph.Parse(out)
	if err != nil || again.Secrets["gh_token"].SecretRef != "env/GITHUB_TOKEN" {
		t.Fatalf("round trip lost the secret: %v %+v", err, again.Secrets)
	}
	if strings.Contains(string(out), "value") {
		t.Fatalf("marshaled graph must hold references only:\n%s", out)
	}
}

func TestParseOtherSecretsShapesStillFailCleanly(t *testing.T) {
	_, err := yamlgraph.Parse([]byte(secretsGraph("secrets: [a, b]\n", shellNode("a", ""))))
	if err == nil || errors.Is(err, yamlgraph.ErrMissingSchemaVersion) {
		t.Fatalf("secrets as a list must be a parse error, got %v", err)
	}
}

// leakedLiterals are values someone might paste where a name belongs.
var leakedLiterals = []string{"ghp_LiteralTokenPasted1", "Xx-Not_A_Name.9", "123456789012345678"}

func assertNoLiteral(t *testing.T, label, literal string, result graph.ValidationResult) {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), literal) {
		t.Errorf("%s: diagnostics echo %q: %s", label, literal, encoded)
	}
}

func TestSecretEnvUndeclaredNeverEchoesTheValue(t *testing.T) {
	for _, literal := range leakedLiterals {
		result := validateText(t, secretsGraph(oneSecret, shellNode("a", "      secret_env: {GH_TOKEN: \""+literal+"\"}\n")))
		d := diagnosticFor(t, result, "E_SECRET_ENV_UNDECLARED")
		if !strings.Contains(d.Message, `"a"`) || !strings.Contains(d.Message, "GH_TOKEN") {
			t.Errorf("message must still name the node and variable: %+v", d)
		}
		assertNoLiteral(t, "undeclared", literal, result)
	}
	// A well-formed undeclared name is still named so the author can fix it.
	result := validateText(t, secretsGraph(oneSecret, shellNode("a", "      secret_env: {GH_TOKEN: nope}\n")))
	if d := diagnosticFor(t, result, "E_SECRET_ENV_UNDECLARED"); !strings.Contains(d.Message, `"nope"`) {
		t.Errorf("a valid secret name is named: %+v", d)
	}
}

func TestSecretEnvWithoutSecretsBlockIsUndeclared(t *testing.T) {
	result := validateText(t, secretsGraph("", shellNode("a", "      secret_env: {GH_TOKEN: gh_token}\n")))
	d := diagnosticFor(t, result, "E_SECRET_ENV_UNDECLARED")
	if result.Valid() || !strings.Contains(d.Message, `"a"`) {
		t.Fatalf("valid=%v diagnostic=%+v", result.Valid(), d)
	}
}

func TestTopLevelSecretsOfAnotherShapeNeverEchoesTheContent(t *testing.T) {
	for _, c := range []struct{ name, literal, doc string }{
		{"scalar", "ghp_LiteralTokenPasted1", "secrets: ghp_LiteralTokenPasted1\n"},
		{"number-looking", "123456789012345678", "secrets: 123456789012345678\n"},
		{"multi-line", "line-two-of-a-key", "secrets: |\n  -----BEGIN KEY-----\n  line-two-of-a-key\n"},
		{"list", "ghp_LiteralTokenPasted1", "secrets: [ghp_LiteralTokenPasted1]\n"},
		{"entry key not scalar", "ghp_LiteralTokenPasted1", "secrets:\n  ? [ghp_LiteralTokenPasted1]\n  : {secret_ref: env/X}\n"},
	} {
		_, err := yamlgraph.Parse([]byte(secretsGraph(c.doc, shellNode("a", ""))))
		if err == nil {
			t.Errorf("%s: want a parse error", c.name)
			continue
		}
		// yaml.v3 truncates the quoted scalar, so a prefix is a leak too.
		if strings.Contains(err.Error(), c.literal[:6]) || strings.Contains(err.Error(), "-----BE") {
			t.Errorf("%s: error echoes the content: %v", c.name, err)
		}
	}
}

func TestSecretNameAndKeyTextIsQuotedOnlyWhenItMatchesTheGrammar(t *testing.T) {
	const bad = "ghp_Pasted-Token.9"
	cases := []struct{ name, secrets, nodes, code string }{
		{"invalid secret name", "secrets:\n  " + bad + ":\n    secret_ref: env/X\n", shellNode("a", ""), "E_SECRET_NAME_INVALID"},
		{"invalid secret name scalar entry", "secrets:\n  " + bad + ": plain\n", shellNode("a", ""), "E_SECRET_ENTRY_INVALID"},
		{"unknown entry key", "secrets:\n  gh_token:\n    secret_ref: env/X\n    " + bad + ": v\n", shellNode("a", ""), "E_SECRET_ENTRY_INVALID"},
		{"invalid env variable", oneSecret, shellNode("a", "      secret_env: {\""+bad+"=\": gh_token}\n"), "E_SECRET_ENV_NAME_INVALID"},
	}
	for _, c := range cases {
		result := validateText(t, secretsGraph(c.secrets, c.nodes))
		diagnosticFor(t, result, c.code)
		assertNoLiteral(t, c.name, bad, result)
	}
}

func TestUnknownSecretProviderIsNeverEchoed(t *testing.T) {
	const pasted = "sk-proj-abcd1234"
	result := validateText(t, secretsGraph("secrets:\n  tok:\n    secret_ref: "+pasted+"/token\n", shellNode("a", "")))
	d := diagnosticFor(t, result, "E_SECRET_PROVIDER_UNKNOWN")
	if !strings.Contains(d.Message, `"tok"`) {
		t.Errorf("message must still name the secret: %+v", d)
	}
	assertNoLiteral(t, "unknown provider", pasted, result)
}

func TestSecretEnvKeyTextIsQuotedOnlyWhenItMatchesTheGrammar(t *testing.T) {
	const bad = "ghp_Pasted-Token.9"
	const badUpper = "GHP_Pasted-Token.9"
	cases := []struct{ name, nodes, code string }{
		{"undeclared", shellNode("a", "      secret_env: {\""+bad+"\": nope}\n"), "E_SECRET_ENV_UNDECLARED"},
		{"duplicate", shellNode("a", "      secret_env: {\""+bad+"\": gh_token, \""+badUpper+"\": gh_token}\n"), "E_SECRET_ENV_DUPLICATE"},
		{"collision", shellNode("a", "      env: {\""+bad+"\": x}\n      secret_env: {\""+bad+"\": gh_token}\n"), "E_SECRET_ENV_COLLISION"},
	}
	for _, c := range cases {
		result := validateText(t, secretsGraph(oneSecret, c.nodes))
		d := diagnosticFor(t, result, c.code)
		if !strings.Contains(d.Message, `"a"`) {
			t.Errorf("%s: message must still name the node: %+v", c.name, d)
		}
		assertNoLiteral(t, c.name, "Pasted", result)
	}
}

func TestValidateSecretEnvReportsEveryEntryProblemOnce(t *testing.T) {
	result := validateText(t, secretsGraph(oneSecret, shellNode("a", "      secret_env: {\"A=B\": gh_token, GH_TOKEN: nope}\n")))
	var codes []string
	for _, d := range result.Diagnostics {
		if strings.HasPrefix(d.Code, "E_SECRET_ENV_") {
			codes = append(codes, d.Code)
		}
	}
	want := []string{"E_SECRET_ENV_NAME_INVALID", "E_SECRET_ENV_UNDECLARED"}
	if strings.Join(codes, ",") != strings.Join(want, ",") {
		t.Fatalf("codes = %v, want %v", codes, want)
	}
}

func TestAmbientBypassQuotesTheVariableOnlyWhenItMatchesTheGrammar(t *testing.T) {
	const pasted = "ghp_Ab.Cd-Ef12"
	backing := "secrets:\n  gh_token:\n    secret_ref: \"env/" + pasted + "\"\n"
	nodes := shellNode("a", "      secret_env: {GH_TOKEN: gh_token}\n") +
		shellNode("thief", "      env_allowlist: [\""+pasted+"\"]\n")
	result := validateText(t, secretsGraph(backing, nodes))
	d := diagnosticFor(t, result, "E_SECRET_AMBIENT_BYPASS")
	if !strings.Contains(d.Message, `"thief"`) {
		t.Errorf("message must still name the node: %+v", d)
	}
	assertNoLiteral(t, "ambient bypass", "ghp_Ab", result)
	assertNoLiteral(t, "ambient bypass", "Cd-Ef12", result)
}

func TestAmbientBypassNamesAValidIdentifier(t *testing.T) {
	nodes := shellNode("a", "      secret_env: {GH_TOKEN: gh_token}\n") +
		shellNode("thief", "      env_allowlist: [GITHUB_TOKEN]\n")
	d := diagnosticFor(t, validateText(t, secretsGraph(oneSecret, nodes)), "E_SECRET_AMBIENT_BYPASS")
	if !strings.Contains(d.Message, "names GITHUB_TOKEN,") || !strings.Contains(d.Suggestion, "naming GITHUB_TOKEN in") {
		t.Errorf("a valid identifier is still named: %+v", d)
	}
}

func TestValidateSecretAmbientBypassCoversATestCommandLoopGate(t *testing.T) {
	loop := "  a:\n    type: shell\n    inputs: {}\n    outputs: {out: {type: string, required: true}}\n    config:\n      command: /bin/true\n      cwd: /w\n" +
		"    loop:\n      max_attempts: 2\n      gate:\n        inputs: {candidate: {type: string, required: true}}\n        config:\n          check_id: t\n          kind: test_command\n          command: /bin/true\n          cwd: /w\n          side_effects: none\n          input: candidate\n          env_allowlist: [GITHUB_TOKEN]\n"
	result := validateText(t, secretsGraph(oneSecret, loop))
	if !hasDiagnosticCode(result, "E_SECRET_AMBIENT_BYPASS") {
		t.Fatalf("a loop Gate naming a secret's backing variable must be rejected: %+v", result.Diagnostics)
	}
	// The diagnostic points at the loop Gate's configuration, where the name is.
	if d := diagnosticFor(t, result, "E_SECRET_AMBIENT_BYPASS"); d.Location.Path != "nodes.a.loop.gate.config" {
		t.Fatalf("path = %q, want nodes.a.loop.gate.config", d.Location.Path)
	}
}

// The same variable named by a loop's producer and by its Gate is two problems
// in two places: one diagnostic for each, never two identical ones.
func TestAmbientBypassInProducerAndLoopGateIsReportedOncePerPlace(t *testing.T) {
	loop := "  a:\n    type: shell\n    inputs: {}\n    outputs: {out: {type: string, required: true}}\n    config:\n      command: /bin/true\n      cwd: /w\n      env_allowlist: [GITHUB_TOKEN]\n" +
		"    loop:\n      max_attempts: 2\n      gate:\n        inputs: {candidate: {type: string, required: true}}\n        config:\n          check_id: t\n          kind: test_command\n          command: /bin/true\n          cwd: /w\n          side_effects: none\n          input: candidate\n          env_allowlist: [GITHUB_TOKEN]\n"
	var paths []string
	for _, d := range validateText(t, secretsGraph(oneSecret, loop)).Diagnostics {
		if d.Code == "E_SECRET_AMBIENT_BYPASS" {
			paths = append(paths, d.Location.Path)
		}
	}
	if strings.Join(paths, ",") != "nodes.a.config,nodes.a.loop.gate.config" {
		t.Fatalf("bypass paths = %v", paths)
	}
}
