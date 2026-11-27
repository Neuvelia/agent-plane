import type { ReactNode } from 'react'

// Stroke glyphs of the graph editor v2 (24px grid unless noted), drawn inline in
// currentColor. Decorative: a label is always next to them or on the button.
const S: Record<string, ReactNode> = {
  chevronDown: <path d="m6 9 6 6 6-6" />,
  chevronUp: <path d="m6 15 6-6 6 6" />,
  undo: (
    <>
      <path d="M9 14 4 9l5-5" />
      <path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11" />
    </>
  ),
  redo: (
    <>
      <path d="m15 14 5-5-5-5" />
      <path d="M20 9H9.5a5.5 5.5 0 0 0 0 11H13" />
    </>
  ),
  more: (
    <>
      <circle cx="5" cy="12" r="1.6" fill="currentColor" stroke="none" />
      <circle cx="12" cy="12" r="1.6" fill="currentColor" stroke="none" />
      <circle cx="19" cy="12" r="1.6" fill="currentColor" stroke="none" />
    </>
  ),
  minus: <path d="M5 12h14" />,
  plus: <path d="M12 5v14M5 12h14" />,
  fit: <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />,
  layout: (
    <>
      <rect x="9" y="3" width="6" height="5" rx="1" />
      <rect x="3" y="16" width="6" height="5" rx="1" />
      <rect x="15" y="16" width="6" height="5" rx="1" />
      <path d="M12 8v4M6 16v-4h12v4" />
    </>
  ),
  map: (
    <>
      <path d="M3 6l6-2 6 2 6-2v14l-6 2-6-2-6 2z" />
      <path d="M9 4v14M15 6v14" />
    </>
  ),
  select: <path d="M5 3l14 8-6 1.5L10 19z" />,
  pan: <path d="M8 13V5.5a1.5 1.5 0 0 1 3 0V12M11 11V4a1.5 1.5 0 0 1 3 0v7M14 11V5.5a1.5 1.5 0 0 1 3 0V14c0 4-2.5 7-6 7-2.5 0-4-1.5-5.5-4L4 13.5a1.5 1.5 0 0 1 2.5-1.6L8 14" />,
  comment: <path d="M20 12a8 8 0 0 1-11.6 7.1L4 20l1-4.2A8 8 0 1 1 20 12z" />,
  search: (
    <>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </>
  ),
  close: <path d="M6 6l12 12M18 6 6 18" />,
  check: <path d="M4 12.5 9 17.5 20 6.5" />,
  file: (
    <>
      <path d="M14 3H6v18h12V7z" />
      <path d="M14 3v4h4" />
    </>
  ),
  folder: <path d="M3 7V5h7l2 2h9v12H3z" />,
  reveal: (
    <>
      <path d="M3 7V5h7l2 2h9v12H3z" />
      <path d="M12 11v5M9.5 13.5 12 16l2.5-2.5" />
    </>
  ),
  pencil: <path d="M4 20h4L19 9l-4-4L4 16z" />,
  copy: (
    <>
      <rect x="8" y="8" width="12" height="12" rx="2" />
      <path d="M16 8V4H4v12h4" />
    </>
  ),
  link: (
    <>
      <path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7L11.5 6.8" />
      <path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1.5-1.5" />
    </>
  ),
  trash: <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6" />,
  connect: (
    <>
      <circle cx="5" cy="12" r="2.5" />
      <circle cx="19" cy="12" r="2.5" />
      <path d="M7.5 12h9" />
    </>
  ),
  play: <path d="M7 4.5v15L19 12z" />,
  runFrom: (
    <>
      <path d="M4 4.5v15" />
      <path d="M9 4.5v15L20 12z" />
    </>
  ),
  arrowLeft: <path d="M19 12H5M11 6l-6 6 6 6" />,
  logout: <path d="M14 4h5v16h-5M10 8l-4 4 4 4M6 12h10" />,
}

export type GlyphName = keyof typeof S

export function Glyph({ name, size = 16, width = 2 }: { name: GlyphName; size?: number; width?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={width} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
      {S[name]}
    </svg>
  )
}

/** Filled play triangle on a 12px grid (Save and run, Test this step). */
export const PlaySmall = ({ size = 12 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 12 12" aria-hidden="true" focusable="false">
    <path d="M3 1.8v8.4L10 6z" fill="currentColor" />
  </svg>
)

/** Error: a filled circle with an exclamation mark cut out (never colour alone: a word is always next to it). */
export const ErrorMark = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 12 12" aria-hidden="true" focusable="false" className="g-err">
    <circle cx="6" cy="6" r="5.5" fill="var(--bad)" />
    <path d="M6 3.2v3.4M6 8.6v.1" stroke="var(--bg)" strokeWidth="1.5" strokeLinecap="round" />
  </svg>
)

/** Warning: a filled triangle. */
export const WarnMark = ({ size = 14, color = 'var(--warn)' }: { size?: number; color?: string }) => (
  <svg width={size} height={size} viewBox="0 0 12 12" aria-hidden="true" focusable="false" className="g-warn">
    <path d="M6 1 11.2 10.5H.8z" fill={color} />
  </svg>
)

export const OkMark = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 12 12" fill="none" stroke="var(--ok)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
    <path d="M2 6.5 5 9.5 10 3" />
  </svg>
)

/** The kind glyph of a step, on a 12px grid. Presentation only: an unknown type gets a square. */
export function KindGlyph({ type, size = 12 }: { type: string; size?: number }) {
  const common = { width: size, height: size, viewBox: '0 0 12 12', fill: 'none', stroke: 'currentColor', strokeWidth: 1.4, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true, focusable: false }
  switch (type) {
    case 'shell':
      return (
        <svg {...common}>
          <path d="M2 3l3 3-3 3M6.5 9H10" />
        </svg>
      )
    case 'agent':
      return (
        <svg {...common}>
          <circle cx="6" cy="6" r="4" />
        </svg>
      )
    case 'gate':
      return (
        <svg {...common}>
          <path d="M6 1.5 10.5 6 6 10.5 1.5 6z" />
        </svg>
      )
    case 'human':
      return (
        <svg {...common}>
          <circle cx="6" cy="4" r="1.8" />
          <path d="M2.5 10c.5-2 2-3 3.5-3s3 1 3.5 3" />
        </svg>
      )
    default:
      return (
        <svg {...common}>
          <rect x="2" y="2" width="8" height="8" rx="1.5" />
        </svg>
      )
  }
}
