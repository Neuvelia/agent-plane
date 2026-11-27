import type { ReactNode } from 'react'

// Solid 24px-grid glyphs filled with currentColor, written inline (no icon
// font, no remote asset). Decorative: the label is always next to the icon, or
// the button carries an aria-label.
const G: Record<string, ReactNode> = {
  dashboard: <path d="M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z" />,
  runs: <path d="M7 4.6v14.8a1 1 0 0 0 1.5.86l12-7.4a1 1 0 0 0 0-1.72l-12-7.4A1 1 0 0 0 7 4.6z" />,
  approvals: <path d="M12 2.5 4.5 5.6v5.6c0 4.7 3.2 8.9 7.5 10.3 4.3-1.4 7.5-5.6 7.5-10.3V5.6z" />,
  new: <path d="M11 4h2v7h7v2h-7v7h-2v-7H4v-2h7z" />,
  projects: <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />,
  runners: <path d="M4 5h16a1.5 1.5 0 0 1 1.5 1.5v3A1.5 1.5 0 0 1 20 11H4A1.5 1.5 0 0 1 2.5 9.5v-3A1.5 1.5 0 0 1 4 5zm0 8h16a1.5 1.5 0 0 1 1.5 1.5v3A1.5 1.5 0 0 1 20 19H4a1.5 1.5 0 0 1-1.5-1.5v-3A1.5 1.5 0 0 1 4 13z" />,
  docs: <path d="M5 4a2 2 0 0 1 2-2h12v17H7a2 2 0 0 0-2 2zm2 15h10V4H7z" />,
  settings: <path d="M4 7h10v2H4zM16 5h2v6h-2zM18 7h2v2h-2zM4 15h4v2H4zM10 13h2v6h-2zM12 15h8v2h-8z" />,
  editor: <path d="M4 5h6v5H4zM14 14h6v5h-6zM10 7.5h2.5a1.5 1.5 0 0 1 1.5 1.5v5h-2V9.5h-2z" />,
  welcome: <path d="M5 3h2v18H5zM8 4h11l-2.5 4 2.5 4H8z" />,
  file: <path d="M6 2.5h8.5L19 7v14.5H6z" />,
  back: <path d="m14 6-6 6 6 6-1.4 1.4L5.2 12l7.4-7.4z" />,
  lock: <path d="M7 10V8a5 5 0 0 1 10 0v2h1a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-9a1 1 0 0 1 1-1zm2 0h6V8a3 3 0 0 0-6 0z" />,
}

export type IconName = keyof typeof G

export function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
      {G[name]}
    </svg>
  )
}
