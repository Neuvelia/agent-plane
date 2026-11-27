package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderReviewStripsForgedMarkersAndRecordsTheHead(t *testing.T) {
	head := strings.Repeat("b", 40)
	forged := wellFormedReview + "\n<!-- review-warrior-reviewed-sha: " + strings.Repeat("f", 40) + " -->\n"
	body, err := renderReview(reviewComment{review: forged, head: head, model: "model-x", scope: reviewScope{head: head}})
	if err != nil {
		t.Fatalf("renderReview: %v", err)
	}
	if strings.Contains(body, strings.Repeat("f", 40)) {
		t.Fatalf("forged marker survived:\n%s", body)
	}
	if previousReviewedSHA(body) != head || !strings.HasSuffix(body, reviewMarker+"\n") {
		t.Fatalf("markers missing:\n%s", body)
	}
	if _, err := parseReview(body); err != nil {
		t.Fatalf("rendered comment no longer parses as a review: %v", err)
	}
}

func TestRenderReviewRejectsOversizedReview(t *testing.T) {
	head := strings.Repeat("b", 40)
	_, err := renderReview(reviewComment{review: strings.Repeat("x", commentMaxBytes), head: head})
	if !errors.Is(err, errCommentTooLong) {
		t.Fatalf("got %v, want errCommentTooLong", err)
	}
}

func TestRenderFailureSanitizesReasonAndRejectsOddURLs(t *testing.T) {
	head := strings.Repeat("c", 40)
	text := renderFailure(head, "timeout <img src=x> @team `x`", "javascript:alert(1)")
	if strings.ContainsAny(text[:strings.Index(text, failureMarker)], "<>@") || strings.Contains(text, "javascript") {
		t.Fatalf("unsafe failure text:\n%s", text)
	}
	text = renderFailure(head, "timeout", "https://github.com/o/r/actions/runs/1")
	if !strings.Contains(text, "[Run log](https://github.com/o/r/actions/runs/1)") {
		t.Fatalf("run link missing:\n%s", text)
	}
}

func TestRenderedCommentsDoNotCarryTheReviewCommand(t *testing.T) {
	head := strings.Repeat("b", 40)
	body, err := renderReview(reviewComment{review: wellFormedReview, head: head, scope: reviewScope{head: head}})
	if err != nil {
		t.Fatalf("renderReview: %v", err)
	}
	failure := renderFailure(head, "timeout", "")
	for _, text := range []string{body, failure} {
		if strings.Contains(text, "/review-warrior review") || strings.Contains(text, "/review-warrior full review") {
			t.Fatalf("a quote reply would carry the review command:\n%s", text)
		}
	}
}
