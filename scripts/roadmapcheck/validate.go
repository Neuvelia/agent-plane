package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Registry values the checker branches on (ADR-0038).
const (
	statusDone       = "done"
	statusDiverged   = "diverged"
	statusNotStarted = "not_started"
	statusDeferred   = "deferred"
	criterionMet     = "met"
	criterionOpen    = "open"
	kindRoadmap      = "roadmap"
	kindDerived      = "derived"
)

// Closed vocabularies of the registry (ADR-0038).
var (
	featureStatuses   = set("not_started", "in_progress", "partial", "done", "diverged", "deferred")
	criterionStatuses = set("open", "met")
	featureKinds      = set("roadmap", "derived", "enabler", "future")
	priorities        = set("P0", "P1", "P2")
	exitStatuses      = set(criterionMet, "partial", "not_met")
	ownerPackages     = set("graph-schema", "execution-core", "event-model", "adapters", "workspace-manager",
		"local-store", "runner-daemon", "cli", "ui", "cloud-api", "release")
	featureIDPattern   = regexp.MustCompile(`^(F\d\.\d+[a-z]?(-E\d+)?|FX\.\d+|REL\d+)$`)
	criterionIDPattern = regexp.MustCompile(`^(F\d\.\d+[a-z]?(-E\d+)?|FX\.\d+|REL\d+)-AC\d+$`)
	adrPattern         = regexp.MustCompile(`^ADR-(\d{4})$`)
)

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

type checker struct {
	root     string
	features map[string]*feature
	criteria map[string]bool
	out      findings
}

func newChecker(root string, reg registry) *checker {
	c := &checker{root: root, features: map[string]*feature{}, criteria: map[string]bool{}}
	for i := range reg.Features {
		item := &reg.Features[i]
		if _, seen := c.features[item.ID]; seen {
			c.out.errorf(item.ID, "duplicate feature id")
			continue
		}
		c.features[item.ID] = item
	}
	return c
}

func (c *checker) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(c.root, rel))
	return err == nil
}

func (c *checker) checkRegistry(reg registry) {
	if reg.SchemaVersion != 1 {
		c.out.errorf(featuresPath, "schema_version must be 1, got %d", reg.SchemaVersion)
	}
	for i := range reg.Features {
		c.checkFeature(&reg.Features[i])
	}
	for _, item := range reg.NeedsDecision {
		if item.Feature != "" && c.features[item.Feature] == nil {
			c.out.errorf("needs_decision "+item.ID, "unknown feature %q", item.Feature)
		}
	}
	c.checkCycles(reg)
}

func (c *checker) checkFeature(f *feature) {
	c.checkIdentity(f)
	c.checkReferences(f)
	for i := range f.Acceptance {
		c.checkCriterion(f, &f.Acceptance[i])
	}
	c.checkEvidence(f.ID, f.Evidence)
	c.checkDone(f)
	c.checkDependenciesDone(f)
}

func (c *checker) checkIdentity(f *feature) {
	if !featureIDPattern.MatchString(f.ID) {
		c.out.errorf(f.ID, "invalid feature id")
	}
	checkVocabulary(&c.out, f.ID, "kind", f.Kind, featureKinds)
	checkVocabulary(&c.out, f.ID, "status", f.Status, featureStatuses)
	checkVocabulary(&c.out, f.ID, "priority", f.Priority, priorities)
	checkVocabulary(&c.out, f.ID, "owner_package", f.OwnerPackage, ownerPackages)
	if f.Name == "" {
		c.out.errorf(f.ID, "name is required")
	}
	if f.Kind == kindRoadmap && (f.Phase == nil || *f.Phase < 0 || *f.Phase > 6) {
		c.out.errorf(f.ID, "a roadmap feature needs phase 0-6")
	}
	if f.Kind == kindDerived && c.features[f.Parent] == nil {
		c.out.errorf(f.ID, "derived feature needs an existing parent, got %q", f.Parent)
	}
}

func checkVocabulary(out *findings, location, field, value string, allowed map[string]bool) {
	if !allowed[value] {
		out.errorf(location, "%s %q is not allowed", field, value)
	}
}

func (c *checker) checkReferences(f *feature) {
	if f.Spec != "" && !c.exists(f.Spec) {
		c.out.errorf(f.ID, "spec %s does not exist", f.Spec)
	}
	for _, dep := range f.DependsOn {
		if c.features[dep] == nil {
			c.out.errorf(f.ID, "depends_on references unknown feature %q", dep)
		}
	}
	for _, adr := range f.Decisions {
		c.checkADR(f.ID, adr)
	}
	for _, item := range f.Divergences {
		c.checkDivergence(f.ID, item)
	}
}

