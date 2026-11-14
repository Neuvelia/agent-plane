import type { ReactNode } from 'react'
import type { Tone } from './primitives'

/** Read-only YAML view with line numbers. The text is rendered as text, never as HTML. */
export function CodeView({ text, testId }: { text: string; testId?: string }) {
  const lines = text.replace(/\n$/, '').split('\n')
  return (
    <pre className="code" data-testid={testId}>
      {lines.map((line, i) => (
        <span className="code-line" key={i}>
          <span className="code-no">{i + 1}</span>
          <span>{highlight(line)}</span>
        </span>
      ))}
    </pre>
  )
}

// Minimal YAML colouring: comment, `key:` and scalar values. Display only.
function highlight(line: string): ReactNode {
  const comment = line.indexOf('#')
  if (comment >= 0 && /^\s*#/.test(line)) return <span className="code-comment">{line}</span>
  const m = /^(\s*(?:- )?)([\w.-]+)(:)(.*)$/.exec(line)
  if (!m) return line
  const rest = m[4] ?? ''
  const cls = /^\s*-?\d+(\.\d+)?\s*$/.test(rest) ? 'code-number' : /\{\{.*\}\}/.test(rest) ? 'code-var' : 'code-string'
  return (
    <>
      {m[1]}
      <span className="code-key">{m[2]}</span>
      {m[3]}
      <span className={cls}>{rest}</span>
    </>
  )
}

export interface EventRow {
  sequence: number
  time: string
  type: string
  node?: string
  detail?: string
  tone?: Tone
}

/** Append-only event list. Shows at most `limit` newest rows (default 200). */
export function EventLog({ rows, limit = 200, emptyLabel }: { rows: EventRow[]; limit?: number; emptyLabel: string }) {
  if (rows.length === 0) return <div className="mu empty-line">{emptyLabel}</div>
  const shown = rows.slice(-limit).reverse()
  return (
    <ol className="event-log" data-testid="event-log">
      {shown.map((r) => (
        <li key={r.sequence} className={r.tone ? `ev-${r.tone}` : ''}>
          <span className="mu mono">{String(r.sequence).padStart(3, '0')}</span>
          <span className="mu mono">{r.time}</span>
          <span className="mono">{r.type}</span>
          <span className="mono mu">{[r.node, r.detail].filter(Boolean).join(' · ')}</span>
        </li>
      ))}
    </ol>
  )
}

export interface Dot {
  key: string
  tone: Tone | 'empty'
  label: string
}

/** One dot per item. Empty slots stay visible so the scale is honest. Each dot has a text label. */
export function DotChart({ dots, slots = dots.length, label }: { dots: Dot[]; slots?: number; label: string }) {
  const empty = Math.max(0, slots - dots.length)
  return (
    <div className="dots" role="img" aria-label={label}>
      {dots.map((d) => (
        <span key={d.key} className={`dot d-${d.tone}`} title={d.label} />
      ))}
      {Array.from({ length: empty }, (_, i) => (
        <span key={`e${i}`} className="dot d-empty" />
      ))}
    </div>
  )
}

export const MAX_TIMELINE_COLUMNS = 600

/** Rows of dots over time; `active[row][col]` marks a filled slot. Columns are capped. */
export function DotTimeline({ rows, label }: { rows: { name: string; active: boolean[] }[]; label: string }) {
  return (
    <div className="timeline" role="img" aria-label={label}>
      {rows.map((r) => (
        <div className="tl-row" key={r.name}>
          <span className="mono mu tl-name">{r.name}</span>
          <span className="tl-dots">
            {r.active.slice(0, MAX_TIMELINE_COLUMNS).map((on, i) => (
              <span key={i} className={`dot sm ${on ? 'd-b' : 'd-empty'}`} />
            ))}
          </span>
        </div>
      ))}
    </div>
  )
}
