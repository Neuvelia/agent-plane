import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import type { MessageKey } from '../i18n'
import { useI18n } from '../i18n/context'
import { AboutPane } from './about'
import { AgentsPane } from './agents'
import { EngineGroup, ExecutionGroup, GeneralGroup } from './groups'
import { SoonTag } from './parts'
import { RecordingGroup } from './recording'
import { useEngineReport } from './useEngineReport'

export { parseNames } from './recording'

// F2.5b: the Settings screen of the design canvas. A section list on the left;
// General, Recording, Execution and Engine are groups of one scrolling page,
// Agents and About are panes of their own. Preferences stay where they are
// held (language in the renderer, recording in Electron main); nothing here
// schedules, retries or decides run state.

export type SettingsPane = 'main' | 'agents' | 'about'
type GroupId = 'general' | 'recording' | 'execution' | 'engine'
type SectionId = GroupId | 'agents' | 'about' | 'secrets' | 'policy'

interface Section {
  id: SectionId
  label: MessageKey
  pane?: SettingsPane
}

/** The section list, in the design's order. A section without a pane is not built yet ("Soon"). */
export const SECTIONS: readonly Section[] = [
  { id: 'general', label: 'settings.nav.general', pane: 'main' },
  { id: 'recording', label: 'settings.nav.recording', pane: 'main' },
  { id: 'execution', label: 'settings.nav.execution', pane: 'main' },
  { id: 'agents', label: 'settings.nav.agents', pane: 'agents' },
  { id: 'engine', label: 'settings.nav.engine', pane: 'main' },
  { id: 'secrets', label: 'settings.nav.secrets' },
  { id: 'policy', label: 'settings.nav.policy' },
  { id: 'about', label: 'settings.nav.about', pane: 'about' },
]

/** Height of one section row plus the gap between rows; the active marker moves by this. */
const NAV_STEP = 36
const reducedMotion = () => typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches

export function Settings({ onPaneChange }: { onPaneChange?: (pane: SettingsPane) => void }) {
  const { t } = useI18n()
  const [pane, setPane] = useState<SettingsPane>('main')
  const [active, setActive] = useState<SectionId>('general')
  const [target, setTarget] = useState<GroupId | null>(null)
  const [flash, setFlash] = useState<GroupId | null>(null)
  const engine = useEngineReport()
  const spyLock = useRef(0)

  const select = (s: Section) => {
    if (!s.pane) return
    setActive(s.id)
    if (s.pane !== pane) {
      setPane(s.pane)
      onPaneChange?.(s.pane)
    }
    if (s.pane === 'main') setTarget(s.id as GroupId)
  }

  // Scroll to the chosen group after the main pane is on screen, then mark it briefly.
  useEffect(() => {
    if (!target || pane !== 'main') return
    spyLock.current = performance.now() + 800
    document.getElementById(`st-${target}`)?.scrollIntoView({ behavior: reducedMotion() ? 'auto' : 'smooth', block: 'start' })
    setFlash(target)
    setTarget(null)
  }, [target, pane])

  useScrollSpy(pane === 'main', spyLock, setActive)
  const endFlash = useCallback(() => setFlash(null), [])
  const groupProps = (id: GroupId) => ({ flash: flash === id, onFlashEnd: endFlash })
  const index = SECTIONS.findIndex((s) => s.id === active)

  return (
    <div className="st" data-testid="settings">
      <nav className="st-nav" aria-label={t('settings.nav.label')}>
        <span className="st-nav-mark" aria-hidden="true" style={{ transform: `translateY(${index * NAV_STEP}px)` }} />
        {SECTIONS.map((s) =>
          s.pane ? (
            <button key={s.id} type="button" className={s.id === active ? 'on' : undefined} aria-current={s.id === active ? 'true' : undefined} onClick={() => select(s)} data-testid={`settings-nav-${s.id}`}>
              {t(s.label)}
            </button>
          ) : (
            <div key={s.id} className="st-nav-off" aria-disabled="true" data-testid={`settings-nav-${s.id}`}>
              {t(s.label)}
              <SoonTag />
            </div>
          ),
        )}
      </nav>
      <div key={pane} className="st-pane">
        {pane === 'main' && (
          <div className="st-body">
            <h1>{t('settings.title')}</h1>
            <GeneralGroup {...groupProps('general')} />
            <RecordingGroup {...groupProps('recording')} />
            <ExecutionGroup {...groupProps('execution')} />
            <EngineGroup view={engine.view} {...groupProps('engine')} />
          </div>
        )}
        {pane === 'agents' && <AgentsPane view={engine.view} onCheck={engine.check} onRetry={engine.reload} />}
        {pane === 'about' && <AboutPane view={engine.view} />}
      </div>
    </div>
  )
}

/**
 * While the main pane shows, the group nearest the top of the view becomes the
 * active section. Paused briefly after a click so a smooth scroll does not
 * flicker through the groups it passes. Without IntersectionObserver it does nothing.
 */
function useScrollSpy(enabled: boolean, lock: RefObject<number>, setActive: (id: SectionId) => void) {
  useEffect(() => {
    if (!enabled || typeof IntersectionObserver === 'undefined') return
    const groups = [...document.querySelectorAll<HTMLElement>('.st [data-group]')]
    const visible = new Map<string, number>()
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const id = (e.target as HTMLElement).dataset.group ?? ''
          if (e.isIntersecting) visible.set(id, e.boundingClientRect.top)
          else visible.delete(id)
        }
        if (performance.now() < (lock.current ?? 0) || visible.size === 0) return
        const top = [...visible.entries()].sort((a, b) => a[1] - b[1])[0]![0]
        setActive(top as SectionId)
      },
      { rootMargin: '0px 0px -55% 0px' },
    )
    groups.forEach((g) => observer.observe(g))
    return () => observer.disconnect()
  }, [enabled, lock, setActive])
}
