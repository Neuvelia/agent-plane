// Safe display of a recorded value (F2.3 E5b.2 security). A value is untrusted
// text: it is only ever rendered as text, and what could reorder or hide the
// screen is shown as a visible \uXXXX mark instead. This module is pure; it
// never stores, logs or sends the text.

/** One run of displayed text; a `mark` is an escape the person should notice, not text of the value. */
export interface TextPart {
  text: string
  mark: boolean
}

// Controls other than newline and tab (C0, DEL, C1), the line and paragraph
// separators, and every Unicode bidirectional control (marks, embeddings,
// overrides, isolates and the Arabic letter mark).
const HIDDEN = /[\u0000-\u0008\u000B-\u001F\u007F-\u009F\u061C\u200E\u200F\u2028\u2029\u202A-\u202E\u2066-\u2069]/g

const escapeOf = (char: string): string => `\\u${char.charCodeAt(0).toString(16).toUpperCase().padStart(4, '0')}`

/** The text split so that every hidden character is its own marked part. */
export function visibleParts(text: string): TextPart[] {
  const parts: TextPart[] = []
  let from = 0
  for (const m of text.matchAll(HIDDEN)) {
    if (m.index > from) parts.push({ text: text.slice(from, m.index), mark: false })
    parts.push({ text: escapeOf(m[0]), mark: true })
    from = m.index + 1
  }
  if (from < text.length) parts.push({ text: text.slice(from), mark: false })
  return parts
}

/** The text exactly as displayed, marks included; this is what Copy writes. */
export const displayText = (text: string): string => visibleParts(text).map((p) => p.text).join('')

/** Deeper documents are shown raw: indenting them would cost more than they are worth. */
const MAX_DEPTH = 32

const JSON_WHITESPACE = /[ \t\n\r]/
const LITERAL_END = /[ \t\n\r,:\]}]/

function endOfString(text: string, start: number): number {
  let i = start + 1
  while (text[i] !== '"') i += text[i] === '\\' ? 2 : 1
  return i + 1
}

function endOfLiteral(text: string, start: number): number {
  let i = start
  while (i < text.length && !LITERAL_END.test(text[i]!)) i++
  return i
}

function nextToken(text: string, from: number): number {
  let i = from
  while (i < text.length && JSON_WHITESPACE.test(text[i]!)) i++
  return i
}

/**
 * formatJson indents a JSON document and returns it, or null when the formatted
 * view must not be offered. Offered only when the text is one complete JSON
 * document (the platform parser accepts it all) and every number and string
 * literal is copied from the text, never re-printed from a parsed number: a
 * 20-digit integer keeps its digits, 1e999 stays 1e999, key order and duplicate
 * keys are kept. A result equal to the input is also null: there is nothing to toggle.
 */
export function formatJson(text: string): string | null {
  try {
    JSON.parse(text)
  } catch {
    return null
  }
  const out = indent(text)
  return out === null || out === text ? null : out
}

function indent(text: string): string | null {
  const parts: string[] = []
  let depth = 0
  const newline = () => `\n${'  '.repeat(depth)}`
  for (let i = nextToken(text, 0); i < text.length; i = nextToken(text, i)) {
    const c = text[i]!
    if (c === '{' || c === '[') {
      const close = nextToken(text, i + 1)
      if (text[close] === (c === '{' ? '}' : ']')) {
        parts.push(c + text[close]!)
        i = close + 1
        continue
      }
      if (++depth > MAX_DEPTH) return null
      parts.push(c, newline())
      i++
    } else if (c === '}' || c === ']') {
      depth--
      parts.push(newline(), c)
      i++
    } else if (c === ',') {
      parts.push(',', newline())
      i++
    } else if (c === ':') {
      parts.push(': ')
      i++
    } else {
      const end = c === '"' ? endOfString(text, i) : endOfLiteral(text, i)
      parts.push(text.slice(i, end))
      i = end
    }
  }
  return parts.join('')
}

/** UTF-8 size of the carried text, for "first N of M bytes". */
export const carriedBytes = (text: string): number => new TextEncoder().encode(text).length
