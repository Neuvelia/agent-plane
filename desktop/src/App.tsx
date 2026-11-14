import { useCallback, useEffect, useState } from 'react'
import { listApprovals } from './api'
import { usePoll } from './hooks'
import { useI18n } from './i18n/context'
import { IconRail, Sidebar, TopBar, type NavEntry } from './ui/shell'
import { Approvals, Dashboard, RunConfig, RunInspector } from './views'

type Route = { name: 'runs' } | { name: 'approvals' } | { name: 'new' } | { name: 'run'; id: string }

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
  // Surfaces deferred by F2.5a: greyed out, never navigable.
  const deferred: NavEntry[] = [
    { id: 'projects', icon: 'projects', label: 'nav.projects' },
    { id: 'runners', icon: 'runners', label: 'nav.runners' },
    { id: 'docs', icon: 'docs', label: 'nav.docs' },
    { id: 'settings', icon: 'settings', label: 'nav.settings' },
  ]
  // Canvas screens (inspector, new run) use the narrow rail.
  const canvas = route.name === 'run' || route.name === 'new'
  const crumb = route.name === 'approvals' ? t('nav.approvals') : route.name === 'new' ? t('nav.new') : route.name === 'run' ? t('nav.runs') : t('nav.runs')
  return (
    <div className={canvas ? 'app canvas' : 'app'}>
      {canvas ? <IconRail entries={main} /> : <Sidebar entries={main} deferred={deferred} />}
      <div className="col">
        <TopBar crumbs={[t('app.name'), crumb, ...(route.name === 'run' ? [route.id.slice(0, 8)] : [])]} lost={lost} />
        <main>
          {route.name === 'runs' && <Dashboard onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'run' && <RunInspector key={route.id} runId={route.id} onBack={go('runs')} onLost={onLost} />}
          {route.name === 'approvals' && <Approvals onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'new' && <RunConfig onStarted={(id) => setRoute({ name: 'run', id })} />}
        </main>
      </div>
    </div>
  )
}
