// Package archtest enforces the layer dependency rules of AGENTS.md
// ("Clean architecture rules" R1, R2, R4) by reading production imports.
//
// Layers, inside out: domain -> app -> adapter / delivery -> cmd (composition
// root). Packages are classified by path; packages that have not been moved
// yet are classified by the legacy table below, which shrinks to nothing as
// the refactor lands (docs/audits/2026-11-16-clean-architecture-refactor-plan.md).
//
// Known violations are recorded in baseline.txt. The test fails on a violation
// that is not in the baseline AND on a baseline entry that no longer occurs, so
// the baseline can only shrink.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "agentplane"

type layer string

const (
	layerDomain   layer = "domain"
	layerApp      layer = "app"
	layerAdapter  layer = "adapter"
	layerDelivery layer = "delivery"
	layerRoot     layer = "root"
	layerNeutral  layer = "neutral" // importable by every layer, e.g. version metadata
)

// legacyLayers classifies packages that predate the layered layout.
var legacyLayers = map[string]layer{
	"internal/graph":             layerDomain,
	"internal/events":            layerDomain,
	"internal/graph/edit":        layerAdapter,
	"internal/runtime":           layerApp,
	"internal/agent":             layerAdapter,
	"internal/agent/codex":       layerAdapter,
	"internal/shell":             layerAdapter,
	"internal/gate":              layerAdapter,
	"internal/gitstate":          layerAdapter,
	"internal/store":             layerAdapter,
	"internal/api":               layerDelivery,
	"internal/version":           layerNeutral,
	"internal/fields":            layerNeutral, // config field descriptors shared by domain, adapters and delivery
	"internal/fields/fieldstest": layerNeutral,
	"internal/testenv":           layerNeutral, // test-only helpers
	"internal/capability":        layerAdapter, // probes Git and agent adapters (F2.11)
	"cmd/agentplane":             layerRoot,
	"internal/agent/adaptertest": layerAdapter,
}

// forbiddenStdlib are standard-library packages that domain and application
// code must reach only through ports (R4).
var forbiddenStdlib = map[string]bool{
	"os": true, "os/exec": true, "net": true, "net/http": true,
	"crypto/rand": true, "math/rand": true, "io/ioutil": true,
}

func classify(pkg string) (layer, bool) {
	if l, ok := legacyLayers[pkg]; ok {
		return l, true
	}
	switch {
	case pkg == "cmd/agentplane" || strings.HasPrefix(pkg, "cmd/agentplane/"):
		return layerRoot, true
	case strings.HasPrefix(pkg, "internal/domain/"):
		return layerDomain, true
	case strings.HasPrefix(pkg, "internal/app/"):
		return layerApp, true
	case strings.HasPrefix(pkg, "internal/adapter/"):
		return layerAdapter, true
	case strings.HasPrefix(pkg, "internal/delivery/"):
		return layerDelivery, true
	}
	return "", false
}

// family groups packages that form one adapter (internal/agent and
// internal/agent/codex are one adapter); imports within a family are allowed.
func family(pkg string) string {
	parts := strings.Split(pkg, "/")
	switch {
	case strings.HasPrefix(pkg, "internal/adapter/") && len(parts) >= 3:
		return strings.Join(parts[:3], "/")
	case len(parts) >= 2:
		return strings.Join(parts[:2], "/")
	}
	return pkg
}

func isProject(path string) bool {
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

// isStdlib reports whether an import path belongs to the standard library
// (its first element has no dot).
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".") && !isProject(path)
}

type violation struct{ importer, imported, rule string }

func (v violation) String() string { return v.importer + " -> " + v.imported + " : " + v.rule }

func check(importer string, from layer, imported string) []violation {
	var out []violation
	add := func(rule string) { out = append(out, violation{importer, imported, rule}) }
	switch {
	case isStdlib(imported):
		if (from == layerDomain || from == layerApp) && forbiddenStdlib[imported] {
			add("R4 domain/app must not use " + imported + " directly")
		}
	case !isProject(imported):
		if from == layerDomain || from == layerApp {
			add("R1 domain/app must be standard-library only")
		}
	default:
		pkg := strings.TrimPrefix(strings.TrimPrefix(imported, modulePath), "/")
		to, ok := classify(pkg)
		if !ok {
			add("unclassified package " + pkg)
			return out
		}
		out = append(out, checkLayers(importer, from, imported, pkg, to)...)
	}
	return out
}

