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
var tagPattern = regexp.MustCompile(`\\?\[(NEW|REMAINS|RESOLVED|WITHDRAWN|P[0-9])\\?\]`)

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

// parseReview validates the review shape and returns its findings. Every line
// of the review is accounted for: blank lines before the first heading, the
// four headings exactly and in order, and the body of the section they open.
// Text the gate does not read, such as a finding under an extra heading or
// before the first section, makes the review malformed.
func parseReview(text string) ([]finding, error) {
	bodies, err := splitSections(text)
	if err != nil {
		return nil, err
	}
	var findings []finding
	for i, spec := range reviewSections {
		entries, err := parseEntries(spec, bodies[i])
		if err != nil {
			return nil, err
		}
		if spec.heading == headingFindings {
			findings = entries
		}
	}
	return findings, nil
}

// headingLine matches a Markdown ATX heading at any indentation.
var headingLine = regexp.MustCompile(`^\s*#{1,6}(\s|$)`)

// splitSections returns the body of each review section, in reviewSections
// order. A heading may only be the next expected one, written exactly and at
// column 0; any other heading-like line, and any workflow marker, is an error.
func splitSections(text string) ([]string, error) {
	lines := make([][]string, len(reviewSections))
	current := -1
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		next, err := nextSection(line, current)
		if err != nil {
			return nil, err
		}
		if next != current {
			current = next
			continue
		}
		if current < 0 && line != "" {
			return nil, fmt.Errorf("%w: text before %q: %q", errMalformedReview, headingVerdict, truncate(line, 120))
		}
		if current >= 0 {
			lines[current] = append(lines[current], line)
		}
	}
	if current != len(reviewSections)-1 {
		return nil, fmt.Errorf("%w: %q is missing", errMalformedReview, reviewSections[current+1].heading)
	}
	bodies := make([]string, len(lines))
	for i, body := range lines {
		bodies[i] = strings.TrimSpace(strings.Join(body, "\n"))
	}
	return bodies, nil
}

// nextSection returns the section a line belongs to: current, or the next
// one when the line is its heading.
func nextSection(line string, current int) (int, error) {
	if strings.Contains(line, markerPrefix) {
		return current, fmt.Errorf("%w: workflow marker in the review", errMalformedReview)
	}
	if !headingLine.MatchString(line) {
		return current, nil
	}
	if current+1 < len(reviewSections) && line == reviewSections[current+1].heading {
		return current + 1, nil
	}
	return current, fmt.Errorf("%w: unexpected heading %q; only the four review sections, in order, are allowed", errMalformedReview, truncate(line, 80))
}

// parseEntries reads a tagged section strictly: it is either an empty-section
// line alone, or numbered entries starting at column 0, each followed
// only by indented lines. Anything else, such as a bulleted or unnumbered
// finding, would be invisible to the gate, so it is malformed.
func parseEntries(spec sectionSpec, body string) ([]finding, error) {
	if body == "" {
		return nil, fmt.Errorf("%w: %q is empty", errMalformedReview, spec.heading)
	}
	if spec.tags == nil {
		return nil, rejectTags(spec, body)
	}
	if emptySection[body] {
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

// rejectTags fails a section without entries, such as the verdict, that
// carries an entry tag: a finding written there would not reach the gate.
func rejectTags(spec sectionSpec, body string) error {
	if match := tagPattern.FindString(body); match != "" {
		return fmt.Errorf("%w: tag %s under %q, which takes no entries", errMalformedReview, match, spec.heading)
	}
	return nil
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
