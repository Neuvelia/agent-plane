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
import { Approvals, RunConfig, RunInspector, RunsList } from './views'

type Route = { name: 'dashboard' } | { name: 'runs' } | { name: 'approvals' } | { name: 'new'; path?: string } | { name: 'editor' } | { name: 'settings' } | { name: 'run'; id: string }

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
  const [route, setRoute] = useState<Route>({ name: 'dashboard' })
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
  const routeKey = route.name === 'run' ? `run:${route.id}` : route.name === 'new' ? `new:${route.path ?? ''}` : route.name
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
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
  const footer: NavEntry[] = [{ id: 'settings', icon: 'settings', label: 'nav.settings', active: route.name === 'settings', onSelect: go('settings') }]
  // Surfaces deferred by F2.5a: greyed out, never navigable.
  const deferred: NavEntry[] = [
    { id: 'projects', icon: 'projects', label: 'nav.projects' },
    { id: 'runners', icon: 'runners', label: 'nav.runners' },
    { id: 'docs', icon: 'docs', label: 'nav.docs' },
  ]
  const crumb = route.name === 'approvals' ? t('nav.approvals') : route.name === 'new' ? t('nav.new') : route.name === 'settings' ? t('nav.settings') : route.name === 'editor' ? t('nav.editor') : route.name === 'dashboard' ? t('nav.dashboard') : t('nav.runs')
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
              onOpenGraph={(path) => setRoute({ name: 'new', path })}
              onAllRuns={go('runs')}
            />
          )}
          {route.name === 'runs' && <RunsList onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'run' && <RunInspector key={route.id} runId={route.id} onBack={go('runs')} />}
          {route.name === 'approvals' && <Approvals onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'settings' && <Settings />}
          {route.name === 'editor' && <EditorScreen g={editor} onRun={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'new' && <RunConfig key={route.path ?? ''} initialPath={route.path} onStarted={(id) => setRoute({ name: 'run', id })} />}
        </main>
      </div>
    </div>
  )
}
