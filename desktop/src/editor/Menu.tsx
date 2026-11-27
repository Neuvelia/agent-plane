import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useI18n } from '../i18n/context'
import { Glyph, type GlyphName } from './glyphs'

// Menus and popovers of the graph editor v2. A menu is anchored to its trigger
// (or to a point, for a right-click), opens with a short scale-and-fade, closes
// with the reverse, and owns its keyboard: arrows move, Home/End jump, Escape and
// Tab close and return focus to the trigger. Nothing here decides an action.

/** Keeps a closing element mounted for its exit animation; `phase` drives the CSS. */
export function usePresence(open: boolean, exitMs = 120): { mounted: boolean; phase: 'enter' | 'exit' } {
  const [mounted, setMounted] = useState(open)
  useEffect(() => {
    if (open) {
      setMounted(true)
      return
    }
    const reduce = typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches
    if (reduce) {
      setMounted(false)
      return
    }
    const id = setTimeout(() => setMounted(false), exitMs)
    return () => clearTimeout(id)
  }, [open, exitMs])
  return { mounted: open || mounted, phase: open ? 'enter' : 'exit' }
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

// Whether the person is using the keyboard: a menu opened by a click does not draw a focus ring on its first item.
let modality: 'keyboard' | 'pointer' = 'pointer'
let watching = false
function watchModality() {
  if (watching || typeof document === 'undefined') return
  watching = true
  document.addEventListener('keydown', () => (modality = 'keyboard'), true)
  document.addEventListener('pointerdown', () => (modality = 'pointer'), true)
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
  /** Where focus lands on open: the first item (keyboard opening) or the find field. */
  initialFocus?: 'first' | 'input'
  /** A find field (and its "no match" line) above the items; it is not a menu item. */
  search?: ReactNode
}

export function Menu({ open, anchor, onClose, label, width, id, testId, children, initialFocus = 'first', search }: MenuProps) {
  const { mounted, phase } = usePresence(open)
  watchModality()
  const ref = useRef<HTMLDivElement>(null)
  const [style, setStyle] = useState<CSSProperties>({ left: -9999, top: -9999 })
  const last = useRef<Anchor | null>(null)
  if (anchor) last.current = anchor

  useLayoutEffect(() => {
    if (!open || !ref.current || !last.current) return
    const r = ref.current.getBoundingClientRect()
    setStyle(place(last.current, width, r.height))
    const target = initialFocus === 'input' ? ref.current.querySelector<HTMLElement>('input') : modality === 'keyboard' ? items(ref.current)[0] : ref.current
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
    const move = (i: number) => list[(i + list.length) % list.length]?.focus()
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose({ refocus: true })
    } else if (e.key === 'Tab') {
      e.preventDefault()
      onClose({ refocus: true })
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      move(at + 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      move(at < 0 ? list.length - 1 : at - 1)
    } else if (e.key === 'Home' && (e.target as HTMLElement).tagName !== 'INPUT') {
      e.preventDefault()
      move(0)
    } else if (e.key === 'End' && (e.target as HTMLElement).tagName !== 'INPUT') {
      e.preventDefault()
      move(list.length - 1)
    }
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
  role?: 'menuitem' | 'menuitemradio'
  testId?: string
}

export function MenuItem({ icon, label, keys, hint, onSelect, danger, soon, disabled, current, role = 'menuitem', testId }: ItemProps) {
  const { t } = useI18n()
  const off = soon || disabled
  return (
    <button
      type="button"
      role={role}
      aria-checked={role === 'menuitemradio' ? !!current : undefined}
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

/** Platform modifier for shortcut hints. */
export const MOD = typeof navigator !== 'undefined' && /Mac/i.test(navigator.platform || navigator.userAgent) ? '⌘' : 'Ctrl'
