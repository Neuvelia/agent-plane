import { useCallback, useEffect, useRef, useState } from 'react'
import { listApprovals } from './api'
import { ConnectionProvider, useConnectionReport } from './connection'
import { Dashboard } from './dashboardView'
import { usePoll } from './hooks'
import { useI18n } from './i18n/context'
import { EditorScreen } from './editor/EditorScreen'
import { useGraphDocument } from './editor/useGraphDocument'
import { Settings } from './settings'
import { GlobalBar, Sidebar, TopBar, type NavEntry } from './ui/shell'
import type { ChosenGraph } from './types'
import { Approvals, RunConfig, RunInspector, RunsList } from './views'
import { markWelcomeDone, welcomeDone } from './capabilities'
import { Welcome } from './welcome'

type Route = { name: 'dashboard' } | { name: 'runs' } | { name: 'approvals' } | { name: 'new'; graph?: ChosenGraph } | { name: 'editor'; picker?: boolean } | { name: 'settings' } | { name: 'run'; id: string } | { name: 'welcome' }

export default function App() {
  // Every screen that cannot reach the engine reports here; the global bar says so while any does.
  const [lostSources, setLostSources] = useState<ReadonlySet<string>>(new Set())
  const report = useCallback((source: string, isLost: boolean) => {
    setLostSources((prev) => {
      if (prev.has(source) === isLost) return prev
      const next = new Set(prev)
      if (isLost) next.add(source)
      else next.delete(source)
      return next
    })
  }, [])
  return (
    <ConnectionProvider report={report}>
      <Shell lost={lostSources.size > 0} />
    </ConnectionProvider>
  )
}

function Shell({ lost }: { lost: boolean }) {
  const { t, locale } = useI18n()
  // First run (F2.11 requirement 10): the welcome screen until one of its actions is chosen.
  const [route, setRoute] = useState<Route>(() => (welcomeDone() ? { name: 'dashboard' } : { name: 'welcome' }))
  const polled = usePoll(listApprovals, 1500)
  const pending = polled.data?.length ?? 0
  // The open graph lives here so it survives moving to another screen and back.
  const editor = useGraphDocument()
  useConnectionReport('badge', polled.status === 'error')
  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  // After navigating, move focus to the content so keyboard and screen-reader users start there.
  const mainRef = useRef<HTMLElement>(null)
  const first = useRef(true)
  const routeKey = route.name === 'run' ? `run:${route.id}` : route.name === 'new' ? `new:${route.graph?.grant ?? ''}` : route.name === 'editor' && route.picker ? 'editor:picker' : route.name
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    // The welcome screen and the template picker move focus to their own content.
    if (routeKey === 'welcome' || routeKey === 'editor:picker') return
    mainRef.current?.focus({ preventScroll: true })
  }, [routeKey])

  const go = (name: Route['name']) => () => setRoute({ name } as Route)
  const main: NavEntry[] = [
    { id: 'dashboard', icon: 'dashboard', label: 'nav.dashboard', active: route.name === 'dashboard', onSelect: go('dashboard') },
    { id: 'runs', icon: 'runs', label: 'nav.runs', active: route.name === 'runs' || route.name === 'run', onSelect: go('runs') },
    { id: 'approvals', icon: 'approvals', label: 'nav.approvals', badge: pending, active: route.name === 'approvals', onSelect: go('approvals') },
    { id: 'new', icon: 'new', label: 'nav.new', active: route.name === 'new', onSelect: go('new') },
    { id: 'editor', icon: 'editor', label: 'nav.editor', active: route.name === 'editor', onSelect: go('editor') },
  ]
  const footer: NavEntry[] = [{ id: 'welcome', icon: 'welcome', label: 'nav.welcome', active: route.name === 'welcome', onSelect: go('welcome') }, { id: 'settings', icon: 'settings', label: 'nav.settings', active: route.name === 'settings', onSelect: go('settings') }]
  // Surfaces deferred by F2.5a: greyed out, never navigable.
  const deferred: NavEntry[] = [
    { id: 'projects', icon: 'projects', label: 'nav.projects' },
    { id: 'runners', icon: 'runners', label: 'nav.runners' },
    { id: 'docs', icon: 'docs', label: 'nav.docs' },
  ]
  const leaveWelcome = (next: Route, then?: () => void) => () => {
    markWelcomeDone()
    setRoute(next)
    then?.()
  }
  const crumb = route.name === 'welcome' ? t('nav.welcome') : route.name === 'approvals' ? t('nav.approvals') : route.name === 'new' ? t('nav.new') : route.name === 'settings' ? t('nav.settings') : route.name === 'editor' ? t('nav.editor') : route.name === 'dashboard' ? t('nav.dashboard') : t('nav.runs')
  return (
    <div className="app">
      <a
        className="skip-link"
        href="#content"
        onClick={(e) => {
          e.preventDefault()
          mainRef.current?.focus()
        }}
      >
        {t('a11y.skip')}
      </a>
      <Sidebar entries={main} deferred={deferred} footer={footer} />
      <div className="col">
        <GlobalBar lost={lost} />
        <TopBar crumbs={[crumb, ...(route.name === 'run' ? [route.id.slice(0, 8)] : [])]} />
        <main id="content" ref={mainRef} tabIndex={-1}>
          {route.name === 'dashboard' && (
            <Dashboard
              onOpenRun={(id) => setRoute({ name: 'run', id })}
              onNewRun={() => setRoute({ name: 'new' })}
              onOpenGraph={(graph) => setRoute({ name: 'new', graph })}
              onAllRuns={go('runs')}
            />
          )}
          {route.name === 'runs' && <RunsList onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'run' && <RunInspector key={route.id} runId={route.id} onBack={go('runs')} onOpenSettings={go('settings')} />}
          {route.name === 'approvals' && <Approvals onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'settings' && <Settings />}
          {route.name === 'editor' && <EditorScreen g={editor} startWithPicker={route.picker} onRun={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'welcome' && <Welcome onTemplate={leaveWelcome({ name: 'editor', picker: true })} onOpen={leaveWelcome({ name: 'editor' }, editor.open)} onSkip={leaveWelcome({ name: 'dashboard' })} />}
          {route.name === 'new' && <RunConfig key={route.graph?.grant ?? ''} initialGraph={route.graph} onStarted={(id) => setRoute({ name: 'run', id })} />}
        </main>
      </div>
    </div>
  )
}
