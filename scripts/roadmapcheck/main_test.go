package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validFeatures = `schema_version: 1
needs_decision: []
features:
  - id: F0.1
    name: "Schema"
    kind: roadmap
    phase: 0
    priority: P0
    status: done
    owner_package: graph-schema
    depends_on: []
    acceptance:
      - id: F0.1-AC1
        text: "Round trip keeps meaning."
        status: met
        evidence:
          - "pkg/schema_test.go::TestRoundTrip"
    out_of_scope: []
    decisions: ["ADR-0001"]
    divergences: []
    tasks: []
  - id: F0.2
    name: "Validator"
    kind: roadmap
    phase: 0
    priority: P0
    status: in_progress
    owner_package: graph-schema
    depends_on: ["F0.1"]
    acceptance:
      - id: F0.2-AC1
        text: "Invalid graph exits non-zero."
        status: open
        evidence: []
    out_of_scope: []
    decisions: []
    divergences: []
    tasks: []
`

const validPhases = `schema_version: 1
phases:
  - id: 0
    name: "Core"
    release: "v0.1"
    goal: "Prove semantics."
    exit_criterion: "Three nodes run."
    exit_status: partial
    exit_evidence: []
demos:
  - id: A
    name: "Graph matters"
    text: "Parallel branches."
    features: [F0.1]
    status: met
    evidence: ["pkg/schema_test.go::TestRoundTrip"]
`

// tagPrefix is split so this file does not itself carry criterion tags.
const tagPrefix = "// road" + "map: "

const taggedTest = "package pkg\n\n" + tagPrefix + "F0.1-AC1\nfunc TestRoundTrip(t *testing.T) {}\n"

// fixture writes a minimal repository and returns its root.
func fixture(t *testing.T, features, phases, testFile string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		featuresPath:                 features,
		phasesPath:                   phases,
		"pkg/schema_test.go":         testFile,
		"docs/adr/0001-first-adr.md": "# ADR-0001\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runCheck(t *testing.T, root string, mode statusMode) (bool, string) {
	t.Helper()
	var out bytes.Buffer
	failed, err := run(root, mode, &out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return failed, out.String()
}

func TestValidRegistryPassesAndStatusIsStableAcrossRuns(t *testing.T) {
	root := fixture(t, validFeatures, validPhases, taggedTest)
	if failed, out := runCheck(t, root, writeStatus); failed {
		t.Fatalf("write mode failed:\n%s", out)
	}
	first, _ := os.ReadFile(filepath.Join(root, statusPath))
	if failed, out := runCheck(t, root, verifyStatus); failed {
		t.Fatalf("verify mode failed:\n%s", out)
	}
	runCheck(t, root, writeStatus)
	second, _ := os.ReadFile(filepath.Join(root, statusPath))
	if !bytes.Equal(first, second) {
		t.Fatal("STATUS.md differs between two runs on the same input")
	}
	if !strings.Contains(string(first), "| 0 | Core | 1/2 (50%) |") {
		t.Fatalf("summary row missing:\n%s", first)
	}
}

func TestRegistryErrorsFailTheRun(t *testing.T) {
	cases := []struct {
		name     string
		features string
		testFile string
		want     string
	}{
		{"done with an open criterion", strings.Replace(validFeatures, "status: met", "status: open", 1), taggedTest,
			"status done but F0.1-AC1 is open"},
		{"met without evidence", strings.Replace(validFeatures, "evidence:\n          - \"pkg/schema_test.go::TestRoundTrip\"", "evidence: []", 1), taggedTest,
			"a met criterion needs evidence"},
		{"evidence test missing", strings.Replace(validFeatures, "::TestRoundTrip", "::TestGone", 1), taggedTest,
			`"TestGone" not found`},
		{"evidence file missing", strings.Replace(validFeatures, "pkg/schema_test.go", "pkg/gone_test.go", 1), taggedTest,
			"evidence file pkg/gone_test.go does not exist"},
		{"met criterion without tag", validFeatures, "package pkg\n\nfunc TestRoundTrip(t *testing.T) {}\n",
			"no evidence test file carries the tag"},
		{"unknown dependency", strings.Replace(validFeatures, `depends_on: ["F0.1"]`, `depends_on: ["F9.9"]`, 1), taggedTest,
			`unknown feature "F9.9"`},
		{"dependency cycle", strings.Replace(validFeatures, "depends_on: []", `depends_on: ["F0.2"]`, 1), taggedTest,
			"dependency cycle"},
		{"duplicate feature id", strings.Replace(validFeatures, "id: F0.2\n", "id: F0.1\n", 1), taggedTest,
			"duplicate feature id"},
		{"unknown status", strings.Replace(validFeatures, "status: in_progress", "status: unknown", 1), taggedTest,
			`status "unknown" is not allowed`},
		{"missing ADR file", strings.Replace(validFeatures, "ADR-0001", "ADR-0099", 1), taggedTest,
			"decision ADR-0099 has no file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t, tc.features, validPhases, tc.testFile)
			failed, out := runCheck(t, root, writeStatus)
			if !failed || !strings.Contains(out, tc.want) {
				t.Fatalf("want failure containing %q, got failed=%v:\n%s", tc.want, failed, out)
			}
		})
	}
}

