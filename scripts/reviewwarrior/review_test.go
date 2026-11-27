package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wellFormedReview = `## Verdict

Two findings need a fix before merge.

## Resolved Since Previous Review

1. **[RESOLVED][P2] Unbounded retry** - ` + "`internal/runtime/loop.go:40`" + `
   The loop now stops at the attempt budget.

## Findings

1. **[NEW][P1] Event emitted after state change** - ` + "`internal/runtime/run.go:88`" + `
   A crash between the two loses the transition. Emit first.
2. **\[REMAINS\]\[P0\] Secret in event payload** - ` + "`internal/shell/env.go:12`" + `
   The resolved value reaches the log. Redact it.
3. **[NEW][P3] Duplicate helper** - ` + "`internal/gate/scope.go:5`" + `
   Reuse pathscope.Match.

## Withdrawn Findings

None.
`

func TestParseReviewCountsBlockingFindingsOnlyUnderFindings(t *testing.T) {
	findings, err := parseReview(wellFormedReview)
	if err != nil {
		t.Fatalf("parseReview: %v", err)
	}
	blocking := 0
	for _, f := range findings {
		if f.blocking() {
			blocking++
		}
	}
	if len(findings) != 3 || blocking != 2 {
		t.Fatalf("got %d findings, %d blocking; want 3 and 2", len(findings), blocking)
	}
}

func TestParseReviewAcceptsNoFindings(t *testing.T) {
	review := "## Verdict\n\nNo actionable findings.\n\n## Resolved Since Previous Review\n\nNone.\n\n" +
		"## Findings\n\nNo actionable findings.\n\n## Withdrawn Findings\n\nNone.\n"
	findings, err := parseReview(review)
	if err != nil || len(findings) != 0 {
		t.Fatalf("got %v, %v; want no findings and no error", findings, err)
	}
}

func TestParseReviewRejectsMalformedReviews(t *testing.T) {
	tests := map[string]string{
		"missing section":       strings.Replace(wellFormedReview, "## Withdrawn Findings", "## Withdrawn", 1),
		"duplicated section":    wellFormedReview + "\n## Findings\n\nNo actionable findings.\n",
		"sections out of order": strings.Replace(strings.Replace(strings.Replace(wellFormedReview, "## Findings", "## Tmp", 1), "## Withdrawn Findings", "## Findings", 1), "## Tmp", "## Withdrawn Findings", 1),
		"empty section":         strings.Replace(wellFormedReview, "None.", "", 1),
		"entry without tag":     strings.Replace(wellFormedReview, "**[NEW][P3] Duplicate", "**Duplicate", 1),
		"withdrawn in findings": strings.Replace(wellFormedReview, "[NEW][P3]", "[WITHDRAWN][P3]", 1),
		"unknown priority":      strings.Replace(wellFormedReview, "[NEW][P3]", "[NEW][P7]", 1),
	}
	for name, review := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReview(review); !errors.Is(err, errMalformedReview) {
				t.Fatalf("got %v, want errMalformedReview", err)
			}
		})
	}
}

func TestParseReviewIgnoresIndentedSubLists(t *testing.T) {
	review := strings.Replace(wellFormedReview, "Reuse pathscope.Match.", "Reuse pathscope.Match:\n   1. drop the copy", 1)
	findings, err := parseReview(review)
	if err != nil || len(findings) != 3 {
		t.Fatalf("got %d findings, %v; want 3 and no error", len(findings), err)
	}
}

func TestSectionStopsAtWorkflowMarker(t *testing.T) {
	text := "## Findings\n\n1. **[NEW][P1] A** - `a.go:1`\n<!-- review-warrior-reviewed-sha: x -->\n2. hidden\n"
	body, found := section(text, headingFindings)
	if !found || strings.Contains(body, "hidden") {
		t.Fatalf("got %q, %v; want the body up to the marker", body, found)
	}
}

func TestRunCheckPrintsCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(path, []byte(wellFormedReview), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := run([]string{"check", "-review", path}, &out); err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.String() != "findings=3\nblocking=2\n" {
		t.Fatalf("got %q", out.String())
	}
}
