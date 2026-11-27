package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// The review shape the prompt asks for. The gate trusts only this shape: a
// review that does not have it is malformed and fails, it is never read as
// "no findings".
const (
	headingVerdict   = "## Verdict"
	headingResolved  = "## Resolved Since Previous Review"
	headingFindings  = "## Findings"
	headingWithdrawn = "## Withdrawn Findings"

	// markerPrefix starts every workflow marker. A section ends at one, and
	// rendering strips any the model wrote, so a review cannot forge one.
	markerPrefix = "<!-- review-warrior"
)

var errMalformedReview = errors.New("malformed review")

// itemPattern matches a top-level numbered entry. Models sometimes escape the
// brackets, so an optional backslash is accepted before each one.
var itemPattern = regexp.MustCompile(`^\d+\.\s+\*\*\\?\[([A-Z]+)\\?\]\\?\[P([0-3])\\?\]`)

var numberedLine = regexp.MustCompile(`^\d+\.\s`)

// sectionSpec names a section and the tags its entries may carry.
type sectionSpec struct {
	heading string
	tags    []string
}

var reviewSections = []sectionSpec{
	{heading: headingVerdict},
	{heading: headingResolved, tags: []string{"RESOLVED"}},
	{heading: headingFindings, tags: []string{"NEW", "REMAINS"}},
	{heading: headingWithdrawn, tags: []string{"WITHDRAWN"}},
}

// finding is one entry of the Findings section.
type finding struct {
	tag      string
	priority int
}

// blocking reports whether the finding holds the merge gate (P0 or P1).
func (f finding) blocking() bool {
	return f.priority <= 1
}

// section returns the body under heading, up to the next level-2 heading or
// workflow marker, and whether the heading was found.
func section(text, heading string) (string, bool) {
	var body []string
	inside := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inside {
			inside = trimmed == heading
			continue
		}
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, markerPrefix) {
			break
		}
		body = append(body, line)
	}
	return strings.TrimSpace(strings.Join(body, "\n")), inside
}

// parseReview validates the review shape and returns its findings.
func parseReview(text string) ([]finding, error) {
	if err := checkHeadingOrder(text); err != nil {
		return nil, err
	}
	var findings []finding
	for _, spec := range reviewSections {
		body, _ := section(text, spec.heading)
		entries, err := parseEntries(spec, body)
		if err != nil {
			return nil, err
		}
		if spec.heading == headingFindings {
			findings = entries
		}
	}
	return findings, nil
}

func checkHeadingOrder(text string) error {
	position := -1
	for _, spec := range reviewSections {
		count := 0
		index := -1
		for i, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == spec.heading {
				count++
				index = i
			}
		}
		if count != 1 {
			return fmt.Errorf("%w: %q appears %d times, want exactly once", errMalformedReview, spec.heading, count)
		}
		if index < position {
			return fmt.Errorf("%w: %q is out of order", errMalformedReview, spec.heading)
		}
		position = index
	}
	return nil
}

func parseEntries(spec sectionSpec, body string) ([]finding, error) {
	if body == "" {
		return nil, fmt.Errorf("%w: %q is empty", errMalformedReview, spec.heading)
	}
	var entries []finding
	for _, line := range strings.Split(body, "\n") {
		if !numberedLine.MatchString(line) || spec.tags == nil {
			continue
		}
		entry, err := parseEntry(spec, line)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parseEntry(spec sectionSpec, line string) (finding, error) {
	match := itemPattern.FindStringSubmatch(line)
	if match == nil {
		return finding{}, fmt.Errorf("%w: entry under %q has no [TAG][Pn] prefix: %q", errMalformedReview, spec.heading, truncate(line, 120))
	}
	for _, tag := range spec.tags {
		if match[1] == tag {
			return finding{tag: tag, priority: int(match[2][0] - '0')}, nil
		}
	}
	return finding{}, fmt.Errorf("%w: tag [%s] is not allowed under %q", errMalformedReview, match[1], spec.heading)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

// runCheck validates a review file and prints the finding counts as
// GITHUB_OUTPUT lines. A malformed review is an error.
func runCheck(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	reviewPath := flags.String("review", "", "review Markdown written by the model")
	if err := flags.Parse(args); err != nil {
		return err
	}
	text, err := os.ReadFile(*reviewPath)
	if err != nil {
		return fmt.Errorf("read review: %w", err)
	}
	findings, err := parseReview(string(text))
	if err != nil {
		return err
	}
	blocking := 0
	for _, f := range findings {
		if f.blocking() {
			blocking++
		}
	}
	_, err = fmt.Fprintf(out, "findings=%d\nblocking=%d\n", len(findings), blocking)
	return err
}