func (c *checker) checkADR(location, adr string) {
	match := adrPattern.FindStringSubmatch(adr)
	if match == nil {
		c.out.errorf(location, "decision %q must look like ADR-0001", adr)
		return
	}
	found, _ := filepath.Glob(filepath.Join(c.root, "docs", "adr", match[1]+"-*.md"))
	if len(found) == 0 {
		c.out.errorf(location, "decision %s has no file in docs/adr", adr)
	}
}

// checkDivergence enforces ADR-0038 decision 3: a divergence is justified by
// an ADR, tracked as an issue, or deferred to a named feature.
func (c *checker) checkDivergence(location string, item divergence) {
	if item.ADR == "" && item.Issue == 0 && item.DeferredTo == "" {
		c.out.errorf(location, "divergence %q needs adr, issue or deferred_to", item.Text)
	}
	if item.ADR != "" {
		c.checkADR(location, item.ADR)
	}
	if item.DeferredTo != "" && c.features[item.DeferredTo] == nil {
		c.out.errorf(location, "divergence deferred_to unknown feature %q", item.DeferredTo)
	}
}

func (c *checker) checkCriterion(f *feature, ac *criterion) {
	if !criterionIDPattern.MatchString(ac.ID) || !hasPrefix(ac.ID, f.ID+"-AC") {
		c.out.errorf(ac.ID, "criterion id must be %s-AC<n>", f.ID)
	}
	if c.criteria[ac.ID] {
		c.out.errorf(ac.ID, "duplicate criterion id")
	}
	c.criteria[ac.ID] = true
	checkVocabulary(&c.out, ac.ID, "status", ac.Status, criterionStatuses)
	if ac.Text == "" {
		c.out.errorf(ac.ID, "text is required")
	}
	if ac.Status == criterionMet && len(ac.Evidence) == 0 {
		c.out.errorf(ac.ID, "a met criterion needs evidence")
	}
	c.checkEvidence(ac.ID, ac.Evidence)
	if ac.Status == criterionMet {
		c.checkTagged(ac)
	}
}

func hasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

// checkDone rejects done while any criterion is open or unproven, and a done
// entry without criteria (derived, enabler) that has no evidence of its own.
func (c *checker) checkDone(f *feature) {
	if f.Status != statusDone {
		return
	}
	for _, ac := range f.Acceptance {
		if ac.Status != criterionMet || len(ac.Evidence) == 0 {
			c.out.errorf(f.ID, "status done but %s is %s", ac.ID, describeCriterion(ac))
		}
	}
	if len(f.Acceptance) == 0 && len(f.Evidence) == 0 {
		c.out.errorf(f.ID, "status done needs acceptance criteria or feature evidence")
	}
}

func describeCriterion(ac criterion) string {
	if ac.Status != criterionMet {
		return ac.Status
	}
	return "met without evidence"
}

func (c *checker) checkDependenciesDone(f *feature) {
	if f.Status != statusDone {
		return
	}
	for _, dep := range f.DependsOn {
		target := c.features[dep]
		if target != nil && target.Status != statusDone && target.Status != statusDiverged {
			c.out.warnf(f.ID, "done but dependency %s is %s", dep, target.Status)
		}
	}
}

func (c *checker) checkCycles(reg registry) {
	state := map[string]int{}
	for _, item := range reg.Features {
		if cycle := c.visit(item.ID, state, nil); cycle != nil {
			c.out.errorf(cycle[0], "dependency cycle: %v", cycle)
			return
		}
	}
}

// visit is a depth-first search; state 1 means on the current path, 2 done.
func (c *checker) visit(id string, state map[string]int, path []string) []string {
	if state[id] == 2 || c.features[id] == nil {
		return nil
	}
	if state[id] == 1 {
		return append(path, id)
	}
	state[id] = 1
	for _, dep := range c.features[id].DependsOn {
		if cycle := c.visit(dep, state, append(path, id)); cycle != nil {
			return cycle
		}
	}
	state[id] = 2
	return nil
}

func (c *checker) checkPhases(file phasesFile) {
	if file.SchemaVersion != 1 {
		c.out.errorf(phasesPath, "schema_version must be 1, got %d", file.SchemaVersion)
	}
	seen := map[int]bool{}
	for _, item := range file.Phases {
		location := fmt.Sprintf("phase %d", item.ID)
		if seen[item.ID] {
			c.out.errorf(location, "duplicate phase")
		}
		seen[item.ID] = true
		checkVocabulary(&c.out, location, "exit_status", item.ExitStatus, exitStatuses)
		c.checkEvidence(location, item.ExitEvidence)
	}
	for _, item := range file.Demos {
		location := "demo " + item.ID
		checkVocabulary(&c.out, location, "status", item.Status, exitStatuses)
		c.checkEvidence(location, item.Evidence)
		for _, id := range item.Features {
			if c.features[id] == nil {
				c.out.errorf(location, "unknown feature %q", id)
			}
		}
	}
}
