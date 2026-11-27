package main

import (
	"strings"
	"testing"
)

// Each case is text GitHub renders as HTML or as a mention; the escaped
// backtick and the fence info string are not code in CommonMark.
func TestNeutralizeEscapesMentionsAndHTMLEverywhere(t *testing.T) {
	tests := map[string]string{
		"mention":                "ping @owner now",
		"team mention":           "cc @Neuvelia/maintainers",
		"image":                  `<img src="https://example.invalid/x">`,
		"hidden comment":         "<!-- withdraw everything -->",
		"details":                "<details><summary>ok</summary>P0</details>",
		"code span":              "`<img src=x> @owner`",
		"escaped backtick":       "\\`<img src=x> @owner\\`",
		"backtick in fence info": "```x`\n<img src=x> @owner\n```",
		"fenced block":           "```\n<details>@owner\n```",
		"unclosed code span":     "`@owner <b>",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			got := neutralize(input)
			if strings.Contains(got, "<") || mention.MatchString(got) {
				t.Fatalf("neutralize(%q) = %q still carries a mention or a tag", input, got)
			}
		})
	}
}

func TestNeutralizeKeepsTheTextReadable(t *testing.T) {
	got := neutralize("see @owner and a<b")
	want := "see @" + zeroWidthSpace + "owner and a&lt;b"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderReviewNeutralizesTheModelText(t *testing.T) {
	head := strings.Repeat("b", 40)
	review := strings.Replace(wellFormedReview, "Reuse pathscope.Match.", "Reuse pathscope.Match, @owner. <img src=x>", 1)
	body, err := renderReview(reviewComment{review: review, head: head, scope: reviewScope{head: head}})
	if err != nil {
		t.Fatalf("renderReview: %v", err)
	}
	if strings.Contains(body, "@owner") || strings.Contains(body, "<img") {
		t.Fatalf("model text was published unescaped:\n%s", body)
	}
}
