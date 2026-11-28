import { useId, useState, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { useI18n } from '../i18n/context'

export type Tone = 'n' | 'b' | 'p' | 'x' | 'w'

export function Card({ title, subtitle, action, children, testId }: { title?: ReactNode; subtitle?: ReactNode; action?: ReactNode; children?: ReactNode; testId?: string }) {
  return (
    <section className="card" data-testid={testId}>
      {(title || action) && (
        <header className="card-head">
          <div className="card-titles">
            {title && <h2 className="card-title">{title}</h2>}
            {subtitle && <p className="card-sub">{subtitle}</p>}
          </div>
          {action}
        </header>
      )}
      {children}
    </section>
  )
}

export function Pill({ tone, children, testId, small = false }: { tone: Tone; children: ReactNode; testId?: string; small?: boolean }) {
  return (
    <span className={`pill t-${tone}${small ? ' sm' : ''}`} data-testid={testId}>
      {children}
    </span>
  )
}

export function Tag({ children }: { children: ReactNode }) {
  return <span className="tag">{children}</span>
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'default' | 'primary' | 'danger'; size?: 'sm' | 'md' }

export function Button({ variant = 'default', size = 'md', className = '', type = 'button', ...rest }: ButtonProps) {
  const v = variant === 'primary' ? ' primary' : variant === 'danger' ? ' danger' : ''
  return <button type={type} className={`btn${v}${size === 'sm' ? ' sm' : ''} ${className}`.trim()} {...rest} />
}

export interface Segment<T extends string> {
  value: T
  label: ReactNode
  testId?: string
  /** A disabled segment cannot be picked; the caller shows why in text next to the control. */
  disabled?: boolean
}

/** A pill-shaped single-choice control. Native buttons: Tab moves between segments, Enter/Space picks one. */
export function SegmentedControl<T extends string>({ value, onChange, segments, label }: { value: T; onChange: (v: T) => void; segments: Segment<T>[]; label: string }) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {segments.map((s) => (
        <button key={s.value} type="button" aria-pressed={s.value === value} onClick={() => onChange(s.value)} disabled={s.disabled} data-testid={s.testId}>
          {s.label}
        </button>
      ))}
    </div>
  )
}

/** The "?" help tip: the explanation is hidden until hover or keyboard focus, and Escape closes it. */
export function HelpTip({ children }: { children: ReactNode }) {
  const { t } = useI18n()
  const id = useId()
  const [dismissed, setDismissed] = useState(false)
  return (
    <span className={`tip${dismissed ? ' off' : ''}`} onMouseEnter={() => setDismissed(false)}>
      <button type="button" aria-label={t('help.label')} aria-describedby={id}
        onFocus={() => setDismissed(false)}
        onKeyDown={(e) => e.key === 'Escape' && setDismissed(true)}
      >
        ?
      </button>
      <span role="tooltip" id={id}>
        {children}
      </span>
    </span>
  )
}

/** A headline number. A missing value is shown as "unknown", never as 0. */
export function KPI({ label, value, unknownLabel = '—' }: { label: ReactNode; value?: ReactNode; unknownLabel?: string }) {
  return (
    <div className="kpi">
      <span className="lb">{label}</span>
      <span className="kpi-value">{value === undefined || value === null ? <span className="mu">{unknownLabel}</span> : value}</span>
    </div>
  )
}

export function ListRow({ children, onClick, selected = false, testId }: { children: ReactNode; onClick?: () => void; selected?: boolean; testId?: string }) {
  if (!onClick) return <div className="list-row" data-testid={testId}>{children}</div>
  return (
    <button type="button" className={`list-row action${selected ? ' selected' : ''}`} aria-pressed={selected} onClick={onClick} data-testid={testId}>
      {children}
    </button>
  )
}

export function Field({ label, htmlFor, children }: { label: ReactNode; htmlFor: string; children: ReactNode }) {
  return (
    <div className="field">
      <label className="lb" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
    </div>
  )
}

/** Label/value rows. Values are machine output (ids, paths): shown literally, in mono. */
export function KeyValue({ rows }: { rows: { name: string; value: ReactNode }[] }) {
  return (
    <dl className="kv">
      {rows.map((r) => (
        <div key={r.name}>
          <dt className="mu mono">{r.name}</dt>
          <dd className="mono">{r.value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** One component for every empty, loading and error state, so screens say these things the same way. */
export function StateMessage({ kind, children, action, testId }: { kind: 'empty' | 'loading' | 'error'; children: ReactNode; action?: ReactNode; testId?: string }) {
  return (
    <div className={`empty-state ${kind}`} role={kind === 'error' ? 'alert' : kind === 'loading' ? 'status' : undefined} data-testid={testId ?? (kind === 'empty' ? undefined : `${kind}-state`)}>
      <div>{children}</div>
      {action}
    </div>
  )
}