func checkLayers(importer string, from layer, imported, pkg string, to layer) []violation {
	var out []violation
	add := func(rule string) { out = append(out, violation{importer, imported, rule}) }
	if to == layerNeutral || family(importer) == family(pkg) || from == layerRoot {
		return nil
	}
	switch from {
	case layerDomain:
		if to != layerDomain {
			add("R1 domain imports only domain")
		}
	case layerApp:
		if to != layerDomain && to != layerApp {
			add("R1 app imports only domain and app")
		}
	case layerAdapter:
		if to == layerAdapter {
			add("R2 adapters must not import other adapters")
		}
		if to == layerDelivery || to == layerRoot {
			add("R1 adapter must not import delivery")
		}
	case layerDelivery:
		if to == layerAdapter || to == layerDelivery {
			add("R1 delivery must not import adapters")
		}
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func productionImports(t *testing.T, root string) map[string][]string {
	t.Helper()
	imports := map[string][]string{}
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			pkg := filepath.ToSlash(rel)
			if pkg == "internal/archtest" {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				imported, _ := strconv.Unquote(spec.Path.Value)
				imports[pkg] = append(imports[pkg], imported)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return imports
}

func collect(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for pkg, imports := range productionImports(t, repoRoot(t)) {
		from, ok := classify(pkg)
		if !ok {
			seen[violation{pkg, "-", "unclassified package"}.String()] = true
			continue
		}
		for _, imported := range imports {
			for _, v := range check(pkg, from, imported) {
				seen[v.String()] = true
			}
		}
	}
	lines := make([]string, 0, len(seen))
	for line := range seen {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return lines
}

func readBaseline(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("baseline.txt")
	if err != nil {
		t.Fatal(err)
	}
	baseline := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			baseline[line] = true
		}
	}
	return baseline
}

func TestDependencyRules(t *testing.T) {
	baseline := readBaseline(t)
	current := collect(t)
	present := map[string]bool{}
	for _, line := range current {
		present[line] = true
		if !baseline[line] {
			t.Errorf("new architecture violation (fix it; do not add it to baseline.txt):\n  %s", line)
		}
	}
	for line := range baseline {
		if !present[line] {
			t.Errorf("baseline.txt lists a violation that is gone; delete the line:\n  %s", line)
		}
	}
}

func TestRulesDetectViolations(t *testing.T) {
	cases := []struct {
		name     string
		from     layer
		importer string
		imported string
		want     bool
	}{
		{"domain imports third party", layerDomain, "internal/domain/graph", "go.yaml.in/yaml/v3", true},
		{"domain imports os", layerDomain, "internal/domain/graph", "os", true},
		{"domain imports app", layerDomain, "internal/domain/graph", modulePath + "/internal/app/engine", true},
		{"domain imports domain", layerDomain, "internal/domain/run", modulePath + "/internal/domain/graph", false},
		{"app imports adapter", layerApp, "internal/app/engine", modulePath + "/internal/adapter/shell", true},
		{"adapter imports adapter", layerAdapter, "internal/adapter/shell", modulePath + "/internal/adapter/gitstate", true},
		{"adapter imports app port", layerAdapter, "internal/adapter/shell", modulePath + "/internal/app/engine", false},
		{"delivery imports adapter", layerDelivery, "internal/delivery/httpapi", modulePath + "/internal/adapter/runstore", true},
		{"root imports anything", layerRoot, "cmd/agentplane", modulePath + "/internal/adapter/shell", false},
		{"anyone imports version", layerDomain, "internal/domain/graph", modulePath + "/internal/version", false},
	}
	for _, tc := range cases {
		if got := len(check(tc.importer, tc.from, tc.imported)) > 0; got != tc.want {
			t.Errorf("%s: violation=%v, want %v", tc.name, got, tc.want)
		}
	}
}
