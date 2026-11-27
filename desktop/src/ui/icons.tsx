import type { ReactNode } from 'react'

// Inline SVG glyphs on a 24px grid (no icon font, no remote asset). Decorative:
// the label is always next to the icon, or the button carries an aria-label.

// Solid glyphs filled with currentColor.
const FILLED = {
  projects: <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />,
  runners: <path d="M4 5h16a1.5 1.5 0 0 1 1.5 1.5v3A1.5 1.5 0 0 1 20 11H4A1.5 1.5 0 0 1 2.5 9.5v-3A1.5 1.5 0 0 1 4 5zm0 8h16a1.5 1.5 0 0 1 1.5 1.5v3A1.5 1.5 0 0 1 20 19H4a1.5 1.5 0 0 1-1.5-1.5v-3A1.5 1.5 0 0 1 4 13z" />,
  docs: <path d="M5 4a2 2 0 0 1 2-2h12v17H7a2 2 0 0 0-2 2zm2 15h10V4H7z" />,
  back: <path d="m14 6-6 6 6 6-1.4 1.4L5.2 12l7.4-7.4z" />,
  lock: <path d="M7 10V8a5 5 0 0 1 10 0v2h1a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-9a1 1 0 0 1 1-1zm2 0h6V8a3 3 0 0 0-6 0z" />,
} satisfies Record<string, ReactNode>

// Outline glyphs of the navigation (Tarmind Dashboard UX sidebar), stroked with currentColor.
const STROKED = {
  inbox: (
    <>
      <rect x="3.5" y="3.5" width="7" height="7" rx="1.8" />
      <rect x="13.5" y="3.5" width="7" height="7" rx="1.8" />
      <rect x="3.5" y="13.5" width="7" height="7" rx="1.8" />
      <rect x="13.5" y="13.5" width="7" height="7" rx="1.8" />
    </>
  ),
  runs: <path d="M5 6h14M5 12h14M5 18h9" />,
  approvals: <path d="M12 3 4.5 6v5.5c0 4.5 3.1 8.4 7.5 9.5 4.4-1.1 7.5-5 7.5-9.5V6z" />,
  new: <path d="M12 5v14M5 12h14" />,
  editor: (
    <>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="18" cy="12" r="2.5" />
      <circle cx="6" cy="18" r="2.5" />
      <path d="M8.5 6.5 15.5 11M8.5 17.5 15.5 13" />
    </>
  ),
  welcome: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 8h.01" />
    </>
  ),
  settings: (
    <>
      <path d="M4 7h9M17 7h3M4 17h3M11 17h9" />
      <circle cx="15" cy="7" r="2.2" />
      <circle cx="9" cy="17" r="2.2" />
    </>
  ),
} satisfies Record<string, ReactNode>

export type IconName = keyof typeof FILLED | keyof typeof STROKED

export function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  const stroked = name in STROKED
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      aria-hidden="true"
      focusable="false"
      fill={stroked ? 'none' : 'currentColor'}
      stroke={stroked ? 'currentColor' : undefined}
      strokeWidth={stroked ? 2 : undefined}
      strokeLinecap={stroked ? 'round' : undefined}
      strokeLinejoin={stroked ? 'round' : undefined}
    >
      {stroked ? STROKED[name as keyof typeof STROKED] : FILLED[name as keyof typeof FILLED]}
    </svg>
  )
}
