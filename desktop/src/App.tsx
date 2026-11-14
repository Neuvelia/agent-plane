import { useCallback, useEffect, useState } from 'react'
import { listApprovals } from './api'
import { usePoll } from './hooks'
import { useI18n } from './i18n/context'
import { Settings } from './settings'
import { GlobalBar, Sidebar, TopBar, type NavEntry } from './ui/shell'
import { Approvals, Dashboard, RunConfig, RunInspector } from './views'

type Route = { name: 'runs' } | { name: 'approvals' } | { name: 'new' } | { name: 'settings' } | { name: 'run'; id: string }

export default function App() {
  const { t, locale } = useI18n()
  const [route, setRoute] = useState<Route>({ name: 'runs' })
  const [lost, setLost] = useState(false)
  const pending = usePoll(listApprovals, [], 1500).length
  const onLost = useCallback((v: boolean) => setLost(v), [])
  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  const go = (name: Route['name']) => () => setRoute({ name } as Route)
  const main: NavEntry[] = [
    { id: 'runs', icon: 'runs', label: 'nav.runs', active: route.name === 'runs' || route.name === 'run', onSelect: go('runs') },
    { id: 'approvals', icon: 'approvals', label: 'nav.approvals', badge: pending, active: route.name === 'approvals', onSelect: go('approvals') },
    { id: 'new', icon: 'new', label: 'nav.new', active: route.name === 'new', onSelect: go('new') },
  ]
  const footer: NavEntry[] = [{ id: 'settings', icon: 'settings', label: 'nav.settings', active: route.name === 'settings', onSelect: go('settings') }]
  // Surfaces deferred by F2.5a: greyed out, never navigable.
  const deferred: NavEntry[] = [
    { id: 'projects', icon: 'projects', label: 'nav.projects' },
    { id: 'runners', icon: 'runners', label: 'nav.runners' },
    { id: 'docs', icon: 'docs', label: 'nav.docs' },
  ]
  const crumb = route.name === 'approvals' ? t('nav.approvals') : route.name === 'new' ? t('nav.new') : route.name === 'settings' ? t('nav.settings') : t('nav.runs')
  return (
    <div className="app">
      <Sidebar entries={main} deferred={deferred} footer={footer} />
      <div className="col">
        <GlobalBar lost={lost} />
        <TopBar crumbs={[crumb, ...(route.name === 'run' ? [route.id.slice(0, 8)] : [])]} />
        <main>
          {route.name === 'runs' && <Dashboard onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'run' && <RunInspector key={route.id} runId={route.id} onBack={go('runs')} onLost={onLost} />}
          {route.name === 'approvals' && <Approvals onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'settings' && <Settings />}
          {route.name === 'new' && <RunConfig onStarted={(id) => setRoute({ name: 'run', id })} />}
        </main>
      </div>
    </div>
  )
}
