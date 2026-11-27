package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Golden tests pin the externally visible CLI contract (stdout JSON, event
// stream and exit codes) so structural refactors can prove they changed no
// behavior. Run `go test ./cmd/agentplane -run Golden -update` only when a
// contract change is intended and documented by a feature spec or ADR.
var updateGolden = flag.Bool("update", false, "rewrite golden files")

var (
	uuidPattern     = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	timestampFormat = regexp.MustCompile(`"timestamp":"[^"]*"`)
	durationFormat  = regexp.MustCompile(`"duration_ms":[0-9]+`)
	// Run metadata depends on where the checkout lives (F2.9), not on the engine.
	runMetadataFormat = regexp.MustCompile(`,"(?:repo_name|repo_id|repo_branch|repo_commit|runtime_version)":"[^"]*"`)
)

// normalizeGolden replaces the only run-specific values (identifiers, clock
// readings, measured durations) with stable tokens. Identifiers keep their
// identity: the same UUID maps to the same token within one document, so
// correlation between events is still asserted.
func normalizeGolden(raw []byte) []byte {
	tokens := map[string]string{}
	out := uuidPattern.ReplaceAllStringFunc(string(raw), func(id string) string {
		if token, ok := tokens[id]; ok {
			return token
		}
		token := "<id-" + strconv.Itoa(len(tokens)+1) + ">"
		tokens[id] = token
		return token
	})
	out = timestampFormat.ReplaceAllString(out, `"timestamp":"<ts>"`)
	out = durationFormat.ReplaceAllString(out, `"duration_ms":0`)
	out = runMetadataFormat.ReplaceAllString(out, "")
	return []byte(out)
}

func buildGoldenBinary(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agentplane"+executableSuffix())
	build := exec.Command("go", "build", "-o", binary, "./cmd/agentplane")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, output)
	}
	return binary
}

func runGolden(t *testing.T, binary string, args ...string) (stdout []byte, stderr string, exit int) {
	t.Helper()
	command := exec.Command(binary, args...)
	var out, errOut bytes.Buffer
	command.Stdout, command.Stderr = &out, &errOut
	err := command.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		exit = exitErr.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
	}
	return out.Bytes(), errOut.String(), exit
}

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("golden %s differs\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestGoldenRunJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("golden fixtures use POSIX commands")
	}
	binary := buildGoldenBinary(t)
	cases := []struct {
		fixture  string
		wantExit int
	}{
		{"chain", 0},
		{"fail", 4},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			stdout, stderr, exit := runGolden(t, binary, "run", filepath.Join("testdata", "golden", tc.fixture+".yaml"), "--json")
			if exit != tc.wantExit || stderr != "" {
				t.Fatalf("exit=%d stderr=%q", exit, stderr)
			}
			compareGolden(t, tc.fixture+".run.golden.json", normalizeGolden(stdout))
		})
	}
}

func TestGoldenValidateDiagnosticsCorpus(t *testing.T) {
	binary := buildGoldenBinary(t)
	codes := make([]string, 0, len(corpus))
	for code := range corpus {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	dir := t.TempDir()
	var doc strings.Builder
	for _, code := range codes {
		path := filepath.Join(dir, code+".yaml")
		if err := os.WriteFile(path, []byte(corpus[code]), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, exit := runGolden(t, binary, "graph", "validate", path, "--json")
		// The temp directory is the only path-dependent text.
		stdout = bytes.ReplaceAll(stdout, []byte(dir), []byte("<dir>"))
		fmt.Fprintf(&doc, "## %s exit=%d stderr=%q\n%s\n", code, exit, stderr, stdout)
	}
	compareGolden(t, "diagnostics.golden.txt", []byte(doc.String()))
}
