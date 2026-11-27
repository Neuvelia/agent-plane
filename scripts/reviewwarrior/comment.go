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

// Comment markers. The workflow finds the previous review and failure notice
// by these, and compose reads the reviewed commit from the SHA marker.
const (
	reviewMarker  = "<!-- review-warrior -->"
	failureMarker = "<!-- review-warrior-failure -->"
	shaMarker     = "<!-- review-warrior-reviewed-sha: %s -->"

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
}

func runRender(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	reviewPath := flags.String("review", "", "review Markdown written by the model")
	head := flags.String("head", "", "reviewed head commit SHA")
	since := flags.String("since", "", "commit an incremental review started from; empty for a full review")
	model := flags.String("model", "", "model that wrote the review")
	if err := flags.Parse(args); err != nil {
		return err
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
		scope: reviewScope{head: *head, since: *since},
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, body)
	return err
}

func renderReview(comment reviewComment) (string, error) {
	scope := "the whole pull request"
	if comment.scope.incremental() {
		scope = fmt.Sprintf("the commits after `%s`", short(comment.scope.since))
	}
	header := fmt.Sprintf("**ReviewWarrior** reviewed `%s` (%s) with `%s`.\n\n"+
		"Unresolved P0 and P1 findings set the `ReviewWarrior` status to failure. "+
		"To dispute a finding, reply with the concrete, checkable reason it is wrong "+
		"and include `/review-warrior review`.\n\n",
		short(comment.head), scope, sanitizeLine(comment.model, 60))
	body := header + stripMarkers(strings.TrimSpace(comment.review)) + "\n\n" +
		fmt.Sprintf(shaMarker, comment.head) + "\n" + reviewMarker + "\n"
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
		"Push again or comment `/review-warrior review` to retry; a human review is needed meanwhile.",
		short(head), sanitizeLine(reason, 300))
	if runURLPattern.MatchString(runURL) {
		text += fmt.Sprintf(" [Run log](%s).", runURL)
	}
	return text + "\n\n" + failureMarker + "\n"
}
