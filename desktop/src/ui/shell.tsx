import type { ReactNode } from 'react'
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
        <span className="nav-label">{t(e.label)}</span>
        <span className="soon">{t('nav.soon')}</span>
      </div>
    )
  }
  return (
    <button type="button" className={e.active ? 'nav on' : 'nav'} aria-current={e.active ? 'page' : undefined} onClick={e.onSelect} data-testid={`nav-${e.id}`}>
      <Icon name={e.icon} size={15} />
      <span className="nav-label">{t(e.label)}</span>
      {e.badge ? (
        <span className="nav-count" data-testid="approvals-badge">
          {e.badge}
        </span>
      ) : null}
    </button>
  )
}

export interface SidebarProps {
  /** The primary action under the brand ("New run"). */
  primary?: NavEntry
  entries: NavEntry[]
  /** Recent runs, supplied by the app (the shell holds no data). */
  recent?: ReactNode
  deferred: NavEntry[]
  footer?: NavEntry[]
}

/** Sidebar of the Tarmind Dashboard UX canvas (F2.5b): brand, New run, navigation, recent runs, later surfaces, footer with the profile. */
export function Sidebar({ primary, entries, recent, deferred, footer = [] }: SidebarProps) {
  const { t } = useI18n()
  return (
    <nav className="side" aria-label={t('nav.main')}>
      {/* Brand: right-aligned. On macOS the top-left corner belongs to the
          window's traffic lights, and this strip is draggable. */}
      <div className="side-brand">{t('app.name')}</div>
      {primary && (
        <button type="button" className={primary.active ? 'side-new on' : 'side-new'} aria-current={primary.active ? 'page' : undefined} onClick={primary.onSelect} data-testid={`nav-${primary.id}`}>
          <Icon name={primary.icon} size={14} />
          {t(primary.label)}
        </button>
      )}
      <div className="side-group">
        {entries.map((e) => (
          <Entry key={e.id} e={e} />
        ))}
      </div>
      {recent}
      {deferred.length > 0 && (
        <>
          <div className="side-sec" id="side-later">{t('nav.later')}</div>
          <div className="side-group" role="group" aria-labelledby="side-later">
            {deferred.map((e) => (
              <Entry key={e.id} e={{ ...e, disabled: true }} />
            ))}
          </div>
        </>
      )}
      <div className="side-foot">
        {footer.map((e) => (
          <Entry key={e.id} e={e} />
        ))}
        <div className="side-me">
          <span className="avatar" role="img" aria-label={`${t('topbar.profile')}: ${PROFILE_INITIALS}`} data-testid="profile">
            {PROFILE_INITIALS}
          </span>
          <span className="side-me-name">{t('nav.localProfile')}</span>
        </div>
      </div>
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

/** Bar above the page content: connection state at the right (the profile sits in the sidebar footer, F2.5b).
 *  On macOS the window has no native title bar, so this bar (with the sidebar's
 *  brand strip) is the drag area (see styles.css `.mac`). */
export function GlobalBar({ lost, crumbs }: { lost: boolean; crumbs?: ReactNode[] }) {
  const { t } = useI18n()
  return (
    <header className="global-bar">
      {crumbs ? <TopBar crumbs={crumbs} /> : <span />}
      <div className="g-right">
        <span className={`conn ${lost ? 'bad' : 'ok'}`} data-testid="connection-indicator">
          <span className="dot sm" aria-hidden="true" />
          {lost ? t('topbar.lost') : t('topbar.connected')}
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
