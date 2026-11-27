package main

import (
	"regexp"
	"strings"
)

// The model's review is untrusted text published under the workflow's
// identity. GitHub would turn `@name` into a notification and `<tag>` into
// HTML (images that call out, hidden comments, details blocks that hide a
// finding). Both are escaped everywhere, inside code too: deciding what
// Markdown treats as code means re-implementing its parser (escaped
// backticks, fence info strings), and every gap in that would publish raw
// HTML. The cost is cosmetic: a `<` inside code shows as `&lt;`.

var mention = regexp.MustCompile(`@([A-Za-z0-9])`)

// zeroWidthSpace breaks a mention and is invisible in text and in code.
const zeroWidthSpace = "​"

// neutralize escapes every mention and every HTML tag opener.
func neutralize(text string) string {
	text = strings.ReplaceAll(text, "<", "&lt;")
	return mention.ReplaceAllString(text, "@"+zeroWidthSpace+"$1")
}
