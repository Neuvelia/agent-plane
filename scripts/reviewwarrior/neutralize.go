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

// mentionReference matches a character reference that renders as `@`
// (`&#64;`, `&#x40;`, `&commat;`); GitHub finds mentions in the rendered
// text. Escaping only these keeps `&lt;` from the previous review, which the
// next one may quote, from growing on every round.
var mentionReference = regexp.MustCompile(`&(#0*64;|#[xX]0*40;|commat;)`)

// zeroWidthSpace breaks a mention and is invisible in text and in code.
const zeroWidthSpace = "​"

// neutralize escapes every mention and every HTML tag opener.
func neutralize(text string) string {
	text = mentionReference.ReplaceAllString(text, "&amp;$1")
	text = strings.ReplaceAll(text, "<", "&lt;")
	return mention.ReplaceAllString(text, "@"+zeroWidthSpace+"$1")
}
