import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import type { MessageKey } from '../i18n'
import { useI18n } from '../i18n/context'
import { AboutPane } from './about'
import { AgentsPane } from './agents'
import { EngineGroup, ExecutionGroup, GeneralGroup } from './groups'
import { SoonTag } from './parts'
import { RecordingGroup, useRecording } from './recording'
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

const reducedMotion = () => typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches

/**
 * `visit` changes when Settings is chosen again in the sidebar: the main pane
 * opens, without remounting, so a save in flight and a typed draft are kept.
 */
export function Settings({ onPaneChange, visit = 0 }: { onPaneChange?: (pane: SettingsPane) => void; visit?: number }) {
  const { t, locale } = useI18n()
  const [pane, setPane] = useState<SettingsPane>('main')
  const [active, setActive] = useState<SectionId>('general')
  const [target, setTarget] = useState<GroupId | null>(null)
  const [flash, setFlash] = useState<GroupId | null>(null)
  const engine = useEngineReport()
  const recording = useRecording()
  const spyLock = useRef(0)
  const root = useRef<HTMLDivElement>(null)
  const nav = useRef<HTMLElement>(null)

  const show = useCallback(
    (next: SettingsPane) => {
      setPane(next)
      onPaneChange?.(next)
      // The page scrolls inside the app's main area: a new pane starts at its heading.
      root.current?.scrollIntoView?.({ block: 'start' })
    },
    [onPaneChange],
  )

  const select = (s: Section) => {
    if (!s.pane) return
    setActive(s.id)
    if (s.pane !== pane) show(s.pane)
    if (s.pane === 'main') setTarget(s.id as GroupId)
  }

  const firstVisit = useRef(visit)
  useEffect(() => {
    if (visit === firstVisit.current) return
    firstVisit.current = visit
    setActive('general')
    show('main')
  }, [visit, show])

  // Scroll to the chosen group after the main pane is on screen, then ring it once.
  // Under reduced motion the jump is instant and there is no ring to wait for.
  useEffect(() => {
    if (!target || pane !== 'main') return
    const still = reducedMotion()
    spyLock.current = performance.now() + 800
    document.getElementById(`st-${target}`)?.scrollIntoView({ behavior: still ? 'auto' : 'smooth', block: 'start' })
    setTarget(null)
    if (still) return
    // Cleared first so choosing the same group again rings it again.
    setFlash(null)
    const frame = requestAnimationFrame(() => setFlash(target))
    return () => cancelAnimationFrame(frame)
  }, [target, pane])

  useScrollSpy(pane === 'main', spyLock, setActive)
  const markY = useMarker(nav, active, locale)
  const endFlash = useCallback(() => setFlash(null), [])
  const groupProps = (id: GroupId) => ({ flash: flash === id, onFlashEnd: endFlash })

  return (
    <div className="st" ref={root} data-testid="settings">
      <nav className="st-nav" ref={nav} aria-label={t('settings.nav.label')}>
        <span className="st-nav-mark" aria-hidden="true" style={{ transform: `translateY(${markY}px)` }} />
        {SECTIONS.map((s) =>
          s.pane ? (
            <button key={s.id} type="button" className={s.id === active ? 'on' : undefined} aria-current={s.id === active ? 'true' : undefined} onClick={() => select(s)} data-testid={`settings-nav-${s.id}`}>
              {t(s.label)}
            </button>
          ) : (
            <div key={s.id} className="st-nav-off" aria-disabled="true" data-testid={`settings-nav-${s.id}`}>
              <span className="st-nav-label">{t(s.label)}</span>
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
            <RecordingGroup recording={recording} {...groupProps('recording')} />
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

/** The marker's offset: measured from the active entry, so it follows the real row height and gap. */
function useMarker(nav: RefObject<HTMLElement | null>, active: SectionId, locale: string): number {
  const [y, setY] = useState(0)
  useLayoutEffect(() => {
    const entry = nav.current?.querySelector<HTMLElement>(`[data-testid="settings-nav-${active}"]`)
    if (entry) setY(entry.offsetTop)
  }, [nav, active, locale])
  return y
}

/**
 * While the main pane shows, the visible group nearest the top of the view is
 * the active section. Positions are read when the observer reports, not when a
 * group first crossed, so they are current. Paused briefly after a click so a
 * smooth scroll does not flicker through the groups it passes. Without
 * IntersectionObserver it does nothing.
 */
function useScrollSpy(enabled: boolean, lock: RefObject<number>, setActive: (id: SectionId) => void) {
  useEffect(() => {
    if (!enabled || typeof IntersectionObserver === 'undefined') return
    const visible = new Set<HTMLElement>()
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) visible.add(e.target as HTMLElement)
          else visible.delete(e.target as HTMLElement)
        }
        if (performance.now() < (lock.current ?? 0) || visible.size === 0) return
        const top = [...visible].map((g) => ({ g, y: g.getBoundingClientRect().top })).sort((a, b) => a.y - b.y)[0]!.g
        setActive((top.dataset.group ?? 'general') as SectionId)
      },
      { rootMargin: '0px 0px -55% 0px' },
    )
    document.querySelectorAll<HTMLElement>('.st [data-group]').forEach((g) => observer.observe(g))
    return () => observer.disconnect()
  }, [enabled, lock, setActive])
}
