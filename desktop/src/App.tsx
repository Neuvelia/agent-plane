import { useState } from 'react'
import { listApprovals } from './api'
import { usePoll } from './hooks'
import { Approvals, Dashboard, RunConfig, RunInspector } from './views'

type Route = { name: 'runs' } | { name: 'approvals' } | { name: 'new' } | { name: 'run'; id: string }

export default function App() {
  const [route, setRoute] = useState<Route>({ name: 'runs' })
  const pending = usePoll(listApprovals, [], 1500).length
  const nav = (r: Route['name'], label: string, badge = 0) => (
    <button className={route.name === r || (r === 'runs' && route.name === 'run') ? 'nav on' : 'nav'} data-testid={`nav-${r}`} onClick={() => setRoute({ name: r } as Route)}>
      {label}
      {badge > 0 && <span className="badge" data-testid="approvals-badge">{badge}</span>}
    </button>
  )
  return (
    <div className="app">
      <nav className="side">
        <div className="brand">AgentPlane</div>
        {nav('runs', 'Runs')}
        {nav('approvals', 'Approvals', pending)}
        {nav('new', 'New run')}
      </nav>
      <main>
        {route.name === 'runs' && <Dashboard onOpen={(id) => setRoute({ name: 'run', id })} />}
        {route.name === 'run' && <RunInspector key={route.id} runId={route.id} onBack={() => setRoute({ name: 'runs' })} />}
        {route.name === 'approvals' && <Approvals onOpen={(id) => setRoute({ name: 'run', id })} />}
        {route.name === 'new' && <RunConfig onStarted={(id) => setRoute({ name: 'run', id })} />}
      </main>
    </div>
  )
}
