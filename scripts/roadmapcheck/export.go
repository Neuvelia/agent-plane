package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The export feeds the claude.ai roadmap dashboard (docs/roadmap/README.md,
// "Dashboard"): one JSON file per dataset, each `{rows, meta}` with rows in
// registry order. Output depends only on the registry and the revision label.

type exportFile struct {
	Rows any          `json:"rows"`
	Meta []exportMeta `json:"meta"`
}

type exportMeta struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type featureRow struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Parent        string `json:"parent"`
	Phase         *int   `json:"phase"`
	Priority      string `json:"priority"`
	Status        string `json:"status"`
	OwnerPackage  string `json:"owner_package"`
	CriteriaMet   int    `json:"criteria_met"`
	CriteriaTotal int    `json:"criteria_total"`
	Issues        string `json:"issues"`
	Spec          string `json:"spec"`
	Note          string `json:"note"`
}

type criterionRow struct {
	ID       string `json:"id"`
	Feature  string `json:"feature"`
	Phase    *int   `json:"phase"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
	Text     string `json:"text"`
	Issue    *int   `json:"issue"`
	Evidence int    `json:"evidence"`
	Note     string `json:"note"`
}

type divergenceRow struct {
	Feature    string `json:"feature"`
	Text       string `json:"text"`
	ADR        string `json:"adr"`
	Issue      *int   `json:"issue"`
	DeferredTo string `json:"deferred_to"`
}

type demoRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Features string `json:"features"`
	Note     string `json:"note"`
}

func exportDashboard(dir, revision string, reg registry, phases phasesFile) error {
	meta := []exportMeta{
		{Label: "Source", Value: featuresPath + ", " + phasesPath},
		{Label: "Generator", Value: "go run ./scripts/roadmapcheck -export DIR -revision REV"},
		{Label: "Revision", Value: revision},
	}
	files := map[string]any{
		"features.json":    featureRows(reg),
		"criteria.json":    criterionRows(reg),
		"phases.json":      phases.Phases,
		"demos.json":       demoRows(phases),
		"divergences.json": divergenceRows(reg),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create export directory: %w", err)
	}
	for name, rows := range files {
		data, err := json.MarshalIndent(exportFile{Rows: rows, Meta: meta}, "", " ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func featureRows(reg registry) []featureRow {
	rows := make([]featureRow, 0, len(reg.Features))
	for _, item := range reg.Features {
		met := 0
		for _, ac := range item.Acceptance {
			if ac.Status == criterionMet {
				met++
			}
		}
		rows = append(rows, featureRow{ID: item.ID, Name: item.Name, Kind: item.Kind, Parent: item.Parent, Phase: item.Phase,
			Priority: item.Priority, Status: item.Status, OwnerPackage: item.OwnerPackage, CriteriaMet: met,
			CriteriaTotal: len(item.Acceptance), Issues: issues(item.Tasks), Spec: item.Spec, Note: item.Note})
	}
	return rows
}

func criterionRows(reg registry) []criterionRow {
	rows := []criterionRow{}
	for _, item := range reg.Features {
		for _, ac := range item.Acceptance {
			rows = append(rows, criterionRow{ID: ac.ID, Feature: item.ID, Phase: item.Phase, Priority: item.Priority,
				Status: ac.Status, Text: ac.Text, Issue: optionalIssue(ac.Issue), Evidence: len(ac.Evidence), Note: ac.Note})
		}
	}
	return rows
}

func divergenceRows(reg registry) []divergenceRow {
	rows := []divergenceRow{}
	for _, item := range reg.Features {
		for _, entry := range item.Divergences {
			rows = append(rows, divergenceRow{Feature: item.ID, Text: entry.Text, ADR: entry.ADR,
				Issue: optionalIssue(entry.Issue), DeferredTo: entry.DeferredTo})
		}
	}
	return rows
}

func demoRows(phases phasesFile) []demoRow {
	rows := make([]demoRow, 0, len(phases.Demos))
	for _, item := range phases.Demos {
		features := ""
		for i, id := range item.Features {
			if i > 0 {
				features += ", "
			}
			features += id
		}
		rows = append(rows, demoRow{ID: item.ID, Name: item.Name, Status: item.Status, Features: features, Note: item.Note})
	}
	return rows
}

// optionalIssue keeps "no issue" as null rather than 0 (unknown is not zero).
func optionalIssue(number int) *int {
	if number == 0 {
		return nil
	}
	return &number
}
