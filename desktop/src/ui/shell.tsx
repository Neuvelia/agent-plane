import type { ReactNode } from 'react'
import logo from '../design/tarmind-logo.png'
import { useI18n } from '../i18n/context'
import type { MessageKey } from '../i18n'
import { Icon, type IconName } from './icons'

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

export function Sidebar({ entries, deferred, footer = [] }: { entries: NavEntry[]; deferred: NavEntry[]; footer?: NavEntry[] }) {
  const { t } = useI18n()
  return (
    <nav className="side" aria-label={t('nav.main')}>
      {/* Brand: top of the sidebar, right-aligned. On macOS the top-left corner
          belongs to the window's traffic lights, and this strip is draggable. */}
      <div className="side-brand">
        <img src={logo} alt="" width={28} height={28} />
        <span>{t('app.name')}</span>
      </div>
      {entries.map((e) => (
        <Entry key={e.id} e={e} />
      ))}
      <div className="side-gap" />
      {deferred.map((e) => (
        <Entry key={e.id} e={{ ...e, disabled: true }} />
      ))}
      {footer.map((e) => (
        <Entry key={e.id} e={e} />
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

/** Placeholder identity until accounts exist (F2.5a): initials are hard-coded. */
export const PROFILE_INITIALS = 'KW'

/** Bar above the page content: connection state and profile at the right.
 *  On macOS the window has no native title bar, so this bar (with the sidebar's
 *  brand strip) is the drag area (see styles.css `.mac`). */
export function GlobalBar({ lost }: { lost: boolean }) {
  const { t } = useI18n()
  return (
    <header className="global-bar">
      <div className="g-right">
        <span className={`conn ${lost ? 'bad' : 'ok'}`} data-testid="connection-indicator">
          <span className="dot sm" aria-hidden="true" />
          {lost ? t('topbar.lost') : t('topbar.connected')}
        </span>
        <span className="avatar" role="img" aria-label={`${t('topbar.profile')}: ${PROFILE_INITIALS}`} data-testid="profile">
          {PROFILE_INITIALS}
        </span>
      </div>
    </header>
  )
}

export function TopBar({ crumbs }: { crumbs: ReactNode[] }) {
  const { t } = useI18n()
  return (
    // A nav, not a header: the global bar is the page's only banner landmark.
    <nav className="topbar" aria-label={t('topbar.breadcrumb')}>
      <div className="crumbs">
        {crumbs.map((c, i) => (
          <span key={i} className={i === crumbs.length - 1 ? 'crumb cur' : 'crumb'}>
            {c}
          </span>
        ))}
      </div>
    </nav>
  )
}
