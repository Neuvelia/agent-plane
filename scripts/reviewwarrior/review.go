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

// tagPattern finds an entry tag anywhere on a line, escaped or not. A tag
// outside an entry's own prefix means the entry is not where the gate looks
// for it, so the review is malformed rather than silently uncounted.
var tagPattern = regexp.MustCompile(`\\?\[(NEW|REMAINS|RESOLVED|WITHDRAWN)\\?\]`)

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

// emptySection holds the bodies the prompt allows for a tagged section with
// no entries. Either is accepted in any tagged section.
var emptySection = map[string]bool{"None.": true, "No actionable findings.": true}

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

// parseEntries reads a tagged section strictly: it is either an empty-section
// line alone, or numbered entries starting at column 0, each followed
// only by indented lines. Anything else, such as a bulleted or unnumbered
// finding, would be invisible to the gate, so it is malformed.
func parseEntries(spec sectionSpec, body string) ([]finding, error) {
	if body == "" {
		return nil, fmt.Errorf("%w: %q is empty", errMalformedReview, spec.heading)
	}
	if spec.tags == nil || emptySection[body] {
		return nil, nil
	}
	var entries []finding
	for _, line := range strings.Split(body, "\n") {
		entry, isEntry, err := parseLine(spec, line, len(entries) > 0)
		if err != nil {
			return nil, err
		}
		if isEntry {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// parseLine classifies one line of a tagged section: an entry, a blank or
// indented continuation of the previous entry, or an error.
func parseLine(spec sectionSpec, line string, afterEntry bool) (finding, bool, error) {
	if numberedLine.MatchString(line) {
		entry, err := parseEntry(spec, line)
		return entry, err == nil, err
	}
	if strings.TrimSpace(line) == "" {
		return finding{}, false, nil
	}
	if !afterEntry || !startsIndented(line) {
		return finding{}, false, fmt.Errorf("%w: line under %q is neither a numbered entry nor its indented text: %q", errMalformedReview, spec.heading, truncate(line, 120))
	}
	if tagPattern.MatchString(line) {
		return finding{}, false, fmt.Errorf("%w: tag inside the text of an entry under %q: %q", errMalformedReview, spec.heading, truncate(line, 120))
	}
	return finding{}, false, nil
}

func startsIndented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
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
