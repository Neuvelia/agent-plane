import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useI18n } from '../i18n/context'
import { modifierLabel, moveFocus, openFromButton } from '../ui/menuKeys'
import { usePresence } from '../ui/presence'
import { Glyph, type GlyphName } from './glyphs'

// Menus and popovers of the graph editor v2. A menu is anchored to its trigger
// (or to a point, for a right-click), opens with a short scale-and-fade, closes
// with the reverse, and owns its keyboard: arrows move, Home/End jump, Escape and
// Tab close and return focus to the trigger. Presence (exit animation, reduced
// motion) and the WAI-ARIA key model are the shared ones of the profile menu
// (`ui/presence.ts`, `ui/menuKeys.ts`). Nothing here decides an action.

/** The exit animation of `.v2-menu.closing` and `.v2-float.closing` (editor-v2.css). */
export const MENU_EXIT_MS = 110

/** Where focus lands when a menu opens. A keyboard opening focuses an item; a pointer
 *  opening focuses the menu box, so no item shows a focus ring; a find menu its field. */
export type MenuFocus = 'first' | 'last' | 'container' | 'input'

/** The focus a trigger's click asks for: a keyboard activation (Enter, Space) reports `detail` 0. */
export const focusForClick = (e: { detail: number }): MenuFocus => (e.detail === 0 ? 'first' : 'container')

/** Arrow keys on a closed trigger open its menu on the first or last item (`openFromButton`). */
export function focusForKey(key: string): MenuFocus | null {
  return openFromButton(key)
}

export type Anchor = { kind: 'element'; el: HTMLElement; align?: 'start' | 'end' } | { kind: 'point'; x: number; y: number }

const GAP = 6
const MARGIN = 8

/** Fixed-position coordinates that keep the menu inside the window. */
function place(anchor: Anchor, w: number, h: number): CSSProperties {
  const vw = window.innerWidth
  const vh = window.innerHeight
  let x: number
  let y: number
  let originY = 'top'
  if (anchor.kind === 'point') {
    x = anchor.x
    y = anchor.y
  } else {
    const r = anchor.el.getBoundingClientRect()
    x = anchor.align === 'end' ? r.right - w : r.left
    y = r.bottom + GAP
    if (y + h > vh - MARGIN && r.top - GAP - h > MARGIN) {
      y = r.top - GAP - h
      originY = 'bottom'
    }
  }
  x = Math.max(MARGIN, Math.min(x, vw - w - MARGIN))
  y = Math.max(MARGIN, Math.min(y, vh - h - MARGIN))
  return { left: x, top: y, transformOrigin: `${anchor.kind === 'element' && anchor.align === 'end' ? 'right' : 'left'} ${originY}` }
}

const items = (root: HTMLElement | null) => [...(root?.querySelectorAll<HTMLElement>('[role^="menuitem"]:not([aria-disabled="true"]),input') ?? [])]

interface MenuProps {
  open: boolean
  anchor: Anchor | null
  onClose: (opts?: { refocus: boolean }) => void
  label: string
  width: number
  id?: string
  testId?: string
  children: ReactNode
  /** Where focus lands on open (see MenuFocus). */
  initialFocus?: MenuFocus
  /** A find field (and its "no match" line) above the items; it is not a menu item. */
  search?: ReactNode
}