func TestDoneWithUnfinishedDependencyOnlyWarns(t *testing.T) {
	features := strings.Replace(validFeatures, `depends_on: []`, `depends_on: ["F0.2"]`, 1)
	features = strings.Replace(features, `depends_on: ["F0.1"]`, `depends_on: []`, 1)
	root := fixture(t, features, validPhases, taggedTest)
	failed, out := runCheck(t, root, writeStatus)
	if failed || !strings.Contains(out, "warning: F0.1: done but dependency F0.2 is in_progress") {
		t.Fatalf("want a warning and no failure, got failed=%v:\n%s", failed, out)
	}
}

func TestOrphanTagOnlyWarns(t *testing.T) {
	root := fixture(t, validFeatures, validPhases, taggedTest+"\n"+tagPrefix+"F7.1-AC9\nfunc TestOther(t *testing.T) {}\n")
	failed, out := runCheck(t, root, writeStatus)
	if failed || !strings.Contains(out, "tag F7.1-AC9 names no criterion") {
		t.Fatalf("want an orphan warning and no failure, got failed=%v:\n%s", failed, out)
	}
}

func TestVitestNameTagCountsAsTag(t *testing.T) {
	features := strings.Replace(validFeatures, "pkg/schema_test.go::TestRoundTrip", "desktop/test/x.test.ts::F0.1-AC1", 1)
	root := fixture(t, features, strings.Replace(validPhases, "pkg/schema_test.go::TestRoundTrip", "pkg/schema_test.go", 1), taggedTest)
	path := filepath.Join(root, "desktop/test/x.test.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("it('keeps meaning [F0.1-"+"AC1]', () => {})\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if failed, out := runCheck(t, root, writeStatus); failed {
		t.Fatalf("vitest name tag not accepted:\n%s", out)
	}
}

func TestStaleStatusFailsVerification(t *testing.T) {
	root := fixture(t, validFeatures, validPhases, taggedTest)
	runCheck(t, root, writeStatus)
	if err := os.WriteFile(filepath.Join(root, statusPath), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failed, out := runCheck(t, root, verifyStatus)
	if !failed || !strings.Contains(out, "out of date") {
		t.Fatalf("want a stale STATUS.md error, got failed=%v:\n%s", failed, out)
	}
}

func TestUnknownFieldIsALoadError(t *testing.T) {
	root := fixture(t, strings.Replace(validFeatures, "    tasks: []\n", "    tasks: []\n    colour: red\n", 1), validPhases, taggedTest)
	if _, err := run(root, writeStatus, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("want an unknown-field error, got %v", err)
	}
}
