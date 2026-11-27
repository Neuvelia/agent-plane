package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Paths of the registry files relative to the repository root (ADR-0038).
const (
	featuresPath = "docs/roadmap/features.yaml"
	phasesPath   = "docs/roadmap/phases.yaml"
	statusPath   = "docs/roadmap/STATUS.md"
)

type registry struct {
	SchemaVersion int            `yaml:"schema_version"`
	NeedsDecision []decisionItem `yaml:"needs_decision"`
	Features      []feature      `yaml:"features"`
}

type decisionItem struct {
	ID      string `yaml:"id"`
	Text    string `yaml:"text"`
	Feature string `yaml:"feature"`
}

type feature struct {
	ID           string       `yaml:"id"`
	Name         string       `yaml:"name"`
	Kind         string       `yaml:"kind"`
	Parent       string       `yaml:"parent"`
	Phase        *int         `yaml:"phase"`
	Priority     string       `yaml:"priority"`
	Status       string       `yaml:"status"`
	OwnerPackage string       `yaml:"owner_package"`
	Spec         string       `yaml:"spec"`
	DependsOn    []string     `yaml:"depends_on"`
	Acceptance   []criterion  `yaml:"acceptance"`
	Evidence     []string     `yaml:"evidence"`
	OutOfScope   []string     `yaml:"out_of_scope"`
	Decisions    []string     `yaml:"decisions"`
	Divergences  []divergence `yaml:"divergences"`
	Tasks        []int        `yaml:"tasks"`
	Note         string       `yaml:"note"`
}

type criterion struct {
	ID       string   `yaml:"id"`
	Text     string   `yaml:"text"`
	Status   string   `yaml:"status"`
	Evidence []string `yaml:"evidence"`
	Issue    int      `yaml:"issue"`
	Note     string   `yaml:"note"`
}

type divergence struct {
	Text       string `yaml:"text"`
	ADR        string `yaml:"adr"`
	Issue      int    `yaml:"issue"`
	DeferredTo string `yaml:"deferred_to"`
}

type phasesFile struct {
	SchemaVersion int     `yaml:"schema_version"`
	Phases        []phase `yaml:"phases"`
	Demos         []demo  `yaml:"demos"`
}

type phase struct {
	ID            int      `yaml:"id" json:"id"`
	Name          string   `yaml:"name" json:"name"`
	Release       string   `yaml:"release" json:"release"`
	Goal          string   `yaml:"goal" json:"goal"`
	ExitCriterion string   `yaml:"exit_criterion" json:"exit_criterion"`
	ExitStatus    string   `yaml:"exit_status" json:"exit_status"`
	ExitEvidence  []string `yaml:"exit_evidence" json:"exit_evidence"`
	Note          string   `yaml:"note" json:"note"`
}

type demo struct {
	ID       string   `yaml:"id"`
	Name     string   `yaml:"name"`
	Text     string   `yaml:"text"`
	Features []string `yaml:"features"`
	Status   string   `yaml:"status"`
	Evidence []string `yaml:"evidence"`
	Note     string   `yaml:"note"`
}

// decodeStrict rejects unknown fields so a typo in the registry is an error,
// not a silently ignored key.
func decodeStrict(root, rel string, target any) error {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parse %s: %w", rel, err)
	}
	return nil
}

func loadRegistry(root string) (registry, phasesFile, error) {
	var features registry
	if err := decodeStrict(root, featuresPath, &features); err != nil {
		return registry{}, phasesFile{}, err
	}
	var phases phasesFile
	if err := decodeStrict(root, phasesPath, &phases); err != nil {
		return registry{}, phasesFile{}, err
	}
	return features, phases, nil
}