export function Menu({ open, anchor, onClose, label, width, id, testId, children, initialFocus = 'container', search }: MenuProps) {
  const { mounted, phase } = usePresence(open, MENU_EXIT_MS)
  const ref = useRef<HTMLDivElement>(null)
  const [style, setStyle] = useState<CSSProperties>({ left: -9999, top: -9999 })
  const last = useRef<Anchor | null>(null)
  if (anchor) last.current = anchor

  useLayoutEffect(() => {
    if (!open || !ref.current || !last.current) return
    const r = ref.current.getBoundingClientRect()
    setStyle(place(last.current, width, r.height))
    const list = items(ref.current)
    const target = initialFocus === 'input' ? ref.current.querySelector<HTMLElement>('input') : initialFocus === 'first' ? list[0] : initialFocus === 'last' ? list[list.length - 1] : ref.current
    target?.focus({ preventScroll: true })
  }, [open, width, initialFocus])

  useEffect(() => {
    if (!open) return
    const down = (e: PointerEvent) => {
      const el = ref.current
      if (!el || el.contains(e.target as Node)) return
      if (last.current?.kind === 'element' && last.current.el.contains(e.target as Node)) return
      onClose({ refocus: false })
    }
    const away = () => onClose({ refocus: false })
    document.addEventListener('pointerdown', down, true)
    window.addEventListener('resize', away)
    window.addEventListener('blur', away)
    return () => {
      document.removeEventListener('pointerdown', down, true)
      window.removeEventListener('resize', away)
      window.removeEventListener('blur', away)
    }
  }, [open, onClose])

  // The keyboard is a native listener: the menu box is a container, not a control.
  useEffect(() => {
    const el = ref.current
    if (!open || !el) return
    el.addEventListener('keydown', onKeyDown)
    return () => el.removeEventListener('keydown', onKeyDown)
  })

  if (!mounted) return null
  return createPortal(
    <div ref={ref} className={`v2-menu ${phase}`} tabIndex={-1} style={{ ...style, width }} data-testid={testId}>
      {search}
      <div id={id} role="menu" aria-label={label} className="v2-mlist">
        {children}
      </div>
    </div>,
    document.body,
  )

  function onKeyDown(e: KeyboardEvent) {
    const list = items(ref.current)
    const at = list.indexOf(document.activeElement as HTMLElement)
    if (e.key === 'Escape' || e.key === 'Tab') {
      e.preventDefault()
      if (e.key === 'Escape') e.stopPropagation()
      onClose({ refocus: true })
      return
    }
    // Home and End belong to the find field while it has focus.
    if ((e.key === 'Home' || e.key === 'End') && (e.target as HTMLElement).tagName === 'INPUT') return
    const next = moveFocus(e.key, at, list.length)
    if (next === null) return
    e.preventDefault()
    list[next]?.focus()
  }
}

interface ItemProps {
  icon?: GlyphName | ReactNode
  label: ReactNode
  /** Keys shown at the right, e.g. ['⌘', 'C']. */
  keys?: string[]
  hint?: ReactNode
  onSelect?: () => void
  danger?: boolean
  /** Not available yet: shown, announced as disabled and tagged "Soon". */
  soon?: boolean
  disabled?: boolean
  current?: boolean
  /** `radio` for a choice among items (the current graph, a step to select). */
  kind?: 'action' | 'radio'
  testId?: string
}

export function MenuItem({ icon, label, keys, hint, onSelect, danger, soon, disabled, current, kind = 'action', testId }: ItemProps) {
  const { t } = useI18n()
  const off = soon || disabled
  return (
    <button
      type="button"
      role={kind === 'radio' ? 'menuitemradio' : 'menuitem'}
      aria-checked={kind === 'radio' ? !!current : undefined}
      aria-disabled={off || undefined}
      className={`v2-mi${danger ? ' bad' : ''}${current ? ' cur' : ''}${off ? ' off' : ''}`}
      onClick={off ? undefined : onSelect}
      title={soon ? t('editor.v2.soonHint') : undefined}
      tabIndex={-1}
      data-testid={testId}
    >
      {typeof icon === 'string' ? <Glyph name={icon as GlyphName} /> : (icon ?? <span className="v2-mi-pad" />)}
      <span className="lbl">{label}</span>
      {soon ? (
        <span className="v2-soon">{t('nav.soon')}</span>
      ) : hint !== undefined ? (
        <span className="kb mu">{hint}</span>
      ) : (
        <span className="kb">
          {keys?.map((k) => (
            <kbd key={k}>{k}</kbd>
          ))}
        </span>
      )}
    </button>
  )
}

export const MenuSep = () => <div className="v2-sep" role="separator" />

export const MenuHeading = ({ children }: { children: ReactNode }) => (
  <div className="v2-mh" role="presentation">
    {children}
  </div>
)

/** Platform modifier for shortcut hints, as a key cap: the ⌘ glyph on macOS, Ctrl elsewhere (`modifierLabel`). */
export const MOD = modifierLabel(typeof navigator === 'undefined' ? '' : navigator.platform || navigator.userAgent) === 'Cmd' ? '⌘' : 'Ctrl'
