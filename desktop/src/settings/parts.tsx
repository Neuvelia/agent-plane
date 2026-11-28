import { useLayoutEffect, useRef, useState, type InputHTMLAttributes, type ReactNode } from 'react'
import { useI18n } from '../i18n/context'

// Building blocks of the Settings screen (F2.5f): a titled group, a list of
// rows, and the controls the design draws inside rows. Every control is a
// native element; animation is CSS only and is removed under reduced motion.

/**
 * A titled group: heading, optional one-line description, its rows, then notes
 * below them. `ring` is a counter that is set when the group was chosen in the
 * section list: each new value restarts the ring (two keyframe names alternate,
 * so the same group can ring again). The heading takes focus after a jump.
 */
export function Group({ id, title, description, ring, onRingEnd, children, footer, testId }: { id: string; title: ReactNode; description?: ReactNode; ring?: number | null; onRingEnd?: () => void; children: ReactNode; footer?: ReactNode; testId?: string }) {
  const className = ring ? `st-grp ring-${ring % 2}` : 'st-grp'
  return (
    <section className={className} id={`st-${id}`} aria-labelledby={`st-${id}-h`} data-group={id} data-testid={testId} onAnimationEnd={(e) => e.animationName.startsWith('st-ring') && onRingEnd?.()}>
      <h2 id={`st-${id}-h`} tabIndex={-1}>
        {title}
      </h2>
      {description && <p className="st-desc">{description}</p>}
      <div className="st-list">{children}</div>
      {footer}
    </section>
  )
}

/** One setting: its name, hint and an optional always-visible warning on the left, its control on the right. */
export function Row({ title, hint, warning, children, soon, testId, hintMono = false, hintId, warningId }: { title: ReactNode; hint?: ReactNode; warning?: ReactNode; children?: ReactNode; soon?: boolean; testId?: string; hintMono?: boolean; hintId?: string; warningId?: string }) {
  return (
    <div className={soon ? 'st-row soon' : 'st-row'} data-testid={testId}>
      <div className="st-text">
        <b>{title}</b>
        {hint && (
          <span id={hintId} className={hintMono ? 'mono' : undefined}>
            {hint}
          </span>
        )}
        {warning && (
          <span id={warningId} className="st-warn">
            {warning}
          </span>
        )}
      </div>
      {soon && <SoonTag />}
      {children}
    </div>
  )
}

/** The "Soon" label of a setting with no backing contract yet; its control is disabled. */
export function SoonTag() {
  const { t } = useI18n()
  return (
    <span className="st-soon" title={t('nav.soonHint')}>
      {t('nav.soon')}
    </span>
  )
}

/** An on/off switch: a native checkbox drawn as a switch, so it keeps native semantics. */
export function Switch(props: Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'className'>) {
  return <input type="checkbox" className="st-switch" {...props} />
}

export interface SegmentOption<T extends string> {
  value: T
  label: ReactNode
  testId?: string
}

/**
 * Single choice with a thumb that slides to the picked segment. The thumb is
 * measured from the picked button after layout; before that (and without a
 * DOM) the picked button draws its own background, so nothing depends on it.
 */
export function Segmented<T extends string>({ label, value, options, onChange, disabled = false }: { label: string; value: T; options: SegmentOption<T>[]; onChange?: (v: T) => void; disabled?: boolean }) {
  const keys = options.map((o) => o.value).join('|')
  const group = useRef<HTMLDivElement>(null)
  const [thumb, setThumb] = useState<{ left: number; width: number } | null>(null)
  // Re-measured when the value changes, when the group resizes (a label that
  // changes width) and once the bundled font has loaded.
  useLayoutEffect(() => {
    const el = group.current
    if (!el) return
    const measure = () => {
      const picked = el.querySelector<HTMLButtonElement>('button[aria-pressed="true"]')
      if (!picked || picked.offsetWidth === 0) return
      const next = { left: picked.offsetLeft, width: picked.offsetWidth }
      setThumb((t) => (t && t.left === next.left && t.width === next.width ? t : next))
    }
    measure()
    let alive = true
    void document.fonts?.ready.then(() => alive && measure())
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    el.querySelectorAll('button').forEach((b) => resize?.observe(b))
    return () => {
      alive = false
      resize?.disconnect()
    }
  }, [value, keys])
  return (
    <div ref={group} className={thumb ? 'st-seg measured' : 'st-seg'} role="group" aria-label={label}>
      {thumb && <span className="st-seg-thumb" aria-hidden="true" style={{ transform: `translateX(${thumb.left}px)`, width: thumb.width }} />}
      {options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === value} disabled={disabled} onClick={() => onChange?.(o.value)} data-testid={o.testId}>
          {o.label}
        </button>
      ))}
    </div>
  )
}

/** A number with − and + buttons, kept within [min, max]. Without onChange it is shown disabled. */
export function Stepper({ value, label, min, max, onChange, testId }: { value: number; label: string; min: number; max: number; onChange?: (n: number) => void; testId?: string }) {
  const { t } = useI18n()
  return (
    <div className="st-step" role="group" aria-label={label} data-testid={testId}>
      <button type="button" aria-label={t('settings.decrease')} disabled={!onChange || value <= min} onClick={() => onChange?.(value - 1)}>
        −
      </button>
      <span className="mono">{value}</span>
      <button type="button" aria-label={t('settings.increase')} disabled={!onChange || value >= max} onClick={() => onChange?.(value + 1)}>
        +
      </button>
    </div>
  )
}
