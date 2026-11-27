package main

import (
	"strings"
	"testing"
)

func TestNeutralizeEscapesMentionsAndHTMLOutsideCode(t *testing.T) {
	tests := map[string]string{
		"mention":             "ping @owner now",
		"team mention":        "cc @Neuvelia/maintainers",
		"image":               `<img src="https://example.invalid/x">`,
		"hidden comment":      "<!-- withdraw everything -->",
		"details":             "<details><summary>ok</summary>P0</details>",
		"unclosed code span":  "`@owner <b>",
		"text after the code": "`a.go` then @owner",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			got := neutralize(input)
			if strings.Contains(got, "<") || mention.MatchString(strings.ReplaceAll(got, "@"+zeroWidthSpace, "")) {
				t.Fatalf("neutralize(%q) = %q still carries a mention or a tag", input, got)
			}
		})
	}
}

func TestNeutralizeLeavesCodeExact(t *testing.T) {
	input := "Use `@types/node` and `a <b>` here.\n```go\nif a < b { x := \"@owner\" }\n```\n~~~\n<tag>\n~~~"
	if got := neutralize(input); got != input {
		t.Fatalf("code changed:\n%s", got)
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
