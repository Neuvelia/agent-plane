import type { ReactNode } from 'react'
import { LOCALES, LOCALE_LABEL } from '../i18n'
import { useI18n } from '../i18n/context'
import type { MessageKey } from '../i18n'
import { Icon, type IconName } from './icons'
import { SegmentedControl } from './primitives'

export interface NavEntry {
  id: string
  icon: IconName
  label: MessageKey
  badge?: number
  active?: boolean
  onSelect?: () => void
  /** Deferred surface: shown greyed out, not a link, not a tab stop, does nothing. */
  disabled?: boolean
}

function Entry({ e }: { e: NavEntry }) {
  const { t } = useI18n()
  if (e.disabled) {
    // aria-disabled on a non-interactive element: skipped by Tab, no handler.
    return (
      <div className="nav disabled" aria-disabled="true" title={t('nav.soonHint')} data-testid={`nav-${e.id}`}>
        <Icon name={e.icon} />
        <span className="nav-label">{t(e.label)}</span>
        <span className="tag soon">{t('nav.soon')}</span>
      </div>
    )
  }
  return (
    <button type="button" className={e.active ? 'nav on' : 'nav'} aria-current={e.active ? 'page' : undefined} onClick={e.onSelect} data-testid={`nav-${e.id}`}>
      <Icon name={e.icon} />
      <span className="nav-label">{t(e.label)}</span>
      {e.badge ? (
        <span className="badge" data-testid="approvals-badge">
          {e.badge}
        </span>
      ) : null}
    </button>
  )
}

export function Sidebar({ entries, deferred }: { entries: NavEntry[]; deferred: NavEntry[] }) {
  const { t } = useI18n()
  return (
    <nav className="side" aria-label={t('nav.main')}>
      <div className="brand">{t('app.name')}</div>
      {entries.map((e) => (
        <Entry key={e.id} e={e} />
      ))}
      <div className="side-gap" />
      {deferred.map((e) => (
        <Entry key={e.id} e={{ ...e, disabled: true }} />
      ))}
    </nav>
  )
}

/** Narrow rail for canvas screens: icons only, each with an accessible name. */
export function IconRail({ entries }: { entries: NavEntry[] }) {
  const { t } = useI18n()
  return (
    <nav className="rail" aria-label={t('nav.main')}>
      {entries.map((e) => (
        <button key={e.id} type="button" className={e.active ? 'rail-item on' : 'rail-item'} title={t(e.label)} aria-current={e.active ? 'page' : undefined} onClick={e.onSelect} data-testid={`nav-${e.id}`}>
          <Icon name={e.icon} size={18} />
          <span className="sr-only">{t(e.label)}</span>
          {e.badge ? (
            <span className="badge rail-badge" data-testid="approvals-badge">
              {e.badge}
            </span>
          ) : null}
        </button>
      ))}
    </nav>
  )
}

export function TopBar({ crumbs, lost, children }: { crumbs: ReactNode[]; lost: boolean; children?: ReactNode }) {
  const { t, locale, setLocale } = useI18n()
  return (
    <header className="topbar">
      <div className="crumbs">
        {crumbs.map((c, i) => (
          <span key={i} className={i === crumbs.length - 1 ? 'crumb cur' : 'crumb'}>
            {c}
          </span>
        ))}
      </div>
      <div className="topbar-right">
        {children}
        <span className={`conn ${lost ? 'bad' : 'ok'}`} data-testid="connection-indicator">
          <span className="dot sm" aria-hidden="true" />
          {lost ? t('topbar.lost') : t('topbar.connected')}
        </span>
        <SegmentedControl label={t('topbar.language')} value={locale} onChange={setLocale} segments={LOCALES.map((l) => ({ value: l, label: LOCALE_LABEL[l] }))} />
      </div>
    </header>
  )
}
