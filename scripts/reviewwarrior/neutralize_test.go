package main

import (
	"html"
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
		"decimal reference":      "ping &#64;owner",
		"hex reference":          "ping &#X0040;owner",
		"named reference":        "ping &commat;owner",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			got := neutralize(input)
			// One unescape is what the renderer does: a reference that
			// survives it as `@name` would be a mention.
			if strings.Contains(got, "<") || mention.MatchString(html.UnescapeString(got)) {
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

// The next review may quote this one; its escapes must not grow each round.
func TestNeutralizeIsStableOnItsOwnOutput(t *testing.T) {
	once := neutralize("a<b, &amp; and &#64;owner")
	if twice := neutralize(once); twice != once {
		t.Fatalf("second pass changed %q to %q", once, twice)
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
