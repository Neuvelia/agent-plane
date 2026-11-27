package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// Comment markers. The workflow finds the previous review and failure notice
// by these, and compose reads the reviewed commit from the SHA marker.
const (
	reviewMarker  = "<!-- review-warrior -->"
	failureMarker = "<!-- review-warrior-failure -->"
	shaMarker     = "<!-- review-warrior-reviewed-sha: %s -->"
	// throughMarker records the creation time of the newest comment the run
	// had read. The next run reads responses from there, so a reply written
	// while a review is running is not skipped.
	throughMarker = "<!-- review-warrior-responses-through: %s -->"

	// commentMaxBytes stays under GitHub's 65,536-character comment limit.
	commentMaxBytes = 60_000
)

var errCommentTooLong = errors.New("review is too long for a pull request comment")

// reviewComment is what the pull request comment states about the run.
type reviewComment struct {
	review string
	head   string
	scope  reviewScope
	model  string
	// through is when the newest comment this run read was created; zero
	// leaves the marker out.
	through time.Time
}

func runRender(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	reviewPath := flags.String("review", "", "review Markdown written by the model")
	head := flags.String("head", "", "reviewed head commit SHA")
	since := flags.String("since", "", "commit an incremental review started from; empty for a full review")
	model := flags.String("model", "", "model that wrote the review")
	through := flags.String("responses-through", "", "RFC 3339 creation time of the newest comment the run read")
	if err := flags.Parse(args); err != nil {
		return err
	}
	throughTime, err := parseOptionalTime(*through)
	if err != nil {
		return fmt.Errorf("parse -responses-through: %w", err)
	}
	if err := validateSHA(*head); err != nil {
		return err
	}
	if *since != "" {
		if err := validateSHA(*since); err != nil {
			return err
		}
	}
	review, err := os.ReadFile(*reviewPath)
	if err != nil {
		return fmt.Errorf("read review: %w", err)
	}
	body, err := renderReview(reviewComment{
		review: string(review), head: *head, model: *model,
		scope: reviewScope{head: *head, since: *since}, through: throughTime,
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, body)
	return err
}

// renderReview builds the comment. Its own text never contains a review
// command: a reply that quotes the comment must not start a paid run, and the
// workflow only honours a command at the start of a comment.
func renderReview(comment reviewComment) (string, error) {
	scope := "the whole pull request"
	if comment.scope.incremental() {
		scope = fmt.Sprintf("the commits after `%s`", short(comment.scope.since))
	}
	header := fmt.Sprintf("**ReviewWarrior** reviewed `%s` (%s) with `%s`.\n\n"+
		"Unresolved P0 and P1 findings set the `ReviewWarrior` status to failure. "+
		"To dispute a finding, write a new comment that starts with the review command "+
		"(see `.github/review-warrior/README.md`) and gives the concrete, checkable reason it is wrong.\n\n",
		short(comment.head), scope, sanitizeLine(comment.model, 60))
	body := header + neutralize(stripMarkers(strings.TrimSpace(comment.review))) + "\n\n" +
		fmt.Sprintf(shaMarker, comment.head) + "\n"
	if !comment.through.IsZero() {
		body += fmt.Sprintf(throughMarker, comment.through.UTC().Format(time.RFC3339)) + "\n"
	}
	body += reviewMarker + "\n"
	if len(body) > commentMaxBytes {
		return "", fmt.Errorf("%w: %d bytes", errCommentTooLong, len(body))
	}
	return body, nil
}

// stripMarkers drops every line that carries a workflow marker, so a review
// cannot forge the reviewed commit or the comment identity.
func stripMarkers(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, markerPrefix) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

var unsafeCharacters = regexp.MustCompile(`[^A-Za-z0-9 .,:;/()_-]`)

// sanitizeLine keeps a short, plain, single-line value: workflow-supplied
// text such as an error reason must not carry Markdown, HTML or mentions.
func sanitizeLine(text string, limit int) string {
	return truncate(strings.TrimSpace(unsafeCharacters.ReplaceAllString(text, " ")), limit)
}

func runRenderFailure(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("render-failure", flag.ContinueOnError)
	head := flags.String("head", "", "head commit SHA")
	reason := flags.String("reason", "", "why the review did not complete")
	runURL := flags.String("run-url", "", "URL of the workflow run")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := validateSHA(*head); err != nil {
		return err
	}
	_, err := io.WriteString(out, renderFailure(*head, *reason, *runURL))
	return err
}

var runURLPattern = regexp.MustCompile(`^https://[A-Za-z0-9.-]+/[A-Za-z0-9._/-]+$`)

func renderFailure(head, reason, runURL string) string {
	text := fmt.Sprintf("**ReviewWarrior** could not finish the review of `%s`: %s.\n\n"+
		"The `ReviewWarrior` status stays failed until a review completes. "+
		"Push again, or retry with a comment that starts with the review command "+
		"(see `.github/review-warrior/README.md`); a human review is needed meanwhile.",
		short(head), sanitizeLine(reason, 300))
	if runURLPattern.MatchString(runURL) {
		text += fmt.Sprintf(" [Run log](%s).", runURL)
	}
	return text + "\n\n" + failureMarker + "\n"
}
